// Package core holds logic shared by the TUI (internal/app) and the CLI
// (internal/cli): config-derived resolvers that the git operations consume.
package core

import (
	"os"
	"path/filepath"
	"strings"

	"lts-revamp/internal/opener"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/tools"
)

// BasisResolver creates a BasisBranchResolver over a snapshot of the per-repo
// config, taken on the caller's thread. Command goroutines must never read
// cfg.Local directly: settings can write that map while an operation runs,
// and concurrent map access panics. Call this (and PkgResolver /
// WorkspaceOpts) when constructing a command, not inside its goroutine.
func BasisResolver(cfg *config.Config) git.BasisBranchResolver {
	snap := make(map[string]string, len(cfg.Local))
	for key, rc := range cfg.Local {
		snap[key] = rc.BasisBranch
	}
	return func(repoName string) string {
		if b, ok := snap[strings.ToUpper(repoName)]; ok && b != "" {
			return b
		}
		return "main"
	}
}

// HiddenResolver snapshots which repos are hidden (see BasisResolver) —
// all-repo operations must leave hidden repos untouched, network included.
func HiddenResolver(cfg *config.Config) git.SkipResolver {
	snap := make(map[string]bool, len(cfg.Local))
	for key, rc := range cfg.Local {
		snap[key] = rc.Hidden
	}
	return func(repoName string) bool {
		return snap[strings.ToUpper(repoName)]
	}
}

// PkgResolver snapshots the per-repo package-manager config (see
// BasisResolver). "auto" resolves per repo via lockfile/corepack detection
// — inside the returned closure, so goroutines detect against the repo's
// files without touching live config.
func PkgResolver(cfg *config.Config) func(string) string {
	def := cfg.Global.PackageManager
	workDir := cfg.WorkDir
	snap := make(map[string]string, len(cfg.Local))
	for key, rc := range cfg.Local {
		snap[key] = rc.PackageManager
	}
	return func(repoName string) string {
		pm, ok := snap[strings.ToUpper(repoName)]
		if !ok || pm == "" {
			pm = def
		}
		if pm == "auto" {
			return tools.DetectPM(filepath.Join(workDir, repoName))
		}
		return pm
	}
}

// ResolvePM resolves a repo's package manager to a concrete tool — never
// returns "auto".
func ResolvePM(cfg *config.Config, repoName string) string {
	pm := cfg.GetRepoPackageManager(repoName)
	if pm == "auto" {
		return tools.DetectPM(filepath.Join(cfg.WorkDir, repoName))
	}
	return pm
}

// SetupResolver snapshots per-repo setup scripts (see BasisResolver).
func SetupResolver(cfg *config.Config) func(string) string {
	snap := make(map[string]string, len(cfg.Local))
	for key, rc := range cfg.Local {
		snap[key] = rc.SetupScript
	}
	return func(repoName string) string {
		return snap[strings.ToUpper(repoName)]
	}
}

// WorkspaceOpts assembles the create-time options from config, snapshotting
// everything on the caller's thread — see BasisResolver.
func WorkspaceOpts(cfg *config.Config) git.WorkspaceOptions {
	return git.WorkspaceOptions{
		PkgManager:   PkgResolver(cfg),
		SetupScript:  SetupResolver(cfg),
		AICliCommand: cfg.Global.AICliCommand,
		IDECommand:   cfg.Global.IDECommand,
		OpenEnvInIDE: cfg.Global.OpenEnvInIDE,
		CopyEnv:      cfg.Global.CopyEnvFiles,
		CopyMCP:      cfg.Global.CopyMCPJson,
	}
}

// ApplyLTSRoots registers the configured per-repo LTS roots with the git
// layer. Call after every config load or LTS_ROOT change.
func ApplyLTSRoots(cfg *config.Config) {
	git.SetLTSRoots(cfg.LTSRoots())
}

// MigrateLayout runs the automatic LTS layout upgrade: group dirs are
// renamed to the collision-proof "+" naming first (tmux sessions, which
// are keyed by path, follow them), then the per-worktree migrations and
// workspace repairs. Returns the number of items changed.
func MigrateLayout(workDir string) int {
	renames := git.MigrateGroupDirNames(workDir)
	for _, g := range renames {
		subs, _ := os.ReadDir(g.New)
		for _, s := range subs {
			if !s.IsDir() || strings.HasPrefix(s.Name(), ".") {
				continue
			}
			opener.RenameSession(filepath.Join(g.Old, s.Name()), filepath.Join(g.New, s.Name()))
			members, _ := os.ReadDir(filepath.Join(g.New, s.Name()))
			for _, m := range members {
				if m.IsDir() {
					opener.RenameSession(filepath.Join(g.Old, s.Name(), m.Name()), filepath.Join(g.New, s.Name(), m.Name()))
				}
			}
		}
	}
	return len(renames) + git.MigrateDirectoryStructure(workDir) + git.RepairWorkspaceContents(workDir)
}
