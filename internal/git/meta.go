package git

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// LTS-recorded worktree metadata, stored as .lts-meta.json in each LTS dir.
// Git doesn't record worktree creation time and directory birthtime isn't
// portable, so creation is recorded at create time; worktrees that predate
// the file simply have no entry (CreatedAt 0).
const ltsMetaFile = ".lts-meta.json"

type wtMeta struct {
	CreatedAt int64 `json:"created_at"`
	// NamingVerified marks a directory that the startup migration check has
	// certified as using the current naming convention. Once set, the dir is
	// never treated as legacy again, even if a later branch switch makes its
	// name coincide with the legacy form of the new branch.
	NamingVerified bool `json:"naming_verified,omitempty"`
}

type ltsMeta struct {
	Worktrees map[string]wtMeta `json:"worktrees"`
}

func readLTSMeta(ltsDir string) ltsMeta {
	meta := ltsMeta{Worktrees: map[string]wtMeta{}}
	data, err := os.ReadFile(filepath.Join(ltsDir, ltsMetaFile))
	if err != nil {
		return meta
	}
	json.Unmarshal(data, &meta)
	if meta.Worktrees == nil {
		meta.Worktrees = map[string]wtMeta{}
	}
	return meta
}

func writeLTSMeta(ltsDir string, meta ltsMeta) {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(ltsDir, ltsMetaFile), append(data, '\n'), 0644)
}

// recordWorktreeCreated stores the creation timestamp of a worktree
// (or a monorepo branch subdir) in its LTS dir's metadata.
func recordWorktreeCreated(ltsDir, wtName string, ts int64) {
	meta := readLTSMeta(ltsDir)
	meta.Worktrees[wtName] = wtMeta{CreatedAt: ts}
	writeLTSMeta(ltsDir, meta)
}

// worktreeCreatedAt returns the recorded creation time, 0 if unknown.
func worktreeCreatedAt(ltsDir, wtName string) int64 {
	return readLTSMeta(ltsDir).Worktrees[wtName].CreatedAt
}

// namingVerified reports whether the dir was certified by a migration check.
func namingVerified(ltsDir, wtName string) bool {
	return readLTSMeta(ltsDir).Worktrees[wtName].NamingVerified
}

// markNamingVerified certifies the given dirs (single worktrees or monorepo
// branch subdirs). Writes only when something actually changed.
func markNamingVerified(ltsDir string, names []string) {
	if len(names) == 0 {
		return
	}
	meta := readLTSMeta(ltsDir)
	changed := false
	for _, n := range names {
		m := meta.Worktrees[n]
		if !m.NamingVerified {
			m.NamingVerified = true
			meta.Worktrees[n] = m
			changed = true
		}
	}
	if changed {
		writeLTSMeta(ltsDir, meta)
	}
}

// renameWorktreeMeta follows a worktree directory rename.
func renameWorktreeMeta(ltsDir, oldName, newName string) {
	meta := readLTSMeta(ltsDir)
	if m, ok := meta.Worktrees[oldName]; ok {
		delete(meta.Worktrees, oldName)
		meta.Worktrees[newName] = m
		writeLTSMeta(ltsDir, meta)
	}
}

// removeWorktreeMeta drops a deleted worktree's entry.
func removeWorktreeMeta(ltsDir, wtName string) {
	meta := readLTSMeta(ltsDir)
	if _, ok := meta.Worktrees[wtName]; ok {
		delete(meta.Worktrees, wtName)
		writeLTSMeta(ltsDir, meta)
	}
}
