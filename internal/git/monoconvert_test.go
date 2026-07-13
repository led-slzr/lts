package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoRepoFixture builds two repos (core, erp) with bare origins and a
// pushed main each.
func twoRepoFixture(t *testing.T) (scriptDir string) {
	t.Helper()
	scriptDir, _ = hibernateFixture(t, "core")
	origin := filepath.Join(scriptDir, ".origins", "erp.git")
	if err := os.MkdirAll(origin, 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, origin, "init", "--bare")
	gitRun(t, scriptDir, "clone", origin, "erp")
	erp := filepath.Join(scriptDir, "erp")
	writeFileT(t, filepath.Join(erp, "README.md"), "erp\n")
	gitRun(t, erp, "add", ".")
	gitRun(t, erp, "commit", "-m", "init")
	gitRun(t, erp, "branch", "-M", "main")
	gitRun(t, erp, "push", "-u", "origin", "main")
	return scriptDir
}

func mainBasis(string) string { return "main" }

func TestConvertToMonoCreatesPartnerAndMovesInitiator(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	core := filepath.Join(scriptDir, "core")

	// A single-repo worktree the user made before realizing it should be mono.
	wtPath := filepath.Join(scriptDir, "core-lts", "core-feat-login")
	gitRun(t, core, "worktree", "add", "-b", "feat/login", wtPath)
	writeFileT(t, filepath.Join(scriptDir, "core-lts", "core-feat-login.code-workspace"), "{}")
	writeFileT(t, filepath.Join(wtPath, "wip.txt"), "uncommitted work survives the move\n")

	res, err := ConvertToMono(scriptDir, "core", wtPath, "feat/login",
		[]ConvertPartner{{Name: "erp"}}, mainBasis, WorkspaceOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}

	group := filepath.Join(scriptDir, "core-erp-lts", "feat-login")
	if res.BranchSubdir != group {
		t.Fatalf("group at %s, want %s", res.BranchSubdir, group)
	}
	// Initiator moved with its dirty file intact; partner freshly created.
	if data, _ := os.ReadFile(filepath.Join(group, "core-feat-login", "wip.txt")); !strings.Contains(string(data), "survives") {
		t.Fatal("uncommitted work must survive the move")
	}
	if !isWorktreeDir(filepath.Join(group, "erp-feat-login")) {
		t.Fatal("partner worktree should be created in the group")
	}
	if b := gitRun(t, filepath.Join(group, "erp-feat-login"), "branch", "--show-current"); strings.TrimSpace(b) != "feat/login" {
		t.Fatalf("partner branch = %q", b)
	}
	// git still tracks the moved worktree (worktree move, not a raw rename).
	if out := gitRun(t, core, "worktree", "list"); !strings.Contains(out, filepath.Join("feat-login", "core-feat-login")) {
		t.Fatalf("git lost track of the moved worktree:\n%s", out)
	}
	// Old single-repo location fully cleaned (worktree, workspace, dir).
	if _, err := os.Stat(filepath.Join(scriptDir, "core-lts")); !os.IsNotExist(err) {
		t.Fatal("emptied single-repo LTS dir should dissolve")
	}
	// Group metadata + workspace in place.
	if data, _ := os.ReadFile(filepath.Join(scriptDir, "core-erp-lts", ".lts-repos")); !strings.Contains(string(data), "core") || !strings.Contains(string(data), "erp") {
		t.Fatalf(".lts-repos = %q", data)
	}
	if _, err := os.Stat(filepath.Join(group, "monorepo-feat-login.code-workspace")); err != nil {
		t.Fatal("monorepo workspace file missing")
	}
	// Discovery sees the new group card.
	foundGroup := false
	for _, r := range DiscoverRepos(scriptDir, mainBasis) {
		if r.IsMonorepo && r.Name == "core-erp" && len(r.Worktrees) == 1 {
			foundGroup = true
		}
	}
	if !foundGroup {
		t.Fatal("discovery should show the core-erp group with one worktree")
	}
}

func TestConvertToMonoAdoptsExistingSameBranchWorktree(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	core, erp := filepath.Join(scriptDir, "core"), filepath.Join(scriptDir, "erp")

	coreWT := filepath.Join(scriptDir, "core-lts", "core-feat-x")
	gitRun(t, core, "worktree", "add", "-b", "feat/x", coreWT)
	erpWT := filepath.Join(scriptDir, "erp-lts", "erp-feat-x")
	gitRun(t, erp, "worktree", "add", "-b", "feat/x", erpWT)
	writeFileT(t, filepath.Join(erpWT, "erp-wip.txt"), "adopted work\n")

	res, err := ConvertToMono(scriptDir, "core", coreWT, "feat/x",
		[]ConvertPartner{{Name: "erp", AdoptPath: erpWT}}, mainBasis, WorkspaceOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Adopted) != 1 || res.Adopted[0] != "erp" {
		t.Fatalf("adopted = %v", res.Adopted)
	}
	moved := filepath.Join(scriptDir, "core-erp-lts", "feat-x", "erp-feat-x")
	if data, _ := os.ReadFile(filepath.Join(moved, "erp-wip.txt")); !strings.Contains(string(data), "adopted") {
		t.Fatal("adopted worktree's work must survive")
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "erp-lts")); !os.IsNotExist(err) {
		t.Fatal("adopted partner's emptied LTS dir should dissolve")
	}
}

func TestConvertRefusesWhenNothingValid(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	core := filepath.Join(scriptDir, "core")
	wtPath := filepath.Join(scriptDir, "core-lts", "core-feat-y")
	gitRun(t, core, "worktree", "add", "-b", "feat/y", wtPath)

	// Unknown partner repo: validation fails BEFORE anything moves.
	if _, err := ConvertToMono(scriptDir, "core", wtPath, "feat/y",
		[]ConvertPartner{{Name: "ghost"}}, mainBasis, WorkspaceOptions{}, false); err == nil {
		t.Fatal("unknown partner must fail")
	}
	if !isWorktreeDir(wtPath) {
		t.Fatal("failed validation must leave the initiator untouched")
	}
}

func TestReduceMonoKeepAndDelete(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	core := filepath.Join(scriptDir, "core")
	coreWT := filepath.Join(scriptDir, "core-lts", "core-feat-z")
	gitRun(t, core, "worktree", "add", "-b", "feat/z", coreWT)
	writeFileT(t, filepath.Join(coreWT, "keepme.txt"), "kept work\n")

	res, err := ConvertToMono(scriptDir, "core", coreWT, "feat/z",
		[]ConvertPartner{{Name: "erp"}}, mainBasis, WorkspaceOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}

	// Split: keep core (has work), delete erp (fresh, clean).
	group := res.BranchSubdir
	kept, deleted, err := ReduceMono(scriptDir, group, []ReduceDecision{
		{Name: "core", WtPath: filepath.Join(group, "core-feat-z"), Branch: "feat/z", Delete: false},
		{Name: "erp", WtPath: filepath.Join(group, "erp-feat-z"), Branch: "feat/z", Delete: true},
	}, WorkspaceOptions{})
	if err != nil || kept != 1 || deleted != 1 {
		t.Fatalf("kept=%d deleted=%d err=%v", kept, deleted, err)
	}

	// core is a single-repo worktree again, work and git tracking intact.
	single := filepath.Join(scriptDir, "core-lts", "core-feat-z")
	if data, _ := os.ReadFile(filepath.Join(single, "keepme.txt")); !strings.Contains(string(data), "kept") {
		t.Fatal("kept constituent's work must survive the move out")
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "core-lts", "core-feat-z.code-workspace")); err != nil {
		t.Fatal("kept constituent should get an individual workspace")
	}
	// erp's worktree and local branch are gone; the group dissolved.
	if out := gitRun(t, filepath.Join(scriptDir, "erp"), "branch", "--list", "feat/z"); strings.TrimSpace(out) != "" {
		t.Fatal("deleted constituent's local branch should be gone")
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "core-erp-lts")); !os.IsNotExist(err) {
		t.Fatal("emptied group dir should dissolve")
	}
	// Discovery: core has its single worktree back, no ghost group.
	for _, r := range DiscoverRepos(scriptDir, mainBasis) {
		if r.IsMonorepo {
			t.Fatalf("no group card should remain, got %s", r.Name)
		}
		if r.Name == "core" && len(r.Worktrees) != 1 {
			t.Fatalf("core should have exactly its kept worktree, got %d", len(r.Worktrees))
		}
	}
}

// Every partner failing must roll the initiator back to its original home —
// never stranded in a half-born group.
func TestConvertRollsBackWhenAllPartnersFail(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	core := filepath.Join(scriptDir, "core")
	wtPath := filepath.Join(scriptDir, "core-lts", "core-feat-r")
	gitRun(t, core, "worktree", "add", "-b", "feat/r", wtPath)
	writeFileT(t, filepath.Join(wtPath, "wip.txt"), "precious\n")

	// erp already has feat/r checked out in a worktree the dialog didn't
	// know about — git refuses a second checkout, so the create fails.
	gitRun(t, filepath.Join(scriptDir, "erp"), "worktree", "add",
		filepath.Join(scriptDir, ".elsewhere-feat-r"), "-b", "feat/r")

	_, err := ConvertToMono(scriptDir, "core", wtPath, "feat/r",
		[]ConvertPartner{{Name: "erp"}}, mainBasis, WorkspaceOptions{}, false)
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected rollback error, got %v", err)
	}
	// Initiator is back, work intact, workspace regenerated, group gone.
	if data, _ := os.ReadFile(filepath.Join(wtPath, "wip.txt")); !strings.Contains(string(data), "precious") {
		t.Fatal("initiator must be back at its original path with work intact")
	}
	if out := gitRun(t, core, "worktree", "list"); !strings.Contains(out, "core-lts") {
		t.Fatalf("git should track the restored location:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "core-lts", "core-feat-r.code-workspace")); err != nil {
		t.Fatal("individual workspace should be regenerated on rollback")
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "core-erp-lts")); !os.IsNotExist(err) {
		t.Fatal("the half-born group dir must dissolve")
	}
}

// Converting a branch that already has a group subdir fails before any move.
func TestConvertRefusesExistingGroupBranch(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	core := filepath.Join(scriptDir, "core")
	wtPath := filepath.Join(scriptDir, "core-lts", "core-feat-dup")
	gitRun(t, core, "worktree", "add", "-b", "feat/dup", wtPath)
	if err := os.MkdirAll(filepath.Join(scriptDir, "core-erp-lts", "feat-dup"), 0755); err != nil {
		t.Fatal(err)
	}

	_, err := ConvertToMono(scriptDir, "core", wtPath, "feat/dup",
		[]ConvertPartner{{Name: "erp"}}, mainBasis, WorkspaceOptions{}, false)
	if err == nil || !strings.Contains(err.Error(), "already has") {
		t.Fatalf("expected existing-group refusal, got %v", err)
	}
	if !isWorktreeDir(wtPath) {
		t.Fatal("refusal must leave the initiator untouched")
	}
}

// All-delete split dissolves the whole group with nothing left behind.
func TestReduceMonoAllDeleteDissolvesGroup(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	core := filepath.Join(scriptDir, "core")
	coreWT := filepath.Join(scriptDir, "core-lts", "core-feat-gone")
	gitRun(t, core, "worktree", "add", "-b", "feat/gone", coreWT)

	res, err := ConvertToMono(scriptDir, "core", coreWT, "feat/gone",
		[]ConvertPartner{{Name: "erp"}}, mainBasis, WorkspaceOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	group := res.BranchSubdir
	kept, deleted, err := ReduceMono(scriptDir, group, []ReduceDecision{
		{Name: "core", WtPath: filepath.Join(group, "core-feat-gone"), Branch: "feat/gone", Delete: true},
		{Name: "erp", WtPath: filepath.Join(group, "erp-feat-gone"), Branch: "feat/gone", Delete: true},
	}, WorkspaceOptions{})
	if err != nil || kept != 0 || deleted != 2 {
		t.Fatalf("kept=%d deleted=%d err=%v", kept, deleted, err)
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "core-erp-lts")); !os.IsNotExist(err) {
		t.Fatal("group dir must dissolve after all-delete")
	}
	for _, r := range DiscoverRepos(scriptDir, mainBasis) {
		if r.IsMonorepo || len(r.Worktrees) != 0 {
			t.Fatalf("nothing should remain, got %s with %d wts", r.Name, len(r.Worktrees))
		}
	}
}
