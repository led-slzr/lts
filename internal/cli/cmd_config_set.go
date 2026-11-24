package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/tools"

	"github.com/spf13/cobra"
)

func addConfigSet(a *app, parent *cobra.Command, repoFlag *string) {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Validate and save one setting (global, or per-repo with -r)",
		Long: `Validate VALUE with the same rules as the TUI Settings screen and save it.

Without -r the key is global (~/.config/lts/config). With -r <repo> it is a
per-repo key in <workspace>/.lts.conf: ` + strings.Join(config.RepoKeyNames(), ", ") + `.

Booleans accept true/false/on/off/yes/no. BASIS_BRANCH must exist in the
repo (locally or on origin). LTS_ROOT must be an absolute directory and can
only change while the repo has no worktrees (they would become invisible).
Pass an empty string ("") to reset optional values.` + exitCodesHelp(ExitNotFound, ExitGuard),
		Example: `  lts config set PACKAGE_MANAGER pnpm
  lts config set COPY_MCP_JSON on
  lts config set SETUP_SCRIPT "pnpm --filter shared build" -r core
  lts config set LTS_ROOT ~/worktrees -r erp`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if *repoFlag != "" {
				return a.configSetRepo(*repoFlag, args[0], args[1], dryRun)
			}
			return a.configSetGlobal(args[0], args[1], dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate only; save nothing")
	parent.AddCommand(cmd)
}

func (a *app) configSetGlobal(key, value string, dryRun bool) error {
	if _, ok := config.LookupGlobalKey(key); !ok {
		e := unknownKeyErr(key, false)
		if _, isRepo := config.LookupRepoKey(key); isRepo {
			e.Suggestions = nil
			e.withHint("%s is a per-repo setting — add -r <repo>", strings.ToUpper(key))
		}
		return e
	}
	env, err := a.loadEnv(envOpts{})
	if err != nil {
		return err
	}
	k := strings.ToUpper(key)
	prev, _ := env.Cfg.Global.Get(k)
	if err := env.Cfg.Global.Set(k, value); err != nil {
		return usageErr("%s", err.Error())
	}
	val, _ := env.Cfg.Global.Get(k)
	warnMissingTool(a, k, val)
	existed := fileExists(config.GlobalConfigPath())
	if !dryRun {
		if err := env.Cfg.SaveGlobal(); err != nil {
			return err
		}
		if !existed {
			a.out.Notice("created %s (the TUI's first-run setup wizard will be skipped)", config.GlobalConfigPath())
		}
	}
	res := map[string]any{"scope": "global", "key": k, "value": val, "previous": prev, "file": config.GlobalConfigPath(), "dry_run": dryRun}
	a.out.Result(res, func(w io.Writer) {
		verb := "set"
		if dryRun {
			verb = "would set"
		}
		fmt.Fprintf(w, "%s %s = %s (was %s)\n", verb, k, displayVal(val), displayVal(prev))
	})
	return nil
}

func (a *app) configSetRepo(repo, key, value string, dryRun bool) error {
	if _, ok := config.LookupRepoKey(key); !ok {
		e := unknownKeyErr(key, true)
		if _, isGlobal := config.LookupGlobalKey(key); isGlobal {
			e.Suggestions = nil
			e.withHint("%s is a global setting — drop -r", strings.ToUpper(key))
		}
		return e
	}
	env, err := a.loadEnv(envOpts{discover: true})
	if err != nil {
		return err
	}
	r, err := env.findRepo(repo)
	if err != nil {
		return err
	}
	k := strings.ToUpper(key)
	rc := env.Cfg.Local[strings.ToUpper(r.Name)]
	prev, _ := rc.Get(k)

	switch k {
	case "BASIS_BRANCH":
		value = strings.TrimSpace(value)
		if value == "" {
			return usageErr("BASIS_BRANCH cannot be empty")
		}
		if err := branchExists(r.Path, value); err != nil {
			return err
		}
	case "LTS_ROOT":
		value = strings.TrimSpace(expandHome(value))
		if value != "" {
			abs, _ := filepath.Abs(value)
			value = abs
			if info, err := os.Stat(value); err != nil || !info.IsDir() {
				return usageErr("LTS_ROOT %s is not an existing directory", value).withHint("create it first (mkdir -p %s)", value)
			}
		}
		if len(r.Worktrees) > 0 {
			return guardErr("%s has %d worktree(s) under %s; moving LTS_ROOT would hide them", r.Name, len(r.Worktrees), r.LTSPath).
				withHint("delete them first (`lts ls %s`), then change LTS_ROOT", r.Name)
		}
	}

	if dryRun {
		def, _ := config.LookupRepoKey(k)
		if _, err := def.Validate(value); err != nil {
			return usageErr("%s", err.Error())
		}
	} else if err := env.Cfg.SetRepoKey(r.Name, k, value); err != nil {
		return usageErr("%s", err.Error())
	}
	val := value
	if !dryRun {
		val, _ = env.Cfg.Local[strings.ToUpper(r.Name)].Get(k)
	}
	if k == "PACKAGE_MANAGER" {
		warnMissingTool(a, k, val)
	}
	res := map[string]any{"scope": "repo", "repo": r.Name, "key": k, "value": val, "previous": prev, "file": config.LocalConfigPath(env.WorkDir), "dry_run": dryRun}
	a.out.Result(res, func(w io.Writer) {
		verb := "set"
		if dryRun {
			verb = "would set"
		}
		fmt.Fprintf(w, "%s %s.%s = %s (was %s)\n", verb, r.Name, k, displayVal(val), displayVal(prev))
	})
	return nil
}

// branchExists checks a branch locally or on origin, suggesting near names.
func branchExists(repoPath, branch string) error {
	if _, err := git.RunGit(repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return nil
	}
	if _, err := git.RunGit(repoPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil {
		return nil
	}
	return notFoundErr("branch %q not found in %s (local or origin)", branch, filepath.Base(repoPath)).
		withSuggestions(suggest(branch, allBranchNames(repoPath), 3))
}

// allBranchNames lists local and origin branch names (origin/ stripped).
func allBranchNames(repoPath string) []string {
	out, err := git.RunGit(repoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes/origin")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimPrefix(strings.TrimSpace(l), "origin/")
		if l == "" || l == "HEAD" || l == "origin" || seen[l] {
			continue
		}
		seen[l] = true
		names = append(names, l)
	}
	return names
}

func warnMissingTool(a *app, key, val string) {
	switch key {
	case "IDE_COMMAND", "AI_CLI_COMMAND":
		if val != "" && !tools.CommandPresent(val) {
			a.out.Warn("%s is not installed (or not on PATH)", tools.FirstWord(val))
		}
	case "TERMINAL":
		if val != "" && !tools.TerminalPresent(val) {
			a.out.Warn("terminal %s was not found", val)
		}
	case "PACKAGE_MANAGER":
		if val != "" && val != "auto" && val != "default" && !tools.Has(val) {
			a.out.Warn("%s is not installed (or not on PATH)", val)
		}
	}
}
