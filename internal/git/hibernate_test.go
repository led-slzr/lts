package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// git runs a git command in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_AUTHOR_DATE=2026-01-01T00:00:00",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_COMMITTER_DATE=2026-01-01T00:00:00")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// hibernateFixture builds scriptDir/<name> cloned from a local bare origin,
// with an initial pushed commit on main, and returns (scriptDir, repoPath).
func hibernateFixture(t *testing.T, name string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	scriptDir := t.TempDir()
	origin := filepath.Join(scriptDir, ".origins", name+".git")
	if err := os.MkdirAll(origin, 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, origin, "init", "--bare")
	repoPath := filepath.Join(scriptDir, name)
	gitRun(t, scriptDir, "clone", origin, name)
	writeFileT(t, filepath.Join(repoPath, "README.md"), "hello\n")
	gitRun(t, repoPath, "add", ".")
	gitRun(t, repoPath, "commit", "-m", "init")
	gitRun(t, repoPath, "branch", "-M", "main")
	gitRun(t, repoPath, "push", "-u", "origin", "main")
	return scriptDir, repoPath
}

func TestAuditHibernateCleanRepoPasses(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	repo := Repo{Name: "core", Path: repoPath}
	audit := AuditHibernate(scriptDir, repo, "main")
	if n := audit.Blockers(); n != 0 {
		t.Fatalf("expected 0 blockers, got %d: %+v", n, audit.Checks)
	}
	if len(audit.EnvFiles) != 0 {
		t.Fatalf("expected no env files, got %+v", audit.EnvFiles)
	}
}

func TestAuditHibernateUnpushedCommitBlocks(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	gitRun(t, repoPath, "checkout", "-b", "feat/x")
	writeFileT(t, filepath.Join(repoPath, "x.txt"), "x\n")
	gitRun(t, repoPath, "add", ".")
	gitRun(t, repoPath, "commit", "-m", "wip")
	gitRun(t, repoPath, "checkout", "main")

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if audit.Blockers() == 0 {
		t.Fatalf("expected a blocker for unpushed branch, checks: %+v", audit.Checks)
	}
	found := false
	for _, c := range audit.Checks {
		if c.State == CheckFail && strings.Contains(strings.Join(c.Details, " "), "feat/x") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected feat/x named in a failing check: %+v", audit.Checks)
	}
}

func TestAuditHibernatePushedBranchAndMergedBranchPass(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	// Pushed, tracking branch — safe.
	gitRun(t, repoPath, "checkout", "-b", "feat/pushed")
	writeFileT(t, filepath.Join(repoPath, "p.txt"), "p\n")
	gitRun(t, repoPath, "add", ".")
	gitRun(t, repoPath, "commit", "-m", "p")
	gitRun(t, repoPath, "push", "-u", "origin", "feat/pushed")
	// Local-only branch fully merged into main (points at main's tip) — safe.
	gitRun(t, repoPath, "checkout", "main")
	gitRun(t, repoPath, "branch", "feat/merged")

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if n := audit.Blockers(); n != 0 {
		t.Fatalf("expected 0 blockers, got %d: %+v", n, audit.Checks)
	}
}

func TestAuditHibernateStashBlocks(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	writeFileT(t, filepath.Join(repoPath, "README.md"), "changed\n")
	gitRun(t, repoPath, "stash")

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if audit.Blockers() == 0 {
		t.Fatalf("expected stash blocker, checks: %+v", audit.Checks)
	}
}

func TestAuditHibernateDirtyWorktreeBlocks(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	wtPath := filepath.Join(scriptDir, "core-lts", "core-feat-y")
	gitRun(t, repoPath, "worktree", "add", "-b", "feat/y", wtPath)
	gitRun(t, wtPath, "push", "-u", "origin", "feat/y")
	writeFileT(t, filepath.Join(wtPath, "dirty.txt"), "dirty\n")

	repo := Repo{Name: "core", Path: repoPath, LTSDir: "core-lts",
		Worktrees: []Worktree{{Name: "feat/y", Branch: "feat/y", Path: wtPath}}}
	audit := AuditHibernate(scriptDir, repo, "main")
	if audit.Blockers() == 0 {
		t.Fatalf("expected dirty-worktree blocker, checks: %+v", audit.Checks)
	}
}

func TestAuditHibernateMonoEntanglementBlocks(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	monoLts := filepath.Join(scriptDir, "core-erp-ui-lts")
	writeFileT(t, filepath.Join(monoLts, ".lts-repos"), "core\nerp-ui\n")
	if err := os.MkdirAll(filepath.Join(monoLts, "core-erp-ui-login"), 0755); err != nil {
		t.Fatal(err)
	}

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	found := false
	for _, c := range audit.Checks {
		if c.State == CheckFail && strings.Contains(strings.Join(c.Details, " "), "core-erp-ui-lts/core-erp-ui-login") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected mono entanglement blocker naming the branch subdir: %+v", audit.Checks)
	}
}

func TestScanHibernateEnvsFindsNestedUntrackedSkipsTracked(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	// Tracked env-like file: on GitHub already, must be skipped.
	writeFileT(t, filepath.Join(repoPath, ".env.example"), "EXAMPLE=1\n")
	gitRun(t, repoPath, "add", ".env.example")
	gitRun(t, repoPath, "commit", "-m", "example")
	gitRun(t, repoPath, "push")
	// Untracked envs: root + nested (genuine monorepo layout).
	writeFileT(t, filepath.Join(repoPath, ".env"), "ROOT=1\n")
	writeFileT(t, filepath.Join(repoPath, "apps", "web", ".env.local"), "WEB=1\n")
	// Worktree with its own env.
	wtPath := filepath.Join(scriptDir, "core-lts", "core-feat-z")
	gitRun(t, repoPath, "worktree", "add", "-b", "feat/z", wtPath)
	gitRun(t, wtPath, "push", "-u", "origin", "feat/z")
	writeFileT(t, filepath.Join(wtPath, "apps", "api", ".env"), "API=1\n")

	files := scanHibernateEnvs(repoPath, gitWorktrees(repoPath))

	rels := make(map[string]bool)
	for _, f := range files {
		rels[f.Rel] = true
	}
	for _, want := range []string{
		".env",
		filepath.Join("apps", "web", ".env.local"),
		filepath.Join("worktrees", "core-feat-z", "apps", "api", ".env"),
	} {
		if !rels[want] {
			t.Errorf("missing env file %q in %v", want, rels)
		}
	}
	if rels[".env.example"] {
		t.Errorf("tracked .env.example must not be scheduled for backup: %v", rels)
	}
}

func TestBackupAndRestoreEnvsRoundTrip(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	writeFileT(t, filepath.Join(repoPath, ".env"), "ROOT=1\n")
	writeFileT(t, filepath.Join(repoPath, "apps", "web", ".env"), "WEB=1\n")
	wtPath := filepath.Join(scriptDir, "core-lts", "core-feat-w")
	gitRun(t, repoPath, "worktree", "add", "-b", "feat/w", wtPath)
	writeFileT(t, filepath.Join(wtPath, ".env"), "WT=1\n")

	backupRoot := filepath.Join(t.TempDir(), "env-backup")

	dest, n, err := BackupEnvs(scanHibernateEnvs(repoPath, gitWorktrees(repoPath)), backupRoot, "core")
	if err != nil || n != 3 {
		t.Fatalf("backup: n=%d err=%v", n, err)
	}

	dir, mainN, wtN, ok := LatestEnvBackup(backupRoot, "core")
	if !ok || dir != dest || mainN != 2 || wtN != 1 {
		t.Fatalf("LatestEnvBackup = (%s, %d, %d, %v), want (%s, 2, 1, true)", dir, mainN, wtN, ok, dest)
	}

	// Simulate re-clone: restore into a fresh dir; worktree envs stay behind.
	fresh := t.TempDir()
	writeFileT(t, filepath.Join(fresh, ".env"), "ALREADY=1\n") // never clobbered
	restored, err := RestoreEnvBackup(dir, fresh)
	if err != nil || restored != 1 {
		t.Fatalf("restore: n=%d err=%v", restored, err)
	}
	if data, _ := os.ReadFile(filepath.Join(fresh, ".env")); string(data) != "ALREADY=1\n" {
		t.Fatalf("existing .env was clobbered: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(fresh, "apps", "web", ".env")); string(data) != "WEB=1\n" {
		t.Fatalf("nested env not restored in place: %q", data)
	}
	if _, err := os.Stat(filepath.Join(fresh, "worktrees")); !os.IsNotExist(err) {
		t.Fatal("worktree envs must not be restored into the repo dir")
	}
}

func TestHibernateRepoRemovesDirsAndGuardsPaths(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	ltsPath := filepath.Join(scriptDir, "core-lts")
	if err := os.MkdirAll(filepath.Join(ltsPath, "core-feat-a"), 0755); err != nil {
		t.Fatal(err)
	}

	// Path guard: a repo path outside scriptDir must be refused.
	outside := t.TempDir()
	if err := HibernateRepo(scriptDir, Repo{Name: "x", Path: filepath.Join(outside, "x")}); err == nil {
		t.Fatal("expected refusal for repo path outside scriptDir")
	}

	repo := Repo{Name: "core", Path: repoPath, LTSDir: "core-lts"}
	if err := HibernateRepo(scriptDir, repo); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{repoPath, ltsPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed", p)
		}
	}
}
