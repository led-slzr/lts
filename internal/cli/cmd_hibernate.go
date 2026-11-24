package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"lts-revamp/internal/config"
	"lts-revamp/internal/gh"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"

	"github.com/spf13/cobra"
)

func init() { register(addHibernate) }

type auditCheck struct {
	Label   string   `json:"label"`
	State   string   `json:"state"` // ok | warn | fail
	Details []string `json:"details,omitempty"`
}

func checkState(s git.CheckState) string {
	switch s {
	case git.CheckOK:
		return "ok"
	case git.CheckWarn:
		return "warn"
	}
	return "fail"
}

func addHibernate(a *app, root *cobra.Command) {
	var manual, dryRun bool
	var confirm string
	cmd := &cobra.Command{
		Use:     "hibernate <repo>",
		Short:   "Delete a repo's local copy and worktrees after proving GitHub has everything",
		GroupID: groupRemove,
		Long: `Free disk space by deleting a repo's local checkout and all its LTS
worktrees — the TUI's "Hibernate Repo", with the same audit and rules.

Off unless ENABLE_HIBERNATE is true (lts config set ENABLE_HIBERNATE true).

Strict (default; needs gh signed in and a GitHub origin): fetches origin
and proves every local branch is pushed or merged, there are no stashes,
the main checkout and every worktree are clean, and the repo isn't in a
live mono group. ANY failed check refuses (exit 4) — there is no force.

--manual (repos without gh/GitHub/a remote, and only with no live
worktrees): the same checks are advisory warnings; you take responsibility.

Untracked .env* files are always backed up first to
~/.config/lts/env-backup/<repo>/<timestamp>/ and offered back when you
re-clone through the TUI. A failed backup deletes nothing.

Confirmation: a terminal asks you to type DELETE (strict) or the repo name
(manual). Without a terminal pass --confirm <repo-name> — the exact name,
nothing else is accepted. Run --dry-run first to see the audit.` + exitCodesHelp(ExitNotFound, ExitGuard),
		Example: `  lts hibernate old-service --dry-run
  lts hibernate old-service                       # terminal: type DELETE
  lts hibernate old-service --confirm old-service --json
  lts hibernate scratch --manual --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{discover: true, dryRun: dryRun, migrate: true})
			if err != nil {
				return err
			}
			if !env.Cfg.Global.EnableHibernate {
				return guardErr("hibernate is disabled").withHint("it deletes the local repo — enable it deliberately: lts config set ENABLE_HIBERNATE true")
			}
			repo, err := env.findRepo(args[0])
			if err != nil {
				return err
			}
			github := env.Cfg.Global.GithubIntegration && gh.Available() && gh.Authed() && gh.RemoteIsGitHub(repo.Path)
			switch {
			case !manual && !github:
				return guardErr("strict hibernate needs gh signed in and a GitHub origin for %s", repo.Name).
					withHint("without that, only --manual applies (advisory checks; repo must have no worktrees)")
			case manual && github:
				return usageErr("%s is on GitHub with gh available — use strict mode (drop --manual)", repo.Name)
			case manual && len(repo.Worktrees) > 0:
				return guardErr("--manual needs a repo without live worktrees (%s has %d)", repo.Name, len(repo.Worktrees)).
					withHint("delete or split them first (`lts ls %s`)", repo.Name)
			}
			release, err := a.lockRepos(env, []string{repo.Name})
			if err != nil {
				return err
			}
			defer release()

			audit := git.AuditHibernate(env.WorkDir, repo, env.Cfg.GetRepoBasisBranch(repo.Name), a.out.Log)
			var checks []auditCheck
			for _, c := range audit.Checks {
				checks = append(checks, auditCheck{Label: c.Label, State: checkState(c.State), Details: c.Details})
			}
			var envs []string
			for _, e := range audit.EnvFiles {
				envs = append(envs, e.Rel)
			}
			res := map[string]any{"repo": repo.Name, "mode": map[bool]string{true: "manual", false: "strict"}[manual],
				"checks": checks, "blockers": audit.Blockers(), "env_files": envs,
				"freed_bytes": audit.FreedBytes, "freed": git.HumanBytes(audit.FreedBytes)}
			render := func(w io.Writer, verb string) {
				fmt.Fprintf(w, "%s %s (%s mode) — frees ~%s\n", verb, repo.Name, res["mode"], git.HumanBytes(audit.FreedBytes))
				for _, c := range checks {
					mark := map[string]string{"ok": "✓", "warn": "⚠", "fail": "✗"}[c.State]
					fmt.Fprintf(w, "  %s %s\n", mark, c.Label)
					for _, d := range c.Details {
						fmt.Fprintf(w, "      %s\n", d)
					}
				}
				if len(envs) > 0 {
					fmt.Fprintf(w, "  %d .env file(s) will be backed up first\n", len(envs))
				}
			}
			if !manual && audit.Blockers() > 0 {
				e := guardErr("%d audit check(s) failed — %s can't be hibernated safely", audit.Blockers(), repo.Name).
					withHint("fix each failed check (push, merge, commit or drop stashes), then re-run")
				e.Data = res
				if !a.out.JSON {
					render(a.out.Err, "Audit of")
				}
				return e
			}
			if dryRun {
				res["dry_run"] = true
				a.out.Result(res, func(w io.Writer) { render(w, "Would hibernate") })
				return nil
			}
			word := "DELETE"
			if manual {
				word = repo.Name
			}
			switch {
			case confirm != "":
				if confirm != repo.Name {
					return guardErr("--confirm must be exactly %q", repo.Name)
				}
			case a.out.Interactive:
				render(a.out.Err, "About to hibernate")
				if !a.out.ConfirmTyped("This deletes "+repo.Path+" and its LTS worktrees.", word) {
					return guardErr("not confirmed")
				}
			default:
				e := guardErr("confirmation required (non-interactive)").withHint("re-run with --confirm %s after reviewing --dry-run", repo.Name)
				e.Data = res
				return e
			}

			backupRoot := filepath.Join(config.GlobalConfigDir(), "env-backup")
			dest, n, err := git.BackupEnvs(audit.EnvFiles, backupRoot, repo.Name)
			if err != nil {
				return newErr(ExitError, CodeError, "env backup failed, nothing was deleted: %s", err.Error())
			}
			for _, wt := range repo.Worktrees {
				opener.KillSession(wt.Path)
			}
			opener.KillSession(repo.Path)
			if err := git.HibernateRepo(env.WorkDir, repo, a.out.Log); err != nil {
				return newErr(ExitError, CodeError, "hibernate failed: %s", err.Error())
			}
			res["hibernated"] = true
			res["env_backup"] = dest
			res["env_backed_up"] = n
			a.out.Result(res, func(w io.Writer) {
				fmt.Fprintf(w, "Hibernated %s, freed ~%s", repo.Name, git.HumanBytes(audit.FreedBytes))
				if n > 0 {
					fmt.Fprintf(w, "; %d .env file(s) backed up to %s", n, dest)
				}
				fmt.Fprintln(w)
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&manual, "manual", false, "advisory mode for repos without gh/GitHub (no live worktrees allowed)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "run the audit only")
	cmd.Flags().StringVar(&confirm, "confirm", "", "non-interactive confirmation: the exact repo name")
	root.AddCommand(cmd)
}
