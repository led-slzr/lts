package git

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StaleWorktree is a worktree git still has registered whose folder is
// gone (deleted by hand). Its branch is kept by git but stays "checked
// out" until pruned, which blocks creating a new worktree for it.
type StaleWorktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	InLTS  bool   `json:"in_lts"` // the path was inside an LTS (-lts) folder
	Locked bool   `json:"locked"` // `git worktree lock`ed — prune leaves it
}

// StaleWorktrees lists registered-but-missing worktrees of a repo by
// reading its .git/worktrees/* admin dirs (no git process). An admin dir
// still referenced by a live LTS worktree folder (one that was moved and
// whose link git hasn't been told about) is NOT stale — see RepairLinks.
func StaleWorktrees(scriptDir, repoPath string) []StaleWorktree {
	admin := filepath.Join(commonDirOf(repoPath), "worktrees")
	entries, err := os.ReadDir(admin)
	if err != nil {
		return nil
	}
	live := liveAdminDirs(scriptDir, repoPath)
	var out []StaleWorktree
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(admin, e.Name())
		if live[filepath.Clean(dir)] != "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "gitdir"))
		if err != nil {
			continue
		}
		dotgit := strings.TrimSpace(string(data))
		if !filepath.IsAbs(dotgit) {
			dotgit = filepath.Join(dir, dotgit)
		}
		wtPath := filepath.Dir(filepath.Clean(dotgit))
		if _, err := os.Stat(wtPath); err == nil || !os.IsNotExist(err) {
			continue // folder still there (or unknowable) — not stale
		}
		s := StaleWorktree{Path: wtPath}
		if head, err := os.ReadFile(filepath.Join(dir, "HEAD")); err == nil {
			s.Branch = strings.TrimPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/")
			if strings.HasPrefix(s.Branch, "ref: ") || len(s.Branch) == 40 && !strings.Contains(s.Branch, "/") && isHex(s.Branch) {
				s.Branch = "" // detached or exotic
			}
		}
		for p := wtPath; p != filepath.Dir(p); p = filepath.Dir(p) {
			if strings.HasSuffix(filepath.Base(p), "-lts") {
				s.InLTS = true
				break
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "locked")); err == nil {
			s.Locked = true
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return s != ""
}

// PruneStale prunes a repo's registered-but-missing worktrees (git
// worktree prune: admin metadata only — branches and commits are kept)
// and removes LTS leftovers next to them (workspace file, created-at
// metadata, empty LTS dirs). Returns what was pruned.
func PruneStale(scriptDir, repoPath string, log LogFunc) ([]StaleWorktree, error) {
	if log == nil {
		log = noopLog
	}
	RepairLinks(scriptDir, repoPath)
	before := StaleWorktrees(scriptDir, repoPath)
	if len(before) == 0 {
		return nil, nil
	}
	ctx := filepath.Base(repoPath)
	log(ctx, "Pruning stale worktree records", false)
	if _, err := RunGit(repoPath, "worktree", "prune"); err != nil {
		return nil, GitErr(err)
	}
	still := map[string]bool{}
	for _, s := range StaleWorktrees(scriptDir, repoPath) {
		still[s.Path] = true
	}
	var pruned []StaleWorktree
	for _, s := range before {
		if still[s.Path] {
			continue // locked, or git kept it
		}
		pruned = append(pruned, s)
		if s.InLTS {
			parent := filepath.Dir(s.Path)
			os.Remove(filepath.Join(parent, filepath.Base(s.Path)+".code-workspace"))
			removeWorktreeMeta(parent, filepath.Base(s.Path))
			cleanEmptyLTSDirs(parent)
		}
		log(ctx, "Pruned "+s.Path, false)
	}
	return pruned, nil
}

// liveAdminDirs maps each of repoPath's worktree admin dirs that a live
// LTS worktree folder points at (via its .git file) to that folder.
func liveAdminDirs(scriptDir, repoPath string) map[string]string {
	adminRoot := filepath.Clean(filepath.Join(commonDirOf(repoPath), "worktrees"))
	out := map[string]string{}
	visit := func(p string) {
		if !isWorktreeDir(p) {
			return
		}
		if gd := gitDirOf(p); gd != "" && filepath.Dir(gd) == adminRoot {
			out[gd] = p
		}
	}
	for _, d := range allLTSDirs(scriptDir) {
		lts := filepath.Join(d.root, d.name)
		entries, _ := os.ReadDir(lts)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(lts, e.Name())
			visit(p)
			sub, _ := os.ReadDir(p) // mono group members / nested layouts
			for _, se := range sub {
				if se.IsDir() {
					visit(filepath.Join(p, se.Name()))
				}
			}
		}
	}
	return out
}

// RepairLinks re-points git's worktree records at LTS worktree folders
// that were moved (rename, mono convert) without git noticing. Run it
// before any `git worktree prune`: prune deletes the record of a worktree
// whose recorded path is gone, which would orphan a moved-but-live one.
func RepairLinks(scriptDir, repoPath string) {
	var stale []string
	for admin, wt := range liveAdminDirs(scriptDir, repoPath) {
		data, err := os.ReadFile(filepath.Join(admin, "gitdir"))
		if err != nil || filepath.Dir(filepath.Clean(strings.TrimSpace(string(data)))) != filepath.Clean(wt) {
			stale = append(stale, wt)
		}
	}
	if len(stale) == 0 {
		return
	}
	sort.Strings(stale)
	RunGit(repoPath, append([]string{"worktree", "repair"}, stale...)...)
}

// pruneWorktrees is the only safe way to run `git worktree prune`.
func pruneWorktrees(scriptDir, repoPath string) {
	RepairLinks(scriptDir, repoPath)
	RunGit(repoPath, "worktree", "prune")
}
