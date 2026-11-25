package git

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Mono group naming: repo names sorted and joined with "+", each name
// escaped ("%" → "%25", "+" → "%2B") so a name containing "+" can't
// collide. Every repo set maps to exactly one name and back:
//
//	core + erp-ui         → core+erp-ui
//	kindred+lambda + core → core+kindred%2Blambda
//
// Before v3.2 names were joined with "-", so {a, b-c} and {a-b, c} both
// became a-b-c; MigrateGroupDirNames renames those dirs.

func escapeRepoName(n string) string {
	return strings.NewReplacer("%", "%25", "+", "%2B").Replace(n)
}

// MonoGroupName is a group's display/card name ("core+erp-ui").
func MonoGroupName(repos []string) string {
	sorted := append([]string(nil), repos...)
	sort.Strings(sorted)
	for i, n := range sorted {
		sorted[i] = escapeRepoName(n)
	}
	return strings.Join(sorted, "+")
}

// MonoGroupDirName is a group's LTS dir name ("core+erp-ui-lts").
func MonoGroupDirName(repos []string) string {
	return MonoGroupName(repos) + "-lts"
}

// GroupRename is one group dir renamed by MigrateGroupDirNames.
type GroupRename struct {
	Old, New string // absolute dir paths
}

// groupDirsToRename lists group dirs whose name isn't MonoGroupDirName of
// their .lts-repos (and whose target name is free).
func groupDirsToRename(scriptDir string) []GroupRename {
	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		return nil
	}
	var out []GroupRename
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "-lts") {
			continue
		}
		t := getLTSType(scriptDir, e.Name())
		if t != "monorepo" && t != "erp" {
			continue
		}
		repos := getLTSRepos(scriptDir, e.Name())
		if len(repos) < 2 {
			continue
		}
		want := MonoGroupDirName(repos)
		if want == e.Name() {
			continue
		}
		if _, err := os.Stat(filepath.Join(scriptDir, want)); err == nil {
			continue // target taken — leave it (create's ownership check guards it)
		}
		out = append(out, GroupRename{Old: filepath.Join(scriptDir, e.Name()), New: filepath.Join(scriptDir, want)})
	}
	return out
}

// NeedsGroupRename reports whether any group dir still uses old naming.
func NeedsGroupRename(scriptDir string) bool {
	return len(groupDirsToRename(scriptDir)) > 0
}

// MigrateGroupDirNames renames old-style group dirs to MonoGroupDirName
// (one atomic rename per group; member worktrees inside move with it) and
// re-links every member in git so nothing is orphaned. Workspace files use
// relative paths and need no change. Returns the renames so callers can
// follow up (tmux session names are path-based).
func MigrateGroupDirNames(scriptDir string) []GroupRename {
	var done []GroupRename
	for _, g := range groupDirsToRename(scriptDir) {
		// Pin the type before the name stops implying it (legacy "erp-lts")
		typeFile := filepath.Join(g.Old, ".lts-type")
		if _, err := os.Stat(typeFile); os.IsNotExist(err) {
			os.WriteFile(typeFile, []byte("monorepo\n"), 0644)
		}
		if err := os.Rename(g.Old, g.New); err != nil {
			continue
		}
		for _, repo := range getLTSRepos(scriptDir, filepath.Base(g.New)) {
			RepairLinks(scriptDir, filepath.Join(scriptDir, repo))
		}
		done = append(done, g)
	}
	return done
}
