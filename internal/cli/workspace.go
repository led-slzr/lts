package cli

import (
	"os"
	"path/filepath"
	"strings"

	"lts-revamp/internal/config"
	"lts-revamp/internal/core"
	"lts-revamp/internal/git"
)

// Env is the per-invocation context: the resolved workspace dir (the
// folder that holds the repos), its config, and discovered repos.
type Env struct {
	Out     *Output
	WorkDir string
	// WorkDirSource explains how WorkDir was chosen (flag, env, cwd, ...).
	WorkDirSource string
	Cfg           config.Config
	Repos         []git.Repo
}

type envOpts struct {
	discover   bool // run repo discovery
	withStatus bool // compute per-worktree git status during discovery
	dryRun     bool // never perform layout migration
}

// resolveWorkDir picks the workspace dir: --dir, then $LTS_DIR, then a
// search from the current directory (see findWorkspaceRoot).
func resolveWorkDir(flagDir string) (dir, source string, err error) {
	if flagDir != "" {
		dir, source = flagDir, "--dir"
	} else if env := os.Getenv("LTS_DIR"); env != "" {
		dir, source = env, "LTS_DIR"
	}
	if dir != "" {
		dir = expandHome(dir)
		abs, aerr := filepath.Abs(dir)
		if aerr != nil {
			return "", "", aerr
		}
		info, serr := os.Stat(abs)
		if serr != nil || !info.IsDir() {
			return "", "", usageErr("workspace directory does not exist: %s", abs).
				withHint("pass the folder that contains your repos (%s)", source)
		}
		return abs, source, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	root, how := findWorkspaceRoot(cwd)
	return root, how, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// findWorkspaceRoot walks from dir to the folder holding the repos, so a
// command run from inside a worktree or a repo checkout still targets the
// right workspace:
//  1. dir has a .lts.conf → dir
//  2. dir is inside a git checkout (main repo or any worktree, including
//     ones under a custom LTS root) → the main repo's parent
//  3. dir holds git repos or *-lts dirs → dir
//  4. an ancestor has a .lts.conf → that ancestor
//  5. otherwise dir
func findWorkspaceRoot(dir string) (string, string) {
	if fileExists(filepath.Join(dir, ".lts.conf")) {
		return dir, "cwd"
	}
	if common, err := git.RunGit(dir, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		common = filepath.Clean(strings.TrimSpace(common))
		if filepath.Base(common) == ".git" {
			return filepath.Dir(filepath.Dir(common)), "cwd (inside " + filepath.Base(filepath.Dir(common)) + ")"
		}
	}
	if looksLikeWorkspace(dir) {
		return dir, "cwd"
	}
	for p := filepath.Dir(dir); p != filepath.Dir(p); p = filepath.Dir(p) {
		if fileExists(filepath.Join(p, ".lts.conf")) {
			return p, "ancestor with .lts.conf"
		}
	}
	return dir, "cwd"
}

func looksLikeWorkspace(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), "-lts") {
			return true
		}
		if info, err := os.Stat(filepath.Join(dir, e.Name(), ".git")); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// loadEnv resolves the workspace and (optionally) discovers repos.
func (a *app) loadEnv(o envOpts) (*Env, error) {
	dir, source, err := resolveWorkDir(a.flags.dir)
	if err != nil {
		return nil, err
	}
	cfg := config.LoadReadOnly(dir)
	core.ApplyLTSRoots(&cfg)
	env := &Env{Out: a.out, WorkDir: dir, WorkDirSource: source, Cfg: cfg}
	if a.out.Verbose {
		a.out.Notice("workspace: %s (%s)", dir, source)
	}
	if !o.discover {
		return env, nil
	}
	// Same automatic layout upgrade the TUI runs at startup (all-or-nothing)
	if git.NeedsMigration(dir) || git.NeedsWorkspaceRepair(dir) {
		if o.dryRun {
			a.out.Notice("note: this workspace has an old LTS layout; the next non-dry-run command will upgrade it")
		} else {
			n := git.MigrateDirectoryStructure(dir) + git.RepairWorkspaceContents(dir)
			a.out.Notice("Upgraded LTS directory layout (%d item(s)) — same automatic migration the TUI performs", n)
		}
	}
	env.discover(o.withStatus)
	return env, nil
}

// discover (re)loads repos, marks hidden ones, and applies the sort order.
func (e *Env) discover(withStatus bool) {
	e.discoverWith(git.DiscoverOptions{SkipStatus: !withStatus})
}

func (e *Env) discoverWith(opts git.DiscoverOptions) {
	repos := git.DiscoverReposWith(e.WorkDir, core.BasisResolver(&e.Cfg), opts)
	for i := range repos {
		if !repos[i].IsMonorepo && e.Cfg.IsRepoHidden(repos[i].Name) {
			repos[i].Hidden = true
		}
	}
	core.SortRepos(repos, e.Cfg.Global.SortOrder)
	e.Repos = repos
}

// realRepos returns the non-mono repos (optionally including hidden ones).
func (e *Env) realRepos(includeHidden bool) []git.Repo {
	var out []git.Repo
	for _, r := range e.Repos {
		if r.IsMonorepo || (r.Hidden && !includeHidden) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// findRepo returns a real repo by exact name, with suggestions on a miss.
func (e *Env) findRepo(name string) (git.Repo, error) {
	var names []string
	for _, r := range e.Repos {
		if r.IsMonorepo {
			continue
		}
		if r.Name == name {
			return r, nil
		}
		names = append(names, r.Name)
	}
	for _, r := range e.Repos {
		if !r.IsMonorepo && strings.EqualFold(r.Name, name) {
			return r, nil
		}
	}
	err := notFoundErr("no repo named %q in %s", name, e.WorkDir).withSuggestions(suggest(name, names, 3))
	if len(names) == 0 {
		err.withHint("no git repos found here — run from the folder that contains your repos, or pass --dir")
	} else {
		err.withHint("run `lts ls` to list repos")
	}
	return git.Repo{}, err
}
