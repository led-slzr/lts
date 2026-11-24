package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"lts-revamp/internal/core"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"

	"github.com/spf13/cobra"
)

func init() {
	register(addRefresh)
	register(addRebase)
	register(addRename)
}

type repoResult struct {
	Repo  string `json:"repo"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func addRefresh(a *app, root *cobra.Command) {
	var dryRun bool
	var repoFlag []string
	cmd := &cobra.Command{
		Use:     "refresh [repo...]",
		Aliases: []string{"fetch"},
		Short:   "Fetch origin and fast-forward each repo's basis branch",
		GroupID: groupChange,
		Long: `For each repo: git fetch origin, then fast-forward the basis branch —
pull --ff-only when the main checkout is on it, otherwise update the ref
(git fetch origin basis:basis). Worktrees are not touched; rebase them with
` + "`lts rebase`" + `. With no repos, every repo that isn't hidden is refreshed.` + exitCodesHelp(ExitNotFound, ExitPartial, ExitInterrupted),
		Example: `  lts refresh
  lts refresh core erp
  lts refresh --json | jq '.results[] | select(.ok|not)'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{discover: true, dryRun: dryRun})
			if err != nil {
				return err
			}
			names := append(args, splitRepoFlag(repoFlag)...)
			var repos []git.Repo
			if len(names) == 0 {
				repos = env.realRepos(false)
			} else if repos, err = env.checkRepoFilter(names); err != nil {
				return err
			}
			if dryRun {
				var list []string
				for _, r := range repos {
					list = append(list, r.Name)
				}
				a.out.Result(map[string]any{"dry_run": true, "repos": list}, func(w io.Writer) {
					fmt.Fprintf(w, "Would refresh: %s\n", strings.Join(list, ", "))
				})
				return nil
			}
			var lockNames []string
			for _, r := range repos {
				lockNames = append(lockNames, r.Name)
			}
			release, err := a.lockRepos(env, lockNames)
			if err != nil {
				return err
			}
			defer release()
			results := []repoResult{}
			failed := 0
			for _, r := range repos {
				if a.interrupted.Load() {
					break
				}
				res := repoResult{Repo: r.Name, OK: true}
				if err := git.RefreshRepo(r.Path, env.Cfg.GetRepoBasisBranch(r.Name), a.out.Log); err != nil {
					res.OK, res.Error = false, err.Error()
					failed++
					a.out.Log(r.Name, "Failed: "+err.Error(), true)
				} else {
					env.Cfg.SetRepoLastRefresh(r.Name, time.Now().Unix())
				}
				results = append(results, res)
			}
			payload := map[string]any{"results": results}
			if failed > 0 {
				return &Error{Exit: ExitPartial, Code: CodePartial, Data: payload,
					Message: fmt.Sprintf("%d of %d repo(s) failed to refresh", failed, len(results))}
			}
			a.out.Result(payload, func(w io.Writer) {
				fmt.Fprintf(w, "Refreshed %d repo(s)\n", len(results))
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list the repos that would be refreshed")
	cmd.Flags().StringSliceVarP(&repoFlag, "repo", "r", nil, "repos to refresh (same as positional args)")
	root.AddCommand(cmd)
}

func addRebase(a *app, root *cobra.Command) {
	var noInstall, refresh, dryRun bool
	var repoFlag []string
	cmd := &cobra.Command{
		Use:     "rebase [target]",
		Short:   "Rebase a worktree onto its repo's basis branch",
		GroupID: groupChange,
		Long: `Rebase a single-repo worktree onto the repo's basis branch (as it exists
locally — add --refresh to fetch and fast-forward it first).

Uncommitted changes are stashed and restored. On a conflict the rebase is
aborted, changes are restored, and the command exits 6 — the worktree is
left exactly as it was. After a successful rebase, dependencies are
reinstalled when the worktree has a package.json (skip with --no-install).

Mono-group worktrees are not supported yet (exit 4).` + targetHelp + exitCodesHelp(ExitNotFound, ExitGuard, ExitConflict),
		Example: `  lts rebase                     # the worktree you're in
  lts rebase feat/login --refresh
  lts rebase feat/login -r core --no-install --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{discover: true, dryRun: dryRun})
			if err != nil {
				return err
			}
			t, err := env.resolveTarget(firstArg(args), splitRepoFlag(repoFlag))
			if err != nil {
				return err
			}
			if t.Repo.IsMonorepo {
				return guardErr("rebase isn't supported for mono-group worktrees yet (%s)", t.ID()).
					withHint("rebase each member with git: cd \"$(lts path %s --member <repo>)\" && git rebase <basis>", t.ID())
			}
			basis := env.Cfg.GetRepoBasisBranch(t.Repo.Name)
			main := t.Repo.MainBranch
			if main == "" {
				main = basis
			}
			pm := ""
			if !noInstall {
				pm = core.ResolvePM(&env.Cfg, t.Repo.Name)
			}
			if dryRun {
				si := git.GetSyncInfo(t.WT.Path)
				behind, _ := git.RunGit(t.WT.Path, "rev-list", "--count", "HEAD.."+main)
				steps := []string{}
				if refresh {
					steps = append(steps, "refresh "+t.Repo.Name+" (fetch + fast-forward "+main+")")
				}
				if si.ChangedFiles > 0 {
					steps = append(steps, fmt.Sprintf("stash %d changed file(s), restore after", si.ChangedFiles))
				}
				steps = append(steps, fmt.Sprintf("rebase %s onto %s (%s new commit(s) on %s as of now)", t.WT.Branch, main, strings.TrimSpace(behind), main))
				if pm != "" && fileExists(filepath.Join(t.WT.Path, "package.json")) {
					steps = append(steps, "reinstall dependencies with "+pm)
				}
				a.out.Result(map[string]any{"dry_run": true, "id": t.ID(), "onto": main, "plan": steps}, func(w io.Writer) {
					fmt.Fprintf(w, "Would rebase %s:\n", t.ID())
					for _, s := range steps {
						fmt.Fprintf(w, "  - %s\n", s)
					}
				})
				return nil
			}
			release, err := a.lockRepos(env, []string{t.Repo.Name})
			if err != nil {
				return err
			}
			defer release()
			if refresh {
				if err := git.RefreshRepo(t.Repo.Path, basis, a.out.Log); err != nil {
					return newErr(ExitError, CodeError, "refresh failed: %s", err.Error())
				}
			}
			before, _ := git.RunGit(t.WT.Path, "rev-parse", "HEAD")
			if err := git.RebaseWorktree(t.WT.Path, main, pm, a.out.Log); err != nil {
				if strings.HasPrefix(err.Error(), "rebase conflict") && strings.Contains(err.Error(), "changes restored") {
					return &Error{Exit: ExitConflict, Code: CodeConflict, Message: err.Error(),
						Hint: fmt.Sprintf("resolve it manually: cd %q && git rebase %s", t.WT.Path, main)}
				}
				return newErr(ExitError, CodeError, "%s", err.Error()).withHint("check the worktree with `git status` in %s", t.WT.Path)
			}
			after, _ := git.RunGit(t.WT.Path, "rev-parse", "HEAD")
			res := map[string]any{"id": t.ID(), "onto": main, "path": t.WT.Path, "changed": before != after, "head": after}
			a.out.Result(res, func(w io.Writer) {
				if before == after {
					fmt.Fprintf(w, "%s is already up to date with %s\n", t.ID(), main)
				} else {
					fmt.Fprintf(w, "Rebased %s onto %s\n", t.ID(), main)
				}
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&noInstall, "no-install", false, "skip reinstalling dependencies")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "fetch and fast-forward the basis branch first")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the plan; change nothing")
	cmd.Flags().StringSliceVarP(&repoFlag, "repo", "r", nil, "disambiguate by repo")
	root.AddCommand(cmd)
}

func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func addRename(a *app, root *cobra.Command) {
	var remote, dryRun bool
	var repoFlag []string
	cmd := &cobra.Command{
		Use:     "rename [target] <new-branch>",
		Aliases: []string{"mv"},
		Short:   "Rename a worktree's branch (and its folder, workspace file, tmux session)",
		GroupID: groupChange,
		Long: `Rename the branch of a worktree — or of every member of a mono group —
and move its folder, workspace file and tmux session to match.

With one argument, the worktree containing the current directory is
renamed. --remote also renames the branch on origin (push new, delete old);
without it the old remote branch is left alone. Protected branches (main,
master, develop, development, staging, production, release/*) can't be
renamed.` + targetHelp + exitCodesHelp(ExitNotFound, ExitGuard, ExitExists),
		Example: `  lts rename feat/login feat/sign-in
  lts rename feat/sign-in            # inside the worktree
  lts rename feat/login feat/sign-in --remote --json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, newBranch := "", args[0]
			if len(args) == 2 {
				target, newBranch = args[0], args[1]
			}
			newBranch = strings.TrimSpace(newBranch)
			if err := git.ValidateBranchName(newBranch); err != nil {
				return usageErr("invalid branch name %q: %s", newBranch, err.Error())
			}
			env, err := a.loadEnv(envOpts{discover: true, withStatus: false, dryRun: dryRun})
			if err != nil {
				return err
			}
			t, err := env.resolveTarget(target, splitRepoFlag(repoFlag))
			if err != nil {
				return err
			}
			old := t.WT.Branch
			if old == newBranch {
				return usageErr("%s is already named %s", t.ID(), newBranch)
			}
			if git.IsProtectedBranch(old) {
				return guardErr("%s is a protected branch and can't be renamed", old)
			}
			for _, p := range repoPathsOf(env, t.Repo) {
				if _, err := git.RunGit(p, "show-ref", "--verify", "--quiet", "refs/heads/"+newBranch); err == nil {
					return newErr(ExitExists, CodeExists, "branch %s already exists in %s", newBranch, filepath.Base(p)).
						withHint("pick another name, or delete that branch first")
				}
			}
			st := statusOf(env, t)
			if remote && !git.HasRemoteBranch(st) {
				return usageErr("--remote: %s was never pushed, so there is no remote branch to rename", old).
					withHint("drop --remote")
			}
			newDir := git.BranchToDirName(newBranch)
			if dryRun {
				steps := []string{"rename branch " + old + " → " + newBranch, "move folder " + filepath.Base(t.WT.Path) + " → " + newDir, "rewrite the workspace file", "rename the tmux session (if any)"}
				if remote {
					steps = append(steps, "push "+newBranch+" to origin and delete origin/"+old)
				}
				a.out.Result(map[string]any{"dry_run": true, "id": t.ID(), "from": old, "to": newBranch, "plan": steps}, func(w io.Writer) {
					fmt.Fprintf(w, "Would rename %s:\n", t.ID())
					for _, s := range steps {
						fmt.Fprintf(w, "  - %s\n", s)
					}
				})
				return nil
			}
			lock := []string{t.Repo.Name}
			if t.Repo.IsMonorepo {
				lock = t.Repo.RepoNames
			}
			release, err := a.lockRepos(env, lock)
			if err != nil {
				return err
			}
			defer release()
			ai, ide, openEnv := env.Cfg.Global.AICliCommand, env.Cfg.Global.IDECommand, env.Cfg.Global.OpenEnvInIDE
			var res *git.RenameResult
			if t.Repo.IsMonorepo {
				pm := core.ResolvePM(&env.Cfg, t.Repo.RepoNames[0])
				res, err = git.RenameMonorepoWorktrees(env.WorkDir, t.WT.Path, t.Repo.RepoNames, old, newBranch, remote, pm, ai, ide, openEnv, a.out.Log)
			} else {
				pm := core.ResolvePM(&env.Cfg, t.Repo.Name)
				res, err = git.RenameWorktree(t.Repo.Path, t.WT.Path, old, newBranch, remote, pm, ai, ide, openEnv, a.out.Log)
			}
			if err != nil {
				return newErr(ExitError, CodeError, "rename failed: %s", err.Error())
			}
			newPath := t.WT.Path
			if res != nil && res.NewPath != "" {
				newPath = res.NewPath
				opener.RenameSession(t.WT.Path, res.NewPath)
			}
			newID := t.Repo.LTSDir + "/" + filepath.Base(newPath)
			a.out.Result(map[string]any{"from": old, "to": newBranch, "id": newID, "path": newPath, "remote_renamed": remote}, func(w io.Writer) {
				fmt.Fprintln(w, newPath)
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "also rename the branch on origin")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the plan; change nothing")
	cmd.Flags().StringSliceVarP(&repoFlag, "repo", "r", nil, "disambiguate by repo")
	root.AddCommand(cmd)
}

// statusOf computes the git status of a target (worst member for groups).
func statusOf(env *Env, t Target) git.WTStatus {
	if !t.Repo.IsMonorepo {
		st, _ := git.GetWorktreeStatus(t.WT.Path, env.Cfg.GetRepoBasisBranch(t.Repo.Name))
		return st
	}
	worst := git.StatusClean
	for _, m := range git.MonoMembers(t.WT.Path) {
		st, _ := git.GetWorktreeStatus(m.Path, env.Cfg.GetRepoBasisBranch(m.Repo))
		if statusSeverity(st) > statusSeverity(worst) {
			worst = st
		}
	}
	return worst
}
