package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lts-revamp/internal/config"
	"lts-revamp/internal/core"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"

	"github.com/spf13/cobra"
)

func init() { register(addCreate) }

type createFlags struct {
	repos     []string
	basis     string
	ltsRoot   string
	install   bool
	noInstall bool
	copyEnv   bool
	noCopyEnv bool
	copyMCP   bool
	noCopyMCP bool
	noSetup   bool
	open      string
	reuse     bool
	dryRun    bool
}

type createdWT struct {
	ID            string `json:"id"`
	Repo          string `json:"repo"`
	Path          string `json:"path"`
	WorkspaceFile string `json:"workspace_file,omitempty"`
	Existing      bool   `json:"existing"`
}

type planStep struct {
	Repo   string `json:"repo"`
	Action string `json:"action"`
	// SideEffect marks changes outside the new worktree (main checkout)
	SideEffect bool `json:"side_effect,omitempty"`
}

func addCreate(a *app, root *cobra.Command) {
	var f createFlags
	cmd := &cobra.Command{
		Use:     "create <branch> [-r repo[,repo...]]",
		Aliases: []string{"new"},
		Short:   "Create a worktree for a branch in one repo, or a mono group across several",
		GroupID: groupChange,
		Long: `Create an LTS worktree for <branch>.

One repo (-r core): core-lts/<branch-dir>/ next to the repos, or under
--lts-root DIR (remembered as the repo's LTS_ROOT so ls, the TUI and every
other command find it). Several repos (-r core,erp): a mono group
core-erp-lts/<branch-dir>/ with one worktree per repo and a shared
workspace file. Repos can also be given as extra arguments.

The branch is checked out if it exists locally, tracked if it exists on
origin, otherwise created from the repo's basis branch (--basis overrides:
--basis dev for all repos, --basis core=dev,erp=main per repo).

Side effects on each repo's MAIN checkout (reported on stderr and in
--dry-run): uncommitted changes there are auto-stashed ("LTS auto-stash
..."), it is switched to the basis branch, and the basis branch is pulled
(--ff-only). Then: git fetch, the worktree is added, .env* (and optionally
.mcp.json) files are copied in, dependencies are installed, the repo's
SETUP_SCRIPT runs, and a .code-workspace file is written.

Output: the new worktree path on stdout (the group dir for a mono group),
so ` + "`cd \"$(lts create feat/x -r core)\"`" + ` works. If a worktree for the branch
already exists, create exits 7 — unless --reuse, which returns the
existing path with exit 0 (safe to retry).` + exitCodesHelp(ExitNotFound, ExitGuard, ExitPartial, ExitExists, ExitInterrupted),
		Example: `  lts create feat/login -r core
  lts create feat/integration -r core,erp --basis erp=dev
  lts create fix/typo -r docs --no-install --open ide
  lts create feat/x -r core --lts-root ~/worktrees
  lts create feat/x -r core --dry-run --json
  cd "$(lts create feat/x -r core --reuse -q)"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runCreate(cmd, args[0], append(splitRepoFlag(f.repos), args[1:]...), f)
		},
	}
	fl := cmd.Flags()
	fl.StringSliceVarP(&f.repos, "repo", "r", nil, "repo(s) to branch (repeat or comma-separate; 2+ = mono group)")
	fl.StringVar(&f.basis, "basis", "", "basis branch override: BRANCH, or repo=BRANCH,repo=BRANCH")
	fl.StringVar(&f.ltsRoot, "lts-root", "", "single repo: directory to hold <repo>-lts (saved as the repo's LTS_ROOT)")
	fl.BoolVar(&f.install, "install", false, "install dependencies (overrides NEW_WT_PACKAGE_INSTALL=false)")
	fl.BoolVar(&f.noInstall, "no-install", false, "skip dependency install")
	fl.BoolVar(&f.copyEnv, "copy-env", false, "copy .env* files (overrides COPY_ENV_FILES=false)")
	fl.BoolVar(&f.noCopyEnv, "no-copy-env", false, "don't copy .env* files")
	fl.BoolVar(&f.copyMCP, "copy-mcp", false, "copy .mcp.json files (overrides COPY_MCP_JSON=false)")
	fl.BoolVar(&f.noCopyMCP, "no-copy-mcp", false, "don't copy .mcp.json files")
	fl.BoolVar(&f.noSetup, "no-setup", false, "skip the repo's SETUP_SCRIPT")
	fl.StringVar(&f.open, "open", "", "open the result afterwards: ide | ai | terminal")
	fl.BoolVar(&f.reuse, "reuse", false, "if the worktree already exists, return it (exit 0) instead of exit 7")
	fl.BoolVar(&f.dryRun, "dry-run", false, "print the plan (including main-checkout side effects); change nothing")
	cmd.MarkFlagsMutuallyExclusive("install", "no-install")
	cmd.MarkFlagsMutuallyExclusive("copy-env", "no-copy-env")
	cmd.MarkFlagsMutuallyExclusive("copy-mcp", "no-copy-mcp")
	root.AddCommand(cmd)
}

// parseBasis turns "dev" or "core=dev,erp=main" into a per-repo map.
func parseBasis(s string, repos []string) (map[string]string, error) {
	out := map[string]string{}
	s = strings.TrimSpace(s)
	if s == "" {
		return out, nil
	}
	if !strings.Contains(s, "=") {
		for _, r := range repos {
			out[r] = s
		}
		return out, nil
	}
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 || kv[0] == "" || kv[1] == "" {
			return nil, usageErr("invalid --basis %q", part).withHint("use --basis BRANCH or --basis repo=BRANCH,repo=BRANCH")
		}
		found := false
		for _, r := range repos {
			if strings.EqualFold(r, kv[0]) {
				out[r] = kv[1]
				found = true
			}
		}
		if !found {
			return nil, usageErr("--basis names repo %q, which is not being created", kv[0]).withSuggestions(suggest(kv[0], repos, 2))
		}
	}
	return out, nil
}

func parseUsage(s string) (opener.ClickUsage, error) {
	switch strings.ToLower(s) {
	case "ide":
		return opener.ClickIDE, nil
	case "ai", "ai-cli", "aicli":
		return opener.ClickAICli, nil
	case "terminal", "term":
		return opener.ClickTerminal, nil
	}
	return 0, usageErr("invalid --open %q", s).withSuggestions(suggest(s, []string{"ide", "ai", "terminal"}, 2))
}

func openerOpts(cfg *config.Config) opener.Options {
	return opener.Options{
		IDECommand:      cfg.Global.IDECommand,
		AICliCommand:    cfg.Global.AICliCommand,
		Terminal:        cfg.Global.Terminal,
		Multiplexer:     cfg.Global.Multiplexer,
		TmuxAIPaneWidth: cfg.Global.TmuxAIPaneWidth,
		TmuxRightPanes:  cfg.Global.TmuxRightPanes,
	}
}

func (a *app) runCreate(cmd *cobra.Command, branch string, repoArgs []string, f createFlags) error {
	branch = strings.TrimSpace(branch)
	if err := git.ValidateBranchName(branch); err != nil {
		return usageErr("invalid branch name %q: %s", branch, err.Error())
	}
	env, err := a.loadEnv(envOpts{discover: true, dryRun: f.dryRun})
	if err != nil {
		return err
	}
	if len(repoArgs) == 0 {
		var names []string
		for _, r := range env.realRepos(false) {
			names = append(names, r.Name)
		}
		e := usageErr("which repo? pass -r <repo> (or -r a,b for a mono group)")
		e.Available = names
		return e.withHint("e.g. lts create %s -r %s", branch, firstOr(names, "<repo>"))
	}
	// Resolve and dedupe repos
	var repos []git.Repo
	seen := map[string]bool{}
	for _, name := range repoArgs {
		r, err := env.findRepo(name)
		if err != nil {
			return err
		}
		if seen[r.Name] {
			continue
		}
		seen[r.Name] = true
		if r.Hidden {
			return guardErr("%s is hidden", r.Name).withHint("unhide it first: lts config set HIDDEN false -r %s", r.Name)
		}
		repos = append(repos, r)
	}
	var names []string
	for _, r := range repos {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	mono := len(repos) > 1

	var usage opener.ClickUsage
	if f.open != "" {
		if usage, err = parseUsage(f.open); err != nil {
			return err
		}
	}

	basisOverride, err := parseBasis(f.basis, names)
	if err != nil {
		return err
	}
	basisFor := func(repo string) string {
		if b, ok := basisOverride[repo]; ok {
			return b
		}
		return env.Cfg.GetRepoBasisBranch(repo)
	}
	for _, r := range repos {
		if err := branchExists(r.Path, basisFor(r.Name)); err != nil {
			e := asCLIError(err)
			e.Message = "basis " + e.Message
			e.withHint("set it with --basis or `lts config set BASIS_BRANCH <branch> -r %s`", r.Name)
			return e
		}
	}

	// --lts-root: single repo only; persisted as LTS_ROOT
	if f.ltsRoot != "" {
		if mono {
			return usageErr("--lts-root applies to single-repo worktrees; mono groups always live in %s", env.WorkDir)
		}
		abs, _ := filepath.Abs(expandHome(f.ltsRoot))
		r := repos[0]
		cur := git.LTSRootFor(env.WorkDir, r.Name)
		if filepath.Clean(abs) != filepath.Clean(cur) && len(r.Worktrees) > 0 {
			return guardErr("%s already has %d worktree(s) under %s", r.Name, len(r.Worktrees), r.LTSPath).
				withHint("a repo keeps one LTS root; omit --lts-root, or delete those worktrees first")
		}
		f.ltsRoot = abs
	}

	// Already exists? (exit 7 unless --reuse)
	if existing := findExisting(env, names, branch); existing != nil {
		if !f.reuse {
			return newErr(ExitExists, CodeExists, "a worktree for %s already exists: %s", branch, existing.ID()).
				withHint("use it (`lts path %s`), or pass --reuse to get its path with exit 0", existing.ID())
		}
		a.out.Notice("reusing existing worktree %s", existing.ID())
		return a.emitCreate(env, branch, mono, existingResult(*existing), nil, true, f, usage)
	}
	// Branch checked out elsewhere (main checkout, or a single worktree when
	// asking for a mono group)? git would refuse — explain it up front.
	for _, r := range repos {
		if where := checkedOutAt(r.Path, branch); where != "" {
			msg := fmt.Sprintf("branch %s is already checked out in %s at %s", branch, r.Name, where)
			if filepath.Clean(where) == filepath.Clean(r.Path) {
				return guardErr("%s", msg).withHint("that is the repo's main checkout — open the TUI and use \"Migrate to LTS\", or switch it back to %s", basisFor(r.Name))
			}
			if t := targetAtPath(env, where); t != nil {
				if mono && !t.Repo.IsMonorepo {
					return guardErr("%s (LTS worktree %s)", msg, t.ID()).withHint("to add it to a mono group, use the TUI's \"Add to Mono Group\"; or pick another branch name")
				}
				return guardErr("%s (LTS worktree %s)", msg, t.ID()).withHint("use that worktree (`lts path %s`), or pick another branch name", t.ID())
			}
			return guardErr("%s, a worktree LTS does not manage", msg).withHint("finish or remove it there (git worktree remove %q), or pick another branch name", where)
		}
	}

	opts := core.WorkspaceOpts(&env.Cfg)
	if f.copyEnv {
		opts.CopyEnv = true
	}
	if f.noCopyEnv {
		opts.CopyEnv = false
	}
	if f.copyMCP {
		opts.CopyMCP = true
	}
	if f.noCopyMCP {
		opts.CopyMCP = false
	}
	if f.noSetup {
		opts.SetupScript = func(string) string { return "" }
	}
	install := env.Cfg.Global.InstallOnCreate
	if f.install {
		install = true
	}
	if f.noInstall {
		install = false
	}
	installDeps := map[string]bool{}
	for _, n := range names {
		installDeps[n] = install
	}

	plan := createPlan(env, repos, branch, basisFor, opts, install, f.ltsRoot)
	if f.dryRun {
		res := map[string]any{"dry_run": true, "branch": branch, "repos": names, "group": mono, "plan": plan}
		a.out.Result(res, func(w io.Writer) {
			fmt.Fprintf(w, "Would create %s in %s:\n", branch, strings.Join(names, ", "))
			for _, s := range plan {
				mark := " "
				if s.SideEffect {
					mark = "!"
				}
				fmt.Fprintf(w, "  %s [%s] %s\n", mark, s.Repo, s.Action)
			}
			fmt.Fprintln(w, "\n(! = changes the repo's main checkout)")
		})
		return nil
	}

	release, err := a.lockRepos(env, names)
	if err != nil {
		return err
	}
	defer release()

	if f.ltsRoot != "" {
		if err := os.MkdirAll(f.ltsRoot, 0755); err != nil {
			return err
		}
		if err := env.Cfg.SetRepoKey(repos[0].Name, "LTS_ROOT", f.ltsRoot); err != nil {
			return usageErr("%s", err.Error())
		}
		core.ApplyLTSRoots(&env.Cfg)
	}

	log := &git.CreateLog{Stream: a.out.Log}
	results, cerr := git.CreateMonorepoWorktrees(names, env.WorkDir, branch, basisFor, installDeps, opts, log)
	if cerr != nil {
		return newErr(ExitError, CodeError, "create failed: %s", cerr.Error()).
			withHint("see the log above (stderr); nothing was created")
	}
	env.discover(false)
	var created []createdWT
	for _, r := range results {
		id := ""
		if mono {
			id = strings.Join(names, "-") + "-lts/" + filepath.Base(filepath.Dir(r.WorktreePath))
		} else if repo, err := env.findRepo(r.RepoName); err == nil {
			id = repo.LTSDir + "/" + filepath.Base(r.WorktreePath)
		}
		created = append(created, createdWT{ID: id, Repo: r.RepoName, Path: r.WorktreePath, WorkspaceFile: r.WorkspaceFile, Existing: r.IsExisting})
	}
	var partialErr error
	if mono && len(results) < len(names) {
		got := map[string]bool{}
		for _, r := range results {
			got[r.RepoName] = true
		}
		var failed []string
		for _, n := range names {
			if !got[n] {
				failed = append(failed, n)
			}
		}
		partialErr = &Error{Exit: ExitPartial, Code: CodePartial,
			Message: "created the group without: " + strings.Join(failed, ", "),
			Hint:    "see the log above (stderr) for why those repos were skipped"}
	}
	return a.emitCreate(env, branch, mono, created, log.Steps, false, f, usage, partialErr)
}

func firstOr(list []string, def string) string {
	if len(list) > 0 {
		return list[0]
	}
	return def
}

// findExisting returns the existing worktree for branch in exactly this
// repo set (single repo, or the mono group with these members).
func findExisting(env *Env, names []string, branch string) *Target {
	for _, t := range env.allTargets() {
		if t.WT.Branch != branch {
			continue
		}
		if len(names) == 1 && !t.Repo.IsMonorepo && t.Repo.Name == names[0] {
			tt := t
			return &tt
		}
		if len(names) > 1 && t.Repo.IsMonorepo && repoFilterMatches(t.Repo, names) {
			tt := t
			return &tt
		}
	}
	return nil
}

func existingResult(t Target) []createdWT {
	if !t.Repo.IsMonorepo {
		ws := filepath.Join(filepath.Dir(t.WT.Path), filepath.Base(t.WT.Path)+".code-workspace")
		if !fileExists(ws) {
			ws = ""
		}
		return []createdWT{{ID: t.ID(), Repo: t.Repo.Name, Path: t.WT.Path, WorkspaceFile: ws, Existing: true}}
	}
	var out []createdWT
	for _, m := range git.MonoMembers(t.WT.Path) {
		out = append(out, createdWT{ID: t.ID(), Repo: m.Repo, Path: m.Path, Existing: true})
	}
	return out
}

// targetAtPath returns the LTS worktree whose path contains p, if any.
func targetAtPath(env *Env, p string) *Target {
	for _, t := range env.allTargets() {
		if pathWithin(p, t.WT.Path) {
			tt := t
			return &tt
		}
	}
	return nil
}

// checkedOutAt returns where branch is checked out in repoPath ("" = nowhere).
func checkedOutAt(repoPath, branch string) string {
	out, err := git.RunGit(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	var cur string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "worktree ") {
			cur = strings.TrimPrefix(l, "worktree ")
		}
		if l == "branch refs/heads/"+branch {
			return cur
		}
	}
	return ""
}

// createPlan predicts what create will do (dry-run and JSON "plan").
func createPlan(env *Env, repos []git.Repo, branch string, basisFor func(string) string, opts git.WorkspaceOptions, install bool, ltsRoot string) []planStep {
	var plan []planStep
	dir := git.BranchToDirName(branch)
	var names []string
	for _, r := range repos {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	for _, r := range repos {
		basis := basisFor(r.Name)
		add := func(action string, side bool) {
			plan = append(plan, planStep{Repo: r.Name, Action: action, SideEffect: side})
		}
		if dirty, _ := git.RunGit(r.Path, "status", "--porcelain", "--untracked-files=no"); strings.TrimSpace(dirty) != "" {
			add("stash uncommitted changes in the main checkout (git stash list → \"LTS auto-stash ...\")", true)
		}
		if cur, _ := git.RunGit(r.Path, "branch", "--show-current"); cur != basis {
			add(fmt.Sprintf("switch the main checkout from %s to %s", displayVal(cur), basis), true)
		}
		add("pull "+basis+" --ff-only in the main checkout", true)
		add("git fetch origin", false)
		mode := "create new branch " + branch + " from " + basis
		if _, err := git.RunGit(r.Path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			mode = "check out existing local branch " + branch
		} else if _, err := git.RunGit(r.Path, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil {
			mode = "track origin/" + branch
		}
		var path string
		if len(repos) > 1 {
			path = filepath.Join(env.WorkDir, strings.Join(names, "-")+"-lts", dir, r.Name+"-"+dir)
		} else {
			root := git.LTSRootFor(env.WorkDir, r.Name)
			if ltsRoot != "" {
				root = ltsRoot
				add("save LTS_ROOT="+ltsRoot+" for "+r.Name+" in .lts.conf", false)
			}
			path = filepath.Join(root, r.Name+"-lts", dir)
		}
		add(mode+" at "+path, false)
		if opts.CopyEnv {
			envs, _ := filepath.Glob(filepath.Join(r.Path, ".env*"))
			add(fmt.Sprintf("copy .env* files from the main checkout, nested ones included (%d at the repo root; COPY_ENV_FILES, --no-copy-env to skip)", len(envs)), false)
		}
		if opts.CopyMCP && fileExists(filepath.Join(r.Path, ".mcp.json")) {
			add("copy .mcp.json (COPY_MCP_JSON; --no-copy-mcp to skip)", false)
		}
		if install && fileExists(filepath.Join(r.Path, "package.json")) {
			add("install dependencies with "+opts.PkgManager(r.Name), false)
		}
		if s := opts.SetupScript(r.Name); s != "" {
			add("run setup script: "+s, false)
		}
	}
	plan = append(plan, planStep{Repo: strings.Join(names, ","), Action: "write .code-workspace file"})
	return plan
}

func (a *app) emitCreate(env *Env, branch string, mono bool, created []createdWT, steps []string, reused bool, f createFlags, usage opener.ClickUsage, partial ...error) error {
	primary := ""
	if len(created) > 0 {
		primary = created[0].Path
		if mono {
			primary = filepath.Dir(created[0].Path)
		}
	}
	if f.open != "" && primary != "" {
		target := primary
		if usage == opener.ClickIDE && len(created) > 0 && created[0].WorkspaceFile != "" {
			target = created[0].WorkspaceFile
		}
		if err := opener.OpenWorktree(target, usage, openerOpts(&env.Cfg)); err != nil {
			a.out.Warn("could not open %s: %v", target, err)
		}
	}
	if steps == nil {
		steps = []string{}
	}
	res := map[string]any{"branch": branch, "group": mono, "path": primary, "reused": reused, "worktrees": created, "steps": steps}
	var perr *Error
	if len(partial) > 0 && partial[0] != nil {
		perr = asCLIError(partial[0])
		perr.Data = res
		if !a.out.JSON {
			fmt.Fprintln(a.out.Out, primary)
		}
		return perr
	}
	a.out.Result(res, func(w io.Writer) { fmt.Fprintln(w, primary) })
	return nil
}
