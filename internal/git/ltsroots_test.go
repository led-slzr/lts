package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomLTSRootCreateDiscoverDelete(t *testing.T) {
	scriptDir, _ := hibernateFixture(t, "core")
	root := t.TempDir()
	SetLTSRoots(map[string]string{"core": root})
	t.Cleanup(func() { SetLTSRoots(nil) })

	log := &CreateLog{}
	res, err := CreateSingleRepoWorktree(filepath.Join(scriptDir, "core"), scriptDir, "feat/x", "main",
		WorkspaceOptions{}, false, log)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "core-lts", "feat-x")
	if res.WorktreePath != want {
		t.Fatalf("created at %s, want %s", res.WorktreePath, want)
	}
	if exists(filepath.Join(scriptDir, "core-lts")) {
		t.Fatal("no LTS dir should be created next to the repos")
	}

	repos := DiscoverRepos(scriptDir, mainBasis)
	var found bool
	for _, r := range repos {
		if r.Name == "core" {
			if r.LTSPath != filepath.Join(root, "core-lts") {
				t.Fatalf("LTSPath = %q", r.LTSPath)
			}
			for _, wt := range r.Worktrees {
				if wt.Path == want && wt.Branch == "feat/x" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("discovery must find the worktree under the custom root")
	}
	if NeedsMigration(scriptDir) {
		t.Fatal("fresh custom-root layout must not need migration")
	}

	if err := DeleteWorktree(filepath.Join(scriptDir, "core"), want, "feat/x", true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("worktree should be removed")
	}
}

// A clean, pushed worktree under a custom LTS root must not block hibernate.
func TestHibernateAllowsCustomRootWorktrees(t *testing.T) {
	scriptDir, _ := hibernateFixture(t, "core")
	root := t.TempDir()
	SetLTSRoots(map[string]string{"core": root})
	t.Cleanup(func() { SetLTSRoots(nil) })
	res, err := CreateSingleRepoWorktree(filepath.Join(scriptDir, "core"), scriptDir, "feat/h", "main", WorkspaceOptions{}, false, &CreateLog{})
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, res.WorktreePath, "push", "-u", "origin", "feat/h")
	var repo Repo
	for _, r := range DiscoverRepos(scriptDir, mainBasis) {
		if r.Name == "core" {
			repo = r
		}
	}
	audit := AuditHibernate(scriptDir, repo, "main")
	for _, c := range audit.Checks {
		for _, d := range c.Details {
			if strings.Contains(d, "outside") {
				t.Fatalf("custom-root worktree flagged: %s", d)
			}
		}
	}
	// Relative roots are ignored (would resolve against the cwd)
	SetLTSRoots(map[string]string{"core": "relative/dir"})
	if got := LTSRootFor(scriptDir, "core"); got != scriptDir {
		t.Fatalf("relative root used: %s", got)
	}
}
