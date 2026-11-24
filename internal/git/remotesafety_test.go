package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A branch whose origin has commits the local copy lacks is NOT merged:
// cleaning it up with --remote would delete a colleague's pushed work.
func TestBehindUpstreamIsNotMergedAndRemoteSurvives(t *testing.T) {
	scriptDir, _ := hibernateFixture(t, "core")
	core := filepath.Join(scriptDir, "core")
	wt := filepath.Join(scriptDir, "core-lts", "feat-a")
	gitRun(t, core, "worktree", "add", "-b", "feat/a", wt)
	gitRun(t, wt, "push", "-u", "origin", "feat/a")

	// A colleague pushes to feat/a from another clone
	origin := strings.TrimSpace(gitRun(t, core, "remote", "get-url", "origin"))
	other := filepath.Join(t.TempDir(), "other")
	gitRun(t, filepath.Dir(other), "clone", origin, other)
	gitRun(t, other, "checkout", "feat/a")
	writeFileT(t, filepath.Join(other, "theirs.txt"), "colleague work\n")
	gitRun(t, other, "add", ".")
	gitRun(t, other, "commit", "-m", "colleague")
	gitRun(t, other, "push", "origin", "feat/a")
	gitRun(t, wt, "fetch", "origin")

	if st, txt := GetWorktreeStatus(wt, "main"); st == StatusMergedCleanable {
		t.Fatalf("behind-upstream branch with unmerged remote commits reported %s (%s)", st.Slug(), txt)
	}
	if n, _, _ := CleanupMergedCleanables(scriptDir, mainBasis, true, nil); n != 0 {
		t.Fatalf("cleanup removed %d worktree(s)", n)
	}
	// Even a direct delete with deleteRemote must not drop unmerged remote work
	if err := DeleteWorktree(core, wt, "feat/a", true, true); err != nil {
		t.Fatal(err)
	}
	if out := gitRun(t, other, "ls-remote", "--heads", "origin", "feat/a"); !strings.Contains(out, "feat/a") {
		t.Fatal("origin/feat/a (holding a colleague's commit) was deleted")
	}
	_ = os.Remove
}
