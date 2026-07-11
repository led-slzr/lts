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
