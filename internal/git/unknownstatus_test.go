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
