package git

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFastReadsMatchGit(t *testing.T) {
	scriptDir, _ := hibernateFixture(t, "core")
	core := filepath.Join(scriptDir, "core")
	wt := filepath.Join(scriptDir, "core-lts", "feat-f")
	gitRun(t, core, "worktree", "add", "-b", "feat/f", wt)
	det := filepath.Join(scriptDir, "core-lts", "det")
	gitRun(t, core, "worktree", "add", "--detach", det)

	for _, p := range []string{core, wt, det} {
		want := strings.TrimSpace(gitRun(t, p, "branch", "--show-current"))
		got, ok := readHeadBranch(p)
		if !ok || got != want {
			t.Errorf("readHeadBranch(%s) = %q,%v; git says %q", filepath.Base(p), got, ok, want)
		}
	}
	// refExists: loose, then packed
	if !refExists(wt, "refs/heads/main") || refExists(wt, "refs/heads/nope") {
		t.Fatal("refExists (loose) wrong")
	}
	gitRun(t, core, "pack-refs", "--all")
	if !refExists(core, "refs/heads/feat/f") || !refExists(wt, "refs/heads/main") || refExists(core, "refs/heads/nope") {
		t.Fatal("refExists (packed) wrong")
	}
	// activity comes from the reflog entry, close to the commit time
	writeFileT(t, filepath.Join(wt, "a.txt"), "a\n")
	gitRun(t, wt, "add", ".")
	gitRun(t, wt, "commit", "-m", "a")
	var ct int64
	fmtSscan(t, gitRun(t, wt, "log", "-1", "--format=%ct"), &ct)
	if ts, ok := headActivity(wt); !ok || ts < ct {
		t.Fatalf("headActivity = %d,%v; commit at %d", ts, ok, ct)
	}
}

func fmtSscan(t *testing.T, s string, v *int64) {
	t.Helper()
	for _, c := range strings.TrimSpace(s) {
		*v = *v*10 + int64(c-'0')
	}
}
