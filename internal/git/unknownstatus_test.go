package git

import (
	"os"
	"path/filepath"
	"testing"
)

// A worktree folder git can't read must be "unknown" and dangerous —
// never "missing" (safe), which let delete wipe real files with --yes.
func TestUnreadableWorktreeIsUnknownAndDangerous(t *testing.T) {
	scriptDir, _ := hibernateFixture(t, "core")
	core := filepath.Join(scriptDir, "core")
	wt := filepath.Join(scriptDir, "core-lts", "feat-b")
	gitRun(t, core, "worktree", "add", "-b", "feat/b", wt)
	writeFileT(t, filepath.Join(wt, "precious.txt"), "work\n")
	os.RemoveAll(filepath.Join(core, ".git", "worktrees", "feat-b"))

	st, _ := GetWorktreeStatus(wt, "main")
	if st != StatusUnknown {
		t.Fatalf("status = %s, want unknown", st.Slug())
	}
	if _, dangerous := DeleteRisk(st); !dangerous {
		t.Fatal("unknown status must be dangerous to delete")
	}
	os.RemoveAll(wt)
	if st, _ := GetWorktreeStatus(wt, "main"); st != StatusMissing {
		t.Fatalf("a removed folder should be missing, got %s", st.Slug())
	}
}

// Deleting a detached-HEAD worktree must never delete a branch that
// happens to share its folder name.
func TestDetachedWorktreeNeverDeletesNamesakeBranch(t *testing.T) {
	scriptDir, _ := hibernateFixture(t, "core")
	core := filepath.Join(scriptDir, "core")
	wt := filepath.Join(scriptDir, "core-lts", "detached")
	gitRun(t, core, "worktree", "add", "--detach", wt)
	gitRun(t, core, "branch", "detached") // unrelated branch, same name as the folder
	var got *Worktree
	for _, r := range DiscoverRepos(scriptDir, mainBasis) {
		for i := range r.Worktrees {
			if r.Worktrees[i].Path == wt {
				got = &r.Worktrees[i]
			}
		}
	}
	if got == nil || got.Branch != "" {
		t.Fatalf("detached worktree must have no branch, got %+v", got)
	}
	if err := DeleteWorktree(core, wt, got.Branch, true, false); err != nil {
		t.Fatal(err)
	}
	if out := gitRun(t, core, "branch", "--list", "detached"); out == "" {
		t.Fatal("unrelated branch 'detached' was deleted")
	}
	// A detached HEAD with commits on no branch is dangerous
	wt2 := filepath.Join(scriptDir, "core-lts", "orphan")
	gitRun(t, core, "worktree", "add", "--detach", wt2)
	writeFileT(t, filepath.Join(wt2, "o.txt"), "o\n")
	gitRun(t, wt2, "add", ".")
	gitRun(t, wt2, "commit", "-m", "orphan")
	if st, _ := GetWorktreeStatus(wt2, "main"); st != StatusUnknown {
		t.Fatalf("orphan commits on detached HEAD: status %s, want unknown", st.Slug())
	}
}
