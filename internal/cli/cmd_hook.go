package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"lts-revamp/internal/config"
	"lts-revamp/internal/core"
	"lts-revamp/internal/git"

	"github.com/spf13/cobra"
)

func init() { register(addHook) }

// hookInput is the subset of Claude Code's hook stdin JSON we use.
type hookInput struct {
	HookEventName string `json:"hook_event_name"`
	Cwd           string `json:"cwd"`
	Source        string `json:"source"`
	Name          string `json:"name"` // WorktreeCreate
	AgentType     string `json:"agent_type"`
}

func readHookInput(r io.Reader) hookInput {
	var in hookInput
	if f, ok := r.(*os.File); ok && isTerminal(f) {
		return in
	}
	data, _ := io.ReadAll(io.LimitReader(r, 1<<20))
	json.Unmarshal(data, &in)
	return in
}

func addHook(a *app, root *cobra.Command) {
	hook := &cobra.Command{
		Use:     "hook <event>",
		Short:   "Claude Code hook handlers (session-start, subagent-start, worktree-create)",
		GroupID: groupAgents,
		Long: `Handlers for Claude Code hooks, installed by the LTS plugin or
` + "`lts skill install`" + `. Each reads the hook's JSON from stdin and prints the
hook response JSON on stdout. Outside an LTS workspace they print nothing
and exit 0, so they are safe to enable globally.

  session-start    adds a short LTS summary to the session context: where you
                   are (worktree / main checkout), its status, other active
                   worktrees, and the safety rules
  subagent-start   the same summary for every subagent (orchestrators)
  worktree-create  (opt-in) makes Claude Code's isolated worktrees LTS
                   worktrees (env files, deps, setup script, LTS layout).
                   Steps aside — and lets Claude Code create its own — when
                   the repo's main checkout is dirty or off its basis branch,
                   because LTS create would stash/switch it.`,
	}
	for _, ev := range []struct{ name, event string }{
		{"session-start", "SessionStart"}, {"subagent-start", "SubagentStart"},
	} {
		ev := ev
		hook.AddCommand(&cobra.Command{
			Use:   ev.name,
			Short: "Emit LTS context for the " + ev.event + " hook",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				in := readHookInput(stdin)
				ctx := a.hookContext(in.Cwd)
				if ctx == "" {
					return nil
				}
				return writeHookJSON(map[string]any{"hookSpecificOutput": map[string]any{
					"hookEventName": ev.event, "additionalContext": ctx}})
			},
		})
	}
	hook.AddCommand(&cobra.Command{
		Use:   "worktree-create",
		Short: "Create an LTS worktree for Claude Code's WorktreeCreate hook",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			in := readHookInput(stdin)
			path := a.hookWorktreeCreate(in)
			if path == "" {
				return nil // fall back to Claude Code's own worktree
			}
			return writeHookJSON(map[string]any{"hookSpecificOutput": map[string]any{
				"hookEventName": "WorktreeCreate", "worktreePath": path}})
		},
	})
	root.AddCommand(hook)
}

func writeHookJSON(v any) error {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// isLTSWorkspace: the folder is managed by LTS (not just any repo parent).
func isLTSWorkspace(dir string) bool {
	if fileExists(filepath.Join(dir, ".lts.conf")) {
		return true
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), "-lts") {
			return true
		}
	}
	return false
}

func (a *app) hookEnv(cwd string) *Env {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if cwd == "" {
		return nil
	}
	root, _ := findWorkspaceRoot(cwd)
	if !isLTSWorkspace(root) {
		return nil
	}
	cfg := config.LoadReadOnly(root)
	core.ApplyLTSRoots(&cfg)
	a.out = newOutput(false, true, false, true, true)
	env := &Env{Out: a.out, WorkDir: root, Cfg: cfg}
	// Never migrate or fetch from a hook, and skip the per-repo main
	// checkout scan (hookContext checks only the repo the session is in)
	env.discoverWith(git.DiscoverOptions{SkipStatus: true, SkipMigrationCheck: true})
	return env
}

// hookContext builds the ≤15-line session summary ("" = not an LTS workspace).
func (a *app) hookContext(cwd string) string {
	env := a.hookEnv(cwd)
	if env == nil {
		return ""
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	var lines []string
	nRepos, nWT := 0, 0
	for _, r := range env.Repos {
		if !r.IsMonorepo {
			nRepos++
		}
		nWT += len(r.Worktrees)
	}
	lines = append(lines, fmt.Sprintf("LTS workspace: %s (%d repos, %d worktrees). Manage worktrees with the `lts` CLI — run `lts help agents` once for the contract.", env.WorkDir, nRepos, nWT))

	here, inRepo := "", false
	if t, err := env.resolveTarget(cwd, nil); err == nil {
		w := env.worktreeView(t.Repo, t.WT, nil, nil)
		env.fillStatus(t.Repo, t.WT, &w)
		kind := "worktree"
		if t.Repo.IsMonorepo {
			kind = "mono-group worktree (" + strings.Join(t.Repo.RepoNames, " + ") + ")"
		}
		lines = append(lines, fmt.Sprintf("You are in LTS %s %s — branch %s, status: %s.", kind, t.ID(), t.WT.Branch, w.Status.Text))
		here = t.ID()
	} else {
		// Which repo is cwd a checkout of? (git's common dir → main repo)
		top, _ := git.RunGit(cwd, "rev-parse", "--show-toplevel")
		common, _ := git.RunGit(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
		mainRepo := filepath.Dir(filepath.Clean(strings.TrimSpace(common)))
		for _, r := range env.Repos {
			if r.IsMonorepo || common == "" || !pathWithin(mainRepo, r.Path) || !pathWithin(r.Path, mainRepo) {
				continue
			}
			inRepo = true
			if top != "" && !pathWithin(top, r.Path) {
				// a linked worktree of r made by some other tool
				br, _ := git.RunGit(cwd, "branch", "--show-current")
				lines = append(lines, fmt.Sprintf("You are in a git worktree of %s that LTS does not manage (%s, branch %s). Work here as usual; LTS commands that take a target won't see it. New LTS worktrees: lts create <branch> -r %s.", r.Name, top, displayVal(br), r.Name))
				break
			}
			br, _ := git.RunGit(r.Path, "branch", "--show-current")
			lines = append(lines, fmt.Sprintf("You are in the MAIN checkout of %s (branch %s). Don't do branch work here — create a worktree: lts create <branch> -r %s, then work in the printed path.", r.Name, displayVal(br), r.Name))
			if needs, mb, reason := git.MainCheckoutMigration(r.Path, r.MainBranch); needs {
				lines = append(lines, fmt.Sprintf("Note: this main checkout holds work on %s (%s); the user can move it into a worktree via the TUI's \"Migrate to LTS\".", mb, reason))
			}
			break
		}
	}

	// A bare folder with a stray .lts.conf and no worktrees isn't worth
	// the context tokens — speak only where LTS is actually in play
	if here == "" && !inRepo && nWT == 0 {
		return ""
	}

	var others []string
	for _, t := range env.allTargets() {
		if t.ID() == here {
			continue
		}
		others = append(others, t.WT.Branch+" ("+t.Repo.Name+")")
	}
	if len(others) > 0 {
		more := ""
		if len(others) > 8 {
			more = fmt.Sprintf(", +%d more", len(others)-8)
			others = others[:8]
		}
		lines = append(lines, "Other worktrees: "+strings.Join(others, ", ")+more+". Details: lts status --all --json.")
	}
	lines = append(lines,
		"Rules: run `lts delete`/`lts cleanup-merged` with --dry-run first and show the user; never pass --force, --remote, or hibernate unless the user explicitly asked; exit code 4 means a guard refused — ask the user instead of escalating flags.")
	if len(lines) > 15 {
		lines = lines[:15]
	}
	return strings.Join(lines, "\n")
}

var unsafeBranchChars = regexp.MustCompile(`[^A-Za-z0-9._/-]+`)

// hookWorktreeCreate creates (or reuses) an LTS worktree for Claude Code
// and returns its path, or "" to let Claude Code create its own.
func (a *app) hookWorktreeCreate(in hookInput) string {
	env := a.hookEnv(in.Cwd)
	if env == nil {
		return ""
	}
	cwd := in.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	// Which repo(s)? The repo (or mono group) the session is in.
	var names []string
	if t, err := env.resolveTarget(cwd, nil); err == nil {
		if t.Repo.IsMonorepo {
			names = t.Repo.RepoNames
		} else {
			names = []string{t.Repo.Name}
		}
	} else {
		for _, r := range env.realRepos(false) {
			if pathWithin(cwd, r.Path) {
				names = []string{r.Name}
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	branch := strings.Trim(unsafeBranchChars.ReplaceAllString(in.Name, "-"), "-/.")
	if branch == "" || git.ValidateBranchName(branch) != nil {
		return ""
	}
	// Never stash or switch the user's main checkout from a background hook
	for _, n := range names {
		r, err := env.findRepo(n)
		if err != nil {
			return ""
		}
		basis := env.Cfg.GetRepoBasisBranch(n)
		cur, _ := git.RunGit(r.Path, "branch", "--show-current")
		dirty, _ := git.RunGit(r.Path, "status", "--porcelain", "--untracked-files=no")
		if cur != basis || strings.TrimSpace(dirty) != "" {
			fmt.Fprintf(stderr, "lts: %s's main checkout is not clean on %s — letting Claude Code create its own worktree\n", n, basis)
			return ""
		}
	}
	if t := findExisting(env, names, branch); t != nil {
		return t.WT.Path
	}
	release, err := a.lockRepos(env, names)
	if err != nil {
		return ""
	}
	defer release()
	opts := core.WorkspaceOpts(&env.Cfg)
	install := map[string]bool{}
	for _, n := range names {
		install[n] = env.Cfg.Global.InstallOnCreate
	}
	log := &git.CreateLog{Stream: func(ctx, msg string, isErr bool) { fmt.Fprintf(stderr, "[%s] %s\n", ctx, msg) }}
	results, err := git.CreateMonorepoWorktrees(names, env.WorkDir, branch, core.BasisResolver(&env.Cfg), install, opts, log)
	if err != nil || len(results) == 0 {
		return ""
	}
	if len(names) > 1 {
		return filepath.Dir(results[0].WorktreePath)
	}
	return results[0].WorktreePath
}
