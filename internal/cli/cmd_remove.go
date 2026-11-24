package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"

	"github.com/spf13/cobra"
)

func init() {
	register(addDelete)
	register(addCleanupMerged)
	register(addCleanModules)
}

// confirmation describes what a destructive command still needs.
type confirmation struct {
	yes, force bool
}

// requireConfirm enforces the TUI's delete rules for a non-TUI caller:
//   - safe:      --yes, or a y/N prompt on a TTY
//   - dangerous: --force AND (--yes or a prompt); on a TTY without --force
//     the user must type DELETE (the TUI's typed confirmation)
func (a *app) requireConfirm(c confirmation, dangerous bool, question, risk string, flagsHint string) error {
	if dangerous && !c.force {
		if a.out.Interactive {
			if a.out.ConfirmTyped(question+"\n"+risk, "DELETE") {
				return nil
			}
			return guardErr("not confirmed")
		}
		return guardErr("refusing: %s", risk).
			withHint("this work exists nowhere else — ask the user first. Safer options: push it (git push -u origin <branch>), or keep the branch (--keep-branch) when there are no uncommitted files. Only with their explicit OK: --force --yes %s", flagsHint)
	}
	if c.yes {
		return nil
	}
	if a.out.Interactive {
		if a.out.Confirm(question) {
			return nil
		}
		return guardErr("not confirmed")
	}
	if dangerous {
		return guardErr("confirmation required (non-interactive)").withHint("re-run with --force --yes %s", flagsHint)
	}
	return guardErr("confirmation required (non-interactive)").withHint("re-run with --yes %sto confirm", flagsHint)
}

func addDelete(a *app, root *cobra.Command) {
	var keepBranch, remote, force, yes, dryRun bool
	var repoFlag []string
	cmd := &cobra.Command{
		Use:     "delete [target]",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a worktree (and by default its local branch) — guarded",
		GroupID: groupRemove,
		Long: `Delete a worktree folder, its workspace file and tmux session, and its
local branch (keep it with --keep-branch). --remote also deletes the branch
on origin. For a mono group every member worktree is deleted.

Guards (same rules as the TUI):
  • Safe states (clean, merged, to_pull, new, missing) need confirmation:
    --yes, or a y/N prompt on a terminal.
  • Dangerous states — uncommitted changes, unpushed commits, never pushed,
    diverged — need --force as well (a terminal asks you to type DELETE).
    The JSON .error.message says exactly what would be lost.
  • Protected branches (main, master, develop, development, staging,
    production, release/*) are never deleted; only the worktree is removed.
  • Paths outside an LTS (-lts) folder are refused.

Without a terminal nothing ever prompts: a missing --yes / --force exits 4.
Run with --dry-run first to see the risk without changing anything.` + targetHelp + exitCodesHelp(ExitNotFound, ExitGuard),
		Example: `  lts delete feat/login --dry-run
  lts delete feat/login --yes
  lts delete feat/old --yes --remote
  lts delete feat/wip --force --yes      # throws away uncommitted work`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if keepBranch && remote {
				return usageErr("--remote deletes the branch on origin; it can't be combined with --keep-branch")
			}
			env, err := a.loadEnv(envOpts{discover: true, dryRun: dryRun, migrate: true})
			if err != nil {
				return err
			}
			t, err := env.resolveTarget(firstArg(args), splitRepoFlag(repoFlag))
			if err != nil {
				return err
			}
			st := statusOf(env, t)
			risk, dangerous := deleteDanger(env, t, st, keepBranch)
			protected := git.IsProtectedBranch(t.WT.Branch)
			deleteLocal := !keepBranch && !protected
			deleteRemote := remote && !protected
			if remote && !protected && !git.HasRemoteBranch(st) {
				a.out.Warn("%s was never pushed; there is no remote branch to delete", t.WT.Branch)
				deleteRemote = false
			}
			if deleteRemote {
				if err := remoteOnlyGuard(env, t, t.WT.Branch, "refs/heads/"+t.WT.Branch); err != nil {
					return err
				}
			}
			if protected && (remote || !keepBranch) {
				a.out.Notice("%s is a protected branch — only the worktree will be removed", t.WT.Branch)
			}
			plan := []string{"remove worktree " + t.WT.Path}
			if t.Repo.IsMonorepo {
				plan = []string{"remove every member worktree in " + t.WT.Path}
			}
			if deleteLocal {
				plan = append(plan, "delete local branch "+t.WT.Branch)
			}
			if deleteRemote {
				plan = append(plan, "delete origin/"+t.WT.Branch)
			}
			plan = append(plan, "remove the workspace file and tmux session")
			res := map[string]any{
				"id": t.ID(), "branch": t.WT.Branch, "path": t.WT.Path, "state": st.Slug(),
				"dangerous": dangerous, "risk": risk, "protected_branch": protected,
				"delete_local_branch": deleteLocal, "delete_remote_branch": deleteRemote, "plan": plan,
			}
			needs := []string{"--yes"}
			if dangerous {
				needs = []string{"--force", "--yes"}
			}
			res["needs"] = needs
			if dryRun {
				res["dry_run"] = true
				a.out.Result(res, func(w io.Writer) {
					fmt.Fprintf(w, "Would delete %s (%s):\n", t.ID(), st.Slug())
					for _, p := range plan {
						fmt.Fprintf(w, "  - %s\n", p)
					}
					if dangerous {
						fmt.Fprintf(w, "\n  ! %s — needs --force\n", risk)
					}
				})
				return nil
			}
			if err := a.requireConfirm(confirmation{yes: yes, force: force}, dangerous,
				fmt.Sprintf("Delete %s (%s)?", t.ID(), t.WT.Branch), risk, ""); err != nil {
				e := asCLIError(err)
				e.Data = res
				return e
			}
			lockNames := []string{t.Repo.Name}
			if t.Repo.IsMonorepo {
				lockNames = t.Repo.RepoNames
			}
			release, err := a.lockRepos(env, lockNames)
			if err != nil {
				return err
			}
			defer release()
			// Re-check under the lock: work may have landed while we waited
			// for confirmation or for the lock
			if now := statusOf(env, t); now != st {
				if _, nowDangerous := deleteDanger(env, t, now, keepBranch); nowDangerous && !dangerous && !force {
					return guardErr("%s changed to %s while waiting — not deleting", t.ID(), now.Slug()).
						withHint("re-run `lts delete %s --dry-run` to see the new risk", t.ID())
				}
			}
			if deleteRemote {
				if err := remoteOnlyGuard(env, t, t.WT.Branch, "refs/heads/"+t.WT.Branch); err != nil {
					return err
				}
			}
			if err := deleteTarget(env, t, deleteLocal, deleteRemote, a.out.Log); err != nil {
				return newErr(ExitError, CodeError, "delete failed: %s", err.Error())
			}
			if cwd, _ := os.Getwd(); pathWithin(cwd, t.WT.Path) {
				a.out.Warn("your current directory was inside the deleted worktree; cd elsewhere")
			}
			res["deleted"] = true
			a.out.Result(res, func(w io.Writer) { fmt.Fprintf(w, "Deleted %s\n", t.ID()) })
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&keepBranch, "keep-branch", false, "keep the local branch (remove only the worktree)")
	fl.BoolVar(&remote, "remote", false, "also delete the branch on origin")
	fl.BoolVar(&force, "force", false, "allow deleting work that exists nowhere else (uncommitted / unpushed)")
	fl.BoolVarP(&yes, "yes", "y", false, "confirm without prompting")
	fl.BoolVar(&dryRun, "dry-run", false, "show what would be deleted and the risk; change nothing")
	fl.StringSliceVarP(&repoFlag, "repo", "r", nil, "disambiguate by repo")
	root.AddCommand(cmd)
}

// deleteDanger is git.DeleteRisk adjusted for --keep-branch: keeping the
// branch preserves its commits, so only uncommitted files can be lost.
func deleteDanger(env *Env, t Target, st git.WTStatus, keepBranch bool) (string, bool) {
	risk, dangerous := git.DeleteRisk(st)
	if !dangerous || !keepBranch || st == git.StatusUnknown {
		return risk, dangerous
	}
	paths := []string{t.WT.Path}
	if t.Repo.IsMonorepo {
		paths = nil
		for _, m := range git.MonoMembers(t.WT.Path) {
			paths = append(paths, m.Path)
		}
	}
	changed := 0
	for _, p := range paths {
		changed += git.GetSyncInfo(p).ChangedFiles
	}
	if changed == 0 {
		return "", false
	}
	return fmt.Sprintf("%d uncommitted file(s) will be LOST (commits stay on the kept branch)", changed), true
}

// remoteOnlyGuard refuses a remote branch delete/replace when origin holds
// commits found nowhere locally (someone else's pushed work).
func remoteOnlyGuard(env *Env, t Target, branch, localRef string) error {
	for _, p := range repoPathsOf(env, t.Repo) {
		if n := git.RemoteOnlyCommits(p, branch, localRef); n > 0 {
			return guardErr("origin/%s in %s has %d commit(s) that exist only on the remote", branch, filepathBase(p), n).
				withHint("pull them first (git pull in the worktree) or drop --remote; deleting the remote branch would destroy them")
		}
	}
	return nil
}

func deleteTarget(env *Env, t Target, deleteLocal, deleteRemote bool, log git.LogFunc) error {
	var err error
	if t.Repo.IsMonorepo {
		err = git.DeleteMonorepoWorktree(env.WorkDir, t.WT.Path, t.WT.Branch, t.Repo.RepoNames, deleteLocal, deleteRemote, log)
	} else {
		err = git.DeleteWorktree(t.Repo.Path, t.WT.Path, t.WT.Branch, deleteLocal, deleteRemote, log)
	}
	if err == nil {
		opener.KillSession(t.WT.Path)
	}
	return err
}

func addCleanupMerged(a *app, root *cobra.Command) {
	var remote, yes, dryRun bool
	var repoFlag []string
	cmd := &cobra.Command{
		Use:     "cleanup-merged",
		Aliases: []string{"cleanup", "prune"},
		Short:   "Delete every merged, clean worktree (and its local branch)",
		GroupID: groupRemove,
		Long: `Delete every worktree whose branch is merged into the basis branch
(including squash/rebase merges) and has no uncommitted changes — state
merged_cleanable. Local branches are deleted too; --remote also deletes
them on origin. A mono group qualifies only when every member does.
Hidden repos are skipped; protected branches are never deleted.

Needs --yes (or a y/N prompt on a terminal). --dry-run lists candidates.` + exitCodesHelp(ExitNotFound, ExitGuard, ExitPartial, ExitInterrupted),
		Example: `  lts cleanup-merged --dry-run
  lts cleanup-merged --yes
  lts cleanup-merged -r core --yes --remote --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{discover: true, withStatus: true, dryRun: dryRun, migrate: true})
			if err != nil {
				return err
			}
			filter := splitRepoFlag(repoFlag)
			if len(filter) > 0 {
				if _, err := env.checkRepoFilter(filter); err != nil {
					return err
				}
			}
			var cands []Target
			for _, t := range env.allTargets() {
				if t.Repo.Hidden || t.WT.Status != git.StatusMergedCleanable || git.IsProtectedBranch(t.WT.Branch) {
					continue
				}
				if len(filter) > 0 && !repoInFilter(t.Repo, filter) {
					continue
				}
				cands = append(cands, t)
			}
			ids := []string{}
			for _, t := range cands {
				ids = append(ids, t.ID())
			}
			if len(cands) == 0 {
				a.out.Result(map[string]any{"candidates": ids, "deleted": []string{}}, func(w io.Writer) {
					fmt.Fprintln(w, "Nothing to clean up — no merged, clean worktrees")
				})
				return nil
			}
			if dryRun {
				a.out.Result(map[string]any{"dry_run": true, "candidates": ids, "delete_remote_branches": remote}, func(w io.Writer) {
					fmt.Fprintf(w, "Would delete %d merged worktree(s):\n", len(ids))
					for _, t := range cands {
						fmt.Fprintf(w, "  - %s (%s)\n", t.ID(), t.WT.Branch)
					}
				})
				return nil
			}
			if err := a.requireConfirm(confirmation{yes: yes}, false,
				fmt.Sprintf("Delete %d merged worktree(s): %s?", len(ids), strings.Join(ids, ", ")), "", ""); err != nil {
				e := asCLIError(err)
				e.Data = map[string]any{"candidates": ids}
				return e
			}
			lockSet := map[string]bool{}
			var lockNames []string
			for _, t := range cands {
				names := []string{t.Repo.Name}
				if t.Repo.IsMonorepo {
					names = t.Repo.RepoNames
				}
				for _, n := range names {
					if !lockSet[n] {
						lockSet[n] = true
						lockNames = append(lockNames, n)
					}
				}
			}
			release, err := a.lockRepos(env, lockNames)
			if err != nil {
				return err
			}
			defer release()
			deleted, failed := []string{}, []repoResult{}
			for _, t := range cands {
				if a.interrupted.Load() {
					break
				}
				// Re-check right before deleting: work may have landed since discovery
				if st := statusOf(env, t); st != git.StatusMergedCleanable {
					a.out.Warn("skipping %s: now %s", t.ID(), st.Slug())
					continue
				}
				if err := deleteTarget(env, t, true, remote, a.out.Log); err != nil {
					failed = append(failed, repoResult{Repo: t.ID(), Error: err.Error()})
					continue
				}
				deleted = append(deleted, t.ID())
			}
			payload := map[string]any{"candidates": ids, "deleted": deleted, "failed": failed}
			if a.interrupted.Load() {
				return &Error{Exit: ExitInterrupted, Code: CodeInterrupted, Data: payload,
					Message: fmt.Sprintf("interrupted after deleting %d of %d worktree(s)", len(deleted), len(cands))}
			}
			if len(failed) > 0 {
				return &Error{Exit: ExitPartial, Code: CodePartial, Data: payload,
					Message: fmt.Sprintf("%d of %d worktree(s) could not be deleted", len(failed), len(cands))}
			}
			a.out.Result(payload, func(w io.Writer) {
				fmt.Fprintf(w, "Deleted %d merged worktree(s)\n", len(deleted))
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "also delete the branches on origin")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm without prompting")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list the candidates; change nothing")
	cmd.Flags().StringSliceVarP(&repoFlag, "repo", "r", nil, "only these repos")
	root.AddCommand(cmd)
}

func addCleanModules(a *app, root *cobra.Command) {
	var dryRun bool
	var repoFlag []string
	cmd := &cobra.Command{
		Use:     "clean-modules [target]",
		Short:   "Remove node_modules (nested too) from a worktree to free disk",
		GroupID: groupRemove,
		Long: `Delete every node_modules directory inside a worktree (every member of
a mono group), reporting how much space was freed. Source files are never
touched — reinstall any time with your package manager. No confirmation
is needed; --dry-run reports what would be freed.` + targetHelp + exitCodesHelp(ExitNotFound),
		Example: `  lts clean-modules feat/old
  lts clean-modules --dry-run --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{discover: true, dryRun: dryRun, migrate: true})
			if err != nil {
				return err
			}
			t, err := env.resolveTarget(firstArg(args), splitRepoFlag(repoFlag))
			if err != nil {
				return err
			}
			paths := []string{t.WT.Path}
			if t.Repo.IsMonorepo {
				paths = nil
				for _, m := range git.MonoMembers(t.WT.Path) {
					paths = append(paths, m.Path)
				}
			}
			if dryRun {
				var total int64
				for _, p := range paths {
					_, mods := git.WorktreeSize(p)
					total += mods
				}
				a.out.Result(map[string]any{"dry_run": true, "id": t.ID(), "bytes": total, "human": git.HumanBytes(total)}, func(w io.Writer) {
					fmt.Fprintf(w, "Would free %s from %s\n", git.HumanBytes(total), t.ID())
				})
				return nil
			}
			lockNames := []string{t.Repo.Name}
			if t.Repo.IsMonorepo {
				lockNames = t.Repo.RepoNames
			}
			release, err := a.lockRepos(env, lockNames)
			if err != nil {
				return err
			}
			defer release()
			var removed int
			var freed int64
			for _, p := range paths {
				n, b, err := git.CleanModules(p, a.out.Log)
				if err != nil {
					return newErr(ExitError, CodeError, "clean-modules failed in %s: %s", filepath.Base(p), err.Error())
				}
				removed += n
				freed += b
			}
			a.out.Result(map[string]any{"id": t.ID(), "removed_dirs": removed, "bytes": freed, "human": git.HumanBytes(freed)}, func(w io.Writer) {
				fmt.Fprintf(w, "Removed %d node_modules dir(s), freed %s\n", removed, git.HumanBytes(freed))
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be freed")
	cmd.Flags().StringSliceVarP(&repoFlag, "repo", "r", nil, "disambiguate by repo")
	root.AddCommand(cmd)
}
