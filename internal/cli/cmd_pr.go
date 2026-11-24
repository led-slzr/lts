package cli

import (
	"fmt"
	"io"
	"strings"

	"lts-revamp/internal/gh"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"

	"github.com/spf13/cobra"
)

func init() { register(addPR) }

func addPR(a *app, root *cobra.Command) {
	var print bool
	var base, member string
	var repoFlag []string
	cmd := &cobra.Command{
		Use:     "pr [target]",
		Short:   "Open (or print) the GitHub pull-request page for a worktree's branch",
		GroupID: groupChange,
		Long: `Find the open PR for a worktree's branch, or build the GitHub "compare"
page that pre-fills a new PR against the basis branch (--base overrides).

The URL is always printed on stdout. On a terminal the page also opens in
your browser; --print (or no terminal) only prints. The branch must be
pushed and not merged (exit 4 otherwise); unpushed commits produce a
warning. For a mono group pass --member <repo> (each repo gets its own PR).
Existing-PR lookup uses gh when installed; the compare URL needs only a
GitHub origin. GITHUB_INTEGRATION=false disables this command.` + targetHelp + exitCodesHelp(ExitNotFound, ExitGuard),
		Example: `  lts pr
  lts pr feat/login --print
  lts pr feat/integration -r core,erp --member erp --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{discover: true})
			if err != nil {
				return err
			}
			if !env.Cfg.Global.GithubIntegration {
				return guardErr("GitHub integration is disabled").withHint("enable it: lts config set GITHUB_INTEGRATION true")
			}
			t, err := env.resolveTarget(firstArg(args), splitRepoFlag(repoFlag))
			if err != nil {
				return err
			}
			wtPath, repoName := t.WT.Path, t.Repo.Name
			if t.Repo.IsMonorepo {
				var names []string
				for _, m := range git.MonoMembers(t.WT.Path) {
					names = append(names, m.Repo)
					if strings.EqualFold(m.Repo, member) {
						wtPath, repoName = m.Path, m.Repo
					}
				}
				if member == "" || wtPath == t.WT.Path {
					e := usageErr("%s is a mono group; pick the member repo with --member", t.ID())
					e.Available = names
					if member != "" {
						e.Suggestions = suggest(member, names, 3)
					}
					return e
				}
			}
			repo, err := env.findRepo(repoName)
			if err != nil {
				return err
			}
			if !gh.RemoteIsGitHub(repo.Path) {
				return guardErr("%s's origin is not on GitHub", repoName)
			}
			st, _ := git.GetWorktreeStatus(wtPath, env.Cfg.GetRepoBasisBranch(repoName))
			switch {
			case st == git.StatusMergedCleanable || st == git.StatusMergedDirty:
				return guardErr("%s is already merged into its basis branch", t.WT.Branch)
			case !git.HasRemoteBranch(st):
				return guardErr("%s isn't pushed yet", t.WT.Branch).
					withHint("push it first: git -C %q push -u origin %s", wtPath, t.WT.Branch)
			}
			if si := git.GetSyncInfo(wtPath); si.Ahead > 0 {
				a.out.Warn("%d commit(s) not pushed yet — the PR won't include them until you push", si.Ahead)
			}
			if base == "" {
				base = env.Cfg.GetRepoBasisBranch(repoName)
			}
			url, existing := "", false
			if gh.Available() {
				if u := gh.ExistingPRURL(wtPath); u != "" {
					url, existing = u, true
				}
			}
			if url == "" {
				if url, err = gh.CompareURL(repo.Path, base, t.WT.Branch); err != nil {
					return newErr(ExitError, CodeError, "%s", err.Error())
				}
			}
			opened := false
			if !print && a.out.Interactive {
				if err := opener.OpenURL(url); err == nil {
					opened = true
				} else {
					a.out.Warn("could not open a browser: %v", err)
				}
			}
			a.out.Result(map[string]any{"id": t.ID(), "repo": repoName, "branch": t.WT.Branch, "base": base, "url": url, "existing_pr": existing, "opened": opened},
				func(w io.Writer) { fmt.Fprintln(w, url) })
			return nil
		},
	}
	cmd.Flags().BoolVar(&print, "print", false, "only print the URL (never open a browser)")
	cmd.Flags().StringVar(&base, "base", "", "base branch for a new PR (default: the repo's basis branch)")
	cmd.Flags().StringVar(&member, "member", "", "mono group: the member repo to open a PR for")
	cmd.Flags().StringSliceVarP(&repoFlag, "repo", "r", nil, "disambiguate by repo")
	root.AddCommand(cmd)
}
