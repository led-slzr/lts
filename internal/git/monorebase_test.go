package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRebaseMonoGroup(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	res, err := CreateMonorepoWorktrees([]string{"core", "erp"}, scriptDir, "feat/g", mainBasis, map[string]bool{}, WorkspaceOptions{}, &CreateLog{})
	if err != nil {
		t.Fatal(err)
	}
	group := filepath.Dir(res[0].WorktreePath)
	none := func(string) string { return "" }
	// main advances in core only
	writeFileT(t, filepath.Join(scriptDir, "core", "up.txt"), "upstream\n")
	gitRun(t, filepath.Join(scriptDir, "core"), "add", ".")
	gitRun(t, filepath.Join(scriptDir, "core"), "commit", "-m", "up")

	out, err := RebaseMonoGroup(group, "feat/g", mainBasis, none)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range out {
		got[r.Repo] = r.Result
	}
	if got["core"] != "rebased" || got["erp"] != "up_to_date" {
		t.Fatalf("results = %v", got)
	}
	if _, err := os.Stat(filepath.Join(group, "core-feat-g", "up.txt")); err != nil {
		t.Fatal("core member should have main's new commit")
	}

	// A conflict in the first member stops the rest, and is restored
	coreWT := filepath.Join(group, "core-feat-g")
	writeFileT(t, filepath.Join(coreWT, "README.md"), "branch side\n")
	gitRun(t, coreWT, "commit", "-am", "branch")
	writeFileT(t, filepath.Join(scriptDir, "core", "README.md"), "main side\n")
	gitRun(t, filepath.Join(scriptDir, "core"), "commit", "-am", "main")
	out, err = RebaseMonoGroup(group, "feat/g", mainBasis, none)
	if err == nil {
		t.Fatal("want a conflict error")
	}
	if out[0].Result != "conflict" || out[1].Result != "skipped" {
		t.Fatalf("results = %+v", out)
	}
	if err := CheckOngoingOperations(coreWT); err != nil {
		t.Fatal("conflicting member must be left with no rebase in progress")
	}
	// Preflight: member off the group branch → refuse before touching anything
	gitRun(t, filepath.Join(group, "erp-feat-g"), "checkout", "-b", "other")
	if _, err := RebaseMonoGroup(group, "feat/g", mainBasis, none); err == nil {
		t.Fatal("must refuse when a member is off the group branch")
	}
}
