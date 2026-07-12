package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Simulation matrix for the hibernate audit: each test builds a real repo
// in the state a user could be in and asserts the audit's verdict.

// failDetails joins every failing check's details for assertions.
func failDetails(a HibernateAudit) string {
	var out []string
	for _, c := range a.Checks {
		if c.State == CheckFail {
			out = append(out, c.Label)
			out = append(out, c.Details...)
		}
	}
	return strings.Join(out, " | ")
}

func TestAuditAheadAndBehindBranchBlocks(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	// Diverge: local commit on main + remote commit via a second clone.
	other := filepath.Join(scriptDir, "other")
	gitRun(t, scriptDir, "clone", filepath.Join(scriptDir, ".origins", "core.git"), "other")
	writeFileT(t, filepath.Join(other, "remote.txt"), "r\n")
	gitRun(t, other, "add", ".")
	gitRun(t, other, "commit", "-m", "remote work")
	gitRun(t, other, "push")
	os.RemoveAll(other)

	writeFileT(t, filepath.Join(repoPath, "local.txt"), "l\n")
	gitRun(t, repoPath, "add", ".")
	gitRun(t, repoPath, "commit", "-m", "local work")

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if audit.Blockers() == 0 {
		t.Fatalf("diverged main (ahead+behind) must block: %+v", audit.Checks)
	}
	if !strings.Contains(failDetails(audit), "main") {
		t.Fatalf("expected main named: %s", failDetails(audit))
	}
}

func TestAuditBehindOnlyBranchPasses(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	// Remote moves ahead; local main is merely behind — GitHub has more, safe.
	other := filepath.Join(scriptDir, "other")
	gitRun(t, scriptDir, "clone", filepath.Join(scriptDir, ".origins", "core.git"), "other")
	writeFileT(t, filepath.Join(other, "remote.txt"), "r\n")
	gitRun(t, other, "add", ".")
	gitRun(t, other, "commit", "-m", "remote work")
	gitRun(t, other, "push")
	os.RemoveAll(other)

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if n := audit.Blockers(); n != 0 {
		t.Fatalf("behind-only branch must pass, got %d blockers: %+v", n, audit.Checks)
	}
}

func TestAuditUpstreamGoneMergedPassesUnmergedBlocks(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	// Merged-then-remote-deleted (the everyday post-PR state): branch at
	// main's tip, upstream removed.
	gitRun(t, repoPath, "checkout", "-b", "feat/merged")
	gitRun(t, repoPath, "push", "-u", "origin", "feat/merged")
	gitRun(t, repoPath, "checkout", "main")
	gitRun(t, repoPath, "push", "origin", "--delete", "feat/merged")

	// Unmerged with deleted upstream: real commits only the local copy has.
	gitRun(t, repoPath, "checkout", "-b", "feat/lost")
	writeFileT(t, filepath.Join(repoPath, "lost.txt"), "x\n")
	gitRun(t, repoPath, "add", ".")
	gitRun(t, repoPath, "commit", "-m", "lost work")
	gitRun(t, repoPath, "push", "-u", "origin", "feat/lost")
	gitRun(t, repoPath, "checkout", "main")
	gitRun(t, repoPath, "push", "origin", "--delete", "feat/lost")

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	details := failDetails(audit)
	if strings.Contains(details, "feat/merged") {
		t.Fatalf("merged branch with deleted upstream must pass: %s", details)
	}
	if !strings.Contains(details, "feat/lost") {
		t.Fatalf("unmerged branch with deleted upstream must block: %+v", audit.Checks)
	}
}

func TestAuditRespectsBasisBranch(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	// dev basis (gorocky-style): branch merged into origin/dev but NOT main.
	gitRun(t, repoPath, "checkout", "-b", "dev")
	writeFileT(t, filepath.Join(repoPath, "dev.txt"), "d\n")
	gitRun(t, repoPath, "add", ".")
	gitRun(t, repoPath, "commit", "-m", "dev work")
	gitRun(t, repoPath, "push", "-u", "origin", "dev")
	gitRun(t, repoPath, "branch", "feat/on-dev") // local-only, at dev's tip
	gitRun(t, repoPath, "checkout", "main")

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "dev")
	if n := audit.Blockers(); n != 0 {
		t.Fatalf("branch merged into origin/dev must pass under basis dev: %+v", audit.Checks)
	}
	// Sanity: under basis main the same branch is unmerged and must block.
	audit = AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if !strings.Contains(failDetails(audit), "feat/on-dev") {
		t.Fatalf("same branch must block under basis main: %+v", audit.Checks)
	}
}

func TestAuditUntrackedFileInMainBlocks(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	writeFileT(t, filepath.Join(repoPath, "notes.md"), "precious untracked notes\n")

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if audit.Blockers() == 0 {
		t.Fatalf("untracked file must block (it isn't on GitHub): %+v", audit.Checks)
	}
}

func TestAuditDetachedHead(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	// Detached at a pushed commit: nothing to lose.
	gitRun(t, repoPath, "checkout", "--detach", "HEAD")
	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if n := audit.Blockers(); n != 0 {
		t.Fatalf("detached at pushed commit must pass: %+v", audit.Checks)
	}
	// Detached with a new commit on no branch: only the reflog knows it.
	writeFileT(t, filepath.Join(repoPath, "d.txt"), "d\n")
	gitRun(t, repoPath, "add", ".")
	gitRun(t, repoPath, "commit", "-m", "detached work")
	audit = AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if audit.Blockers() == 0 {
		t.Fatalf("detached commit must block: %+v", audit.Checks)
	}
}

func TestAuditFetchFailureBlocks(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	gitRun(t, repoPath, "remote", "set-url", "origin", filepath.Join(scriptDir, "no-such-remote.git"))

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if audit.Blockers() == 0 {
		t.Fatal("unreachable origin must block — remote freshness can't be verified")
	}
}

func TestAuditMissingWorktreeDirSkipped(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	repo := Repo{Name: "core", Path: repoPath, Worktrees: []Worktree{
		{Name: "feat/gone", Branch: "feat/gone", Path: filepath.Join(scriptDir, "core-lts", "core-feat-gone")},
	}}
	audit := AuditHibernate(scriptDir, repo, "main")
	if n := audit.Blockers(); n != 0 {
		t.Fatalf("missing worktree dir has nothing to lose, must pass: %+v", audit.Checks)
	}
}

// A worktree git knows about but the UI snapshot doesn't (created outside
// LTS, or a snapshot raced a reload): hibernate deletes the main .git, so
// dirty work there is stranded. The audit must find it from git's own
// worktree list, not trust the caller's snapshot.
func TestAuditFindsWorktreeMissingFromSnapshot(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	wtPath := filepath.Join(scriptDir, "core-lts", "core-feat-hidden")
	gitRun(t, repoPath, "worktree", "add", "-b", "feat/hidden", wtPath)
	gitRun(t, wtPath, "push", "-u", "origin", "feat/hidden")
	writeFileT(t, filepath.Join(wtPath, "wip.txt"), "uncommitted\n")

	// Snapshot passes NO worktrees — the audit must still find the dirt.
	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if audit.Blockers() == 0 {
		t.Fatalf("dirty worktree unknown to the snapshot must block: %+v", audit.Checks)
	}
}

// A worktree living outside the working dir isn't deleted by hibernate,
// but deleting the main .git orphans it — hard blocker, even when clean.
func TestAuditExternalWorktreeBlocks(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	outside := filepath.Join(t.TempDir(), "elsewhere")
	gitRun(t, repoPath, "worktree", "add", "-b", "feat/ext", outside)
	gitRun(t, outside, "push", "-u", "origin", "feat/ext")

	audit := AuditHibernate(scriptDir, Repo{Name: "core", Path: repoPath}, "main")
	if !strings.Contains(failDetails(audit), "feat/ext") {
		t.Fatalf("external worktree must block: %+v", audit.Checks)
	}
}

func TestScanEnvsSkipsNodeModules(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	_ = scriptDir
	writeFileT(t, filepath.Join(repoPath, "node_modules", "pkg", ".env"), "NOPE=1\n")
	writeFileT(t, filepath.Join(repoPath, ".env"), "YES=1\n")

	files := scanHibernateEnvs(repoPath, gitWorktrees(repoPath))
	if len(files) != 1 || files[0].Rel != ".env" {
		t.Fatalf("node_modules envs must be skipped, got %+v", files)
	}
}

func TestBackupPruneKeepsNewestFive(t *testing.T) {
	backupRoot := t.TempDir()
	src := filepath.Join(t.TempDir(), ".env")
	writeFileT(t, src, "X=1\n")
	repoDir := filepath.Join(backupRoot, "core")
	// Six pre-existing stamps, then one real backup — oldest two must go.
	for _, stamp := range []string{"20250101-000000", "20250102-000000", "20250103-000000",
		"20250104-000000", "20250105-000000", "20250106-000000"} {
		writeFileT(t, filepath.Join(repoDir, stamp, ".env"), "old\n")
	}
	if _, _, err := BackupEnvs([]EnvFile{{Src: src, Rel: ".env"}}, backupRoot, "core"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(repoDir)
	if len(entries) != envBackupKeep {
		t.Fatalf("expected %d backups kept, got %d", envBackupKeep, len(entries))
	}
	if _, err := os.Stat(filepath.Join(repoDir, "20250101-000000")); !os.IsNotExist(err) {
		t.Fatal("oldest backup should have been pruned")
	}
	// The newest (just-created) backup must survive.
	dir, _, _, ok := LatestEnvBackup(backupRoot, "core")
	if !ok || filepath.Base(dir) == "20250106-000000" {
		t.Fatalf("latest should be the fresh backup, got %s ok=%v", dir, ok)
	}
}

func TestBackupUnreadableSourceFailsBeforeAnythingWritten(t *testing.T) {
	backupRoot := t.TempDir()
	_, _, err := BackupEnvs([]EnvFile{{Src: filepath.Join(backupRoot, "vanished", ".env"), Rel: ".env"}}, backupRoot, "core")
	if err == nil {
		t.Fatal("unreadable source must error so the hibernate aborts")
	}
}

func TestHibernateRepoWithoutLTSDir(t *testing.T) {
	scriptDir, repoPath := hibernateFixture(t, "core")
	if err := HibernateRepo(scriptDir, Repo{Name: "core", Path: repoPath}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(repoPath); !os.IsNotExist(err) {
		t.Fatal("repo dir should be removed")
	}
}
