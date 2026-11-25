package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"

	"github.com/spf13/cobra"
)

func init() {
	register(addLs)
	register(addStatus)
	register(addPath)
}

func addLs(a *app, root *cobra.Command) {
	var hidden bool
	var repoFlag []string
	cmd := &cobra.Command{
		Use:     "ls [repo...]",
		Aliases: []string{"list"},
		Short:   "List repos and their LTS worktrees (fast, no git status)",
		GroupID: groupInspect,
		Long: `List every repo in the workspace and its LTS worktrees: branch, id, path,
mono-group members, creation/activity times and tmux state.

ls is a fast inventory — it runs no per-worktree git status. Use
` + "`lts status`" + ` for clean/changed/ahead/behind information.

Each worktree has a stable id (<lts-dir>/<folder>, e.g. core-lts/feat-login)
that every other command accepts as a target.` + exitCodesHelp(ExitNotFound),
		Example: `  lts ls
  lts ls core
  lts ls --json | jq -r '.repos[].worktrees[].id'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{discover: true})
			if err != nil {
				return err
			}
			filter := append(args, splitRepoFlag(repoFlag)...)
			if _, err := env.checkRepoFilter(filter); err != nil {
				return err
			}
			tmux := opener.LiveSessions()
			var views []repoView
			for _, r := range env.Repos {
				if r.Hidden && !hidden {
					continue
				}
				if len(filter) > 0 && !repoInFilter(r, filter) {
					continue
				}
				views = append(views, env.repoView(r, tmux, a.busyFn(env)))
			}
			if views == nil {
				views = []repoView{}
			}
			a.out.Result(map[string]any{"workspace": env.WorkDir, "repos": views}, func(w io.Writer) {
				renderLs(w, env, views)
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&hidden, "hidden", false, "include repos hidden in config")
	cmd.Flags().StringSliceVarP(&repoFlag, "repo", "r", nil, "only these repos (repeat or comma-separate)")
	root.AddCommand(cmd)
}

// repoInFilter: a filter name selects that repo and any group containing it.
func repoInFilter(r git.Repo, filter []string) bool {
	for _, f := range filter {
		if strings.EqualFold(r.Name, f) {
			return true
		}
		for _, n := range r.RepoNames {
			if strings.EqualFold(n, f) {
				return true
			}
		}
	}
	return false
}

func renderLs(w io.Writer, env *Env, views []repoView) {
	if len(views) == 0 {
		fmt.Fprintf(w, "No repos found in %s\n", env.WorkDir)
		return
	}
	for i, v := range views {
		if i > 0 {
			fmt.Fprintln(w)
		}
		header := v.Name
		if v.Kind == "group" {
			header += "  (mono group: " + strings.Join(v.Members, " + ") + ")"
		} else {
			header += fmt.Sprintf("  (basis %s", v.BasisBranch)
			if v.PackageManager != "" {
				header += " · " + v.PackageManager
			}
			header += ")"
		}
		if v.Hidden {
			header += "  [hidden]"
		}
		fmt.Fprintln(w, header)
		if v.MainCheckout != nil && v.MainCheckout.NeedsMigration {
			fmt.Fprintf(w, "  ! main checkout is on %s (%s) — open the TUI to migrate it into a worktree\n", v.MainCheckout.Branch, v.MainCheckout.Reason)
		}
		for _, st := range v.Stale {
			fmt.Fprintf(w, "  ! missing: %s (%s) — folder deleted; run `lts prune %s`\n", branchLabel(st.Branch), st.Path, v.Name)
		}
		if len(v.Worktrees) == 0 {
			if len(v.Stale) == 0 {
				fmt.Fprintln(w, "  (no worktrees)")
			}
			continue
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, wt := range v.Worktrees {
			extra := ""
			if wt.TmuxSession {
				extra = "tmux"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", branchLabel(wt.Branch), wt.ID, ago(wt.LastActivity), extra)
		}
		tw.Flush()
	}
}

// branchLabel shows a detached HEAD (no branch) readably.
func branchLabel(b string) string {
	if b == "" {
		return "(detached)"
	}
	return b
}

// ago renders an RFC3339 timestamp as a short relative age.
func ago(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func addStatus(a *app, root *cobra.Command) {
	var all, fetch bool
	var repoFlag []string
	cmd := &cobra.Command{
		Use:     "status [target]",
		Aliases: []string{"st"},
		Short:   "Show git status of worktrees (changed, ahead/behind, merged, delete risk)",
		GroupID: groupInspect,
		Long: `Show the full status of one worktree or all of them.

Target selection: a branch, folder name, id or path. With no target,
status shows the worktree containing the current directory — or every
worktree when you are not inside one. --all always shows every worktree.

States (JSON .status.state): clean, changed, to_push, to_pull, diverged,
no_remote, new, new_dirty, merged_cleanable, merged_dirty, missing.
dangerous_to_delete is true when deleting would destroy work that exists
nowhere else (the same rule ` + "`lts delete`" + ` enforces).

--fetch runs ` + "`git fetch origin`" + ` per repo first so ahead/behind is current
(network; progress on stderr).` + targetHelp + exitCodesHelp(ExitNotFound),
		Example: `  lts status                 # current worktree (or all)
  lts status feat/login -r core
  lts status --all --fetch --json | jq '.worktrees[] | select(.status.dangerous_to_delete)'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{discover: true})
			if err != nil {
				return err
			}
			filter := splitRepoFlag(repoFlag)
			var targets []Target
			if len(args) == 1 && !all {
				t, err := env.resolveTarget(args[0], filter)
				if err != nil {
					return err
				}
				targets = []Target{t}
			} else if !all {
				if t, err := env.resolveTarget("", filter); err == nil {
					targets = []Target{t}
				}
			}
			if targets == nil {
				if _, err := env.checkRepoFilter(filter); err != nil && len(filter) > 0 {
					return err
				}
				for _, t := range env.allTargets() {
					if len(filter) == 0 || repoInFilter(t.Repo, filter) {
						targets = append(targets, t)
					}
				}
			}
			if fetch {
				fetched := map[string]bool{}
				for _, t := range targets {
					for _, p := range repoPathsOf(env, t.Repo) {
						if !fetched[p] {
							fetched[p] = true
							a.out.Log(filepathBase(p), "Fetching origin...", false)
							// No --prune: deleting remote-tracking refs would flip merged
							// branches to "new" and silently change state
							if _, err := git.RunGit(p, "fetch", "origin"); err != nil {
								a.out.Log(filepathBase(p), "fetch failed: "+err.Error(), true)
							}
						}
					}
				}
			}
			tmux := opener.LiveSessions()
			views := make([]worktreeView, len(targets))
			busy := a.busyFn(env)
			for i, t := range targets {
				views[i] = env.worktreeView(t.Repo, t.WT, tmux, busy)
			}
			// Status is ~15 git calls per worktree; run them in parallel
			// (bounded) — results keep their original order
			sem := make(chan struct{}, 8)
			var wg sync.WaitGroup
			for i, t := range targets {
				wg.Add(1)
				go func(i int, t Target) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					env.fillStatus(t.Repo, t.WT, &views[i])
				}(i, t)
			}
			wg.Wait()
			a.out.Result(map[string]any{"workspace": env.WorkDir, "worktrees": views}, func(w io.Writer) {
				renderStatus(w, views)
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "show every worktree")
	cmd.Flags().BoolVar(&fetch, "fetch", false, "git fetch each repo first (network)")
	cmd.Flags().StringSliceVarP(&repoFlag, "repo", "r", nil, "limit to / disambiguate by repo (repeat or comma-separate)")
	root.AddCommand(cmd)
}

func renderStatus(w io.Writer, views []worktreeView) {
	if len(views) == 0 {
		fmt.Fprintln(w, "No LTS worktrees. Create one with: lts create <branch> -r <repo>")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tBRANCH\tSTATUS\tAHEAD/BEHIND\tNOTE")
	for _, v := range views {
		s := v.Status
		ab := "-"
		if s.Upstream != "" || s.Ahead > 0 || s.Behind > 0 {
			ab = fmt.Sprintf("+%d/-%d", s.Ahead, s.Behind)
		}
		var notes []string
		if s.DangerousDelete {
			notes = append(notes, riskNote(s))
		}
		if s.State == "merged_cleanable" {
			notes = append(notes, "safe to clean up")
		}
		if v.Busy {
			notes = append(notes, "busy")
		}
		if v.TmuxSession {
			notes = append(notes, "tmux")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", v.ID, branchLabel(v.Branch), s.Text, ab, strings.Join(notes, ", "))
		for _, m := range s.MemberStatuses {
			fmt.Fprintf(tw, "  └ %s\t\t%s\t\t\n", m.Repo, m.Text)
		}
	}
	tw.Flush()
}

// repoPathsOf returns the main repo dirs behind a repo or mono group.
func repoPathsOf(env *Env, r git.Repo) []string {
	if !r.IsMonorepo {
		return []string{r.Path}
	}
	var out []string
	for _, n := range r.RepoNames {
		if rr, err := env.findRepo(n); err == nil {
			out = append(out, rr.Path)
		}
	}
	return out
}

func addPath(a *app, root *cobra.Command) {
	var repoFlag []string
	var member string
	cmd := &cobra.Command{
		Use:     "path [target]",
		Short:   "Print a worktree's absolute path (nothing else) — for cd \"$(lts path X)\"",
		GroupID: groupInspect,
		Long: `Print the absolute path of one worktree on stdout and nothing else.

For a mono group the path is the branch subdir that holds every member;
--member <repo> prints that member repo's own worktree instead.` + targetHelp + exitCodesHelp(ExitNotFound),
		Example: `  cd "$(lts path feat/login)"
  lts path feat/integration -r core,erp --member erp`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{discover: true})
			if err != nil {
				return err
			}
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			t, err := env.resolveTarget(arg, splitRepoFlag(repoFlag))
			if err != nil {
				return err
			}
			p := t.WT.Path
			if member != "" {
				if !t.Repo.IsMonorepo {
					return usageErr("--member only applies to mono-group worktrees; %s is a single-repo worktree", t.ID())
				}
				found := false
				var names []string
				for _, m := range git.MonoMembers(t.WT.Path) {
					names = append(names, m.Repo)
					if strings.EqualFold(m.Repo, member) {
						p, found = m.Path, true
					}
				}
				if !found {
					return notFoundErr("%s has no member %q", t.ID(), member).withSuggestions(suggest(member, names, 3))
				}
			}
			a.out.Result(map[string]any{"id": t.ID(), "path": p}, func(w io.Writer) { fmt.Fprintln(w, p) })
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&repoFlag, "repo", "r", nil, "disambiguate by repo")
	cmd.Flags().StringVar(&member, "member", "", "mono group: print this member repo's worktree")
	root.AddCommand(cmd)
}
