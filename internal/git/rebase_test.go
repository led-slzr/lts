package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A worktree whose only changes are untracked files creates no stash;
// the rebase must not pop an unrelated stash from the shared stack.
func TestRebaseNeverPopsForeignStash(t *testing.T) {
	scriptDir, _ := hibernateFixture(t, "core")
	core := filepath.Join(scriptDir, "core")
	wt := filepath.Join(scriptDir, "core-lts", "feat-x")
	gitRun(t, core, "worktree", "add", "-b", "feat/x", wt)

	// Someone else's stash, in the main checkout
	writeFileT(t, filepath.Join(core, "README.md"), "main checkout edit\n")
	gitRun(t, core, "stash", "push", "-m", "foreign")

	writeFileT(t, filepath.Join(wt, "scratch.txt"), "untracked only\n")
	if err := RebaseWorktree(wt, "main", ""); err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if list := gitRun(t, core, "stash", "list"); !strings.Contains(list, "foreign") {
		t.Fatalf("foreign stash was consumed: %q", list)
	}
	if st := gitRun(t, wt, "status", "--porcelain"); strings.Contains(st, "README.md") {
		t.Fatalf("foreign changes leaked into the worktree: %q", st)
	}
}

// Tracked changes are stashed and restored even if another stash lands
// on top of the shared stack meanwhile (restore is by SHA, not stash@{0}).
func TestRebaseRestoresOwnStash(t *testing.T) {
	scriptDir, _ := hibernateFixture(t, "core")
	core := filepath.Join(scriptDir, "core")
	wt := filepath.Join(scriptDir, "core-lts", "feat-y")
	gitRun(t, core, "worktree", "add", "-b", "feat/y", wt)
	writeFileT(t, filepath.Join(wt, "README.md"), "my tracked edit\n")
	if err := RebaseWorktree(wt, "main", ""); err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if st := gitRun(t, wt, "status", "--porcelain"); !strings.Contains(st, "README.md") {
		t.Fatalf("own changes not restored: %q", st)
	}
	if list := gitRun(t, core, "stash", "list"); strings.Contains(list, "lts-rebase") {
		t.Fatalf("stash left behind: %q", list)
	}
}

// An interactive rebase the user is in the middle of must not be aborted.
func TestRebaseRefusesOngoingRebase(t *testing.T) {
	scriptDir, _ := hibernateFixture(t, "core")
	core := filepath.Join(scriptDir, "core")
	wt := filepath.Join(scriptDir, "core-lts", "feat-r")
	gitRun(t, core, "worktree", "add", "-b", "feat/r", wt)
	writeFileT(t, filepath.Join(wt, "a.txt"), "a\n")
	gitRun(t, wt, "add", ".")
	gitRun(t, wt, "commit", "-m", "a")
	// Simulate "stopped at edit": an in-progress rebase-merge dir
	gitDir := strings.TrimSpace(gitRun(t, wt, "rev-parse", "--git-dir"))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(wt, gitDir)
	}
	writeFileT(t, filepath.Join(gitDir, "rebase-merge", "head-name"), "refs/heads/feat/r\n")
	err := RebaseWorktree(wt, "main", "")
	if err == nil || !strings.Contains(err.Error(), "ongoing rebase") {
		t.Fatalf("want refusal for an ongoing rebase, got %v", err)
	}
	if _, serr := os.Stat(filepath.Join(gitDir, "rebase-merge")); serr != nil {
		t.Fatal("the user's in-progress rebase was aborted")
	}
}
