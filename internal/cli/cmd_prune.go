package cli

import (
	"fmt"
	"io"
	"strings"

	"lts-revamp/internal/git"

	"github.com/spf13/cobra"
)

func init() { register(addPrune) }

func addPrune(a *app, root *cobra.Command) {
	var dryRun bool
	cmd := &cobra.Command{
		Use:     "prune [repo...]",
		Short:   "Forget worktrees whose folder was deleted by hand (branches are kept)",
		GroupID: groupRemove,
		Long: `List and prune "missing" worktrees: ones git still has registered although
their folder is gone (deleted with rm, Finder, another tool...). Until
pruned, git keeps their branch "checked out", so no new worktree can use it.

Pruning removes only git's bookkeeping (git worktree prune) plus LTS
leftovers (the workspace file next to the folder). Branches and their
commits are NOT touched — delete those yourself if you no longer need
them. Locked worktrees (git worktree lock) are left alone. No
confirmation is needed; --dry-run lists what would be pruned.

` + "`lts ls`" + ` shows missing worktrees under their repo; JSON: .repos[].stale.` + exitCodesHelp(ExitNotFound, ExitPartial),
		Example: `  lts prune --dry-run
  lts prune
  lts prune core --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{discover: true, dryRun: dryRun, migrate: true})
			if err != nil {
				return err
			}
			repos := env.realRepos(true)
			if len(args) > 0 {
				if repos, err = env.checkRepoFilter(args); err != nil {
					return err
				}
			}
			type item struct {
				Repo string `json:"repo"`
				git.StaleWorktree
			}
			var found []item
			var lockNames []string
			for _, r := range repos {
				for _, s := range r.Stale {
					found = append(found, item{r.Name, s})
				}
				if len(r.Stale) > 0 {
					lockNames = append(lockNames, r.Name)
				}
			}
			if found == nil {
				found = []item{}
			}
			if len(found) == 0 || dryRun {
				a.out.Result(map[string]any{"dry_run": dryRun, "stale": found, "pruned": []item{}}, func(w io.Writer) {
					if len(found) == 0 {
						fmt.Fprintln(w, "No missing worktrees")
						return
					}
					fmt.Fprintf(w, "Would prune %d missing worktree(s):\n", len(found))
					for _, f := range found {
						note := ""
						if f.Locked {
							note = " (locked — kept)"
						}
						fmt.Fprintf(w, "  - [%s] %s %s%s\n", f.Repo, branchLabel(f.Branch), f.Path, note)
					}
				})
				return nil
			}
			release, err := a.lockRepos(env, lockNames)
			if err != nil {
				return err
			}
			defer release()
			pruned := []item{}
			var failed []string
			for _, r := range repos {
				if len(r.Stale) == 0 {
					continue
				}
				done, err := git.PruneStale(env.WorkDir, r.Path, a.out.Log)
				if err != nil {
					failed = append(failed, r.Name+": "+err.Error())
					continue
				}
				for _, s := range done {
					pruned = append(pruned, item{r.Name, s})
				}
			}
			payload := map[string]any{"stale": found, "pruned": pruned}
			if len(failed) > 0 {
				return &Error{Exit: ExitPartial, Code: CodePartial, Data: payload, Message: "prune failed for " + strings.Join(failed, "; ")}
			}
			a.out.Result(payload, func(w io.Writer) {
				fmt.Fprintf(w, "Pruned %d missing worktree(s); their branches were kept\n", len(pruned))
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would be pruned")
	root.AddCommand(cmd)
}
