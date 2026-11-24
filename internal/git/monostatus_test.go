package git

import (
	"os"
	"path/filepath"
	"testing"
)

// A mono group is only "merged · cleanable" when EVERY member is; one
// member with unpushed work must protect the whole group from cleanup.
func TestMonoGroupStatusIsWorstMember(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	group := filepath.Join(scriptDir, "core-erp-lts", "feat-x")
	coreWT := filepath.Join(group, "core-feat-x")
	erpWT := filepath.Join(group, "erp-feat-x")
	os.MkdirAll(group, 0755)
	writeFileT(t, filepath.Join(scriptDir, "core-erp-lts", ".lts-type"), "monorepo\n")
	writeFileT(t, filepath.Join(scriptDir, "core-erp-lts", ".lts-repos"), "core\nerp\n")
	gitRun(t, filepath.Join(scriptDir, "core"), "worktree", "add", "-b", "feat/x", coreWT)
	gitRun(t, filepath.Join(scriptDir, "erp"), "worktree", "add", "-b", "feat/x", erpWT)
	// core: pushed, no unique commits → merged · cleanable
	gitRun(t, coreWT, "push", "-u", "origin", "feat/x")
	// erp: pushed, then a local-only commit → unpushed work
	gitRun(t, erpWT, "push", "-u", "origin", "feat/x")
	writeFileT(t, filepath.Join(erpWT, "work.txt"), "precious\n")
	gitRun(t, erpWT, "add", ".")
	gitRun(t, erpWT, "commit", "-m", "unpushed work")

	var mono *Repo
	for _, r := range DiscoverRepos(scriptDir, mainBasis) {
		if r.IsMonorepo {
			rr := r
			mono = &rr
		}
	}
	if mono == nil || len(mono.Worktrees) != 1 {
		t.Fatalf("mono group not discovered: %+v", mono)
	}
	if st := mono.Worktrees[0].Status; st == StatusMergedCleanable {
		t.Fatalf("group with an unpushed member reported %s", st.Slug())
	}

	if n, _, _ := CleanupMergedCleanables(scriptDir, mainBasis, false, nil); n != 0 {
		t.Fatalf("cleanup removed %d worktree(s) holding unpushed work", n)
	}
	if _, err := os.Stat(filepath.Join(erpWT, "work.txt")); err != nil {
		t.Fatal("unpushed work was destroyed")
	}
}

// A member switched to another branch must not let the group look safe:
// deleting the group force-deletes the group branch in every repo.
func TestMonoMemberOffGroupBranchIsDangerous(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	group := filepath.Join(scriptDir, "core-erp-lts", "feat-m")
	os.MkdirAll(group, 0755)
	writeFileT(t, filepath.Join(scriptDir, "core-erp-lts", ".lts-type"), "monorepo\n")
	writeFileT(t, filepath.Join(scriptDir, "core-erp-lts", ".lts-repos"), "core\nerp\n")
	coreWT, erpWT := filepath.Join(group, "core-feat-m"), filepath.Join(group, "erp-feat-m")
	gitRun(t, filepath.Join(scriptDir, "core"), "worktree", "add", "-b", "feat/m", coreWT)
	gitRun(t, filepath.Join(scriptDir, "erp"), "worktree", "add", "-b", "feat/m", erpWT)
	gitRun(t, coreWT, "push", "-u", "origin", "feat/m")
	// erp: unpushed commit on feat/m, then switch to a pushed branch
	writeFileT(t, filepath.Join(erpWT, "w.txt"), "unpushed\n")
	gitRun(t, erpWT, "add", ".")
	gitRun(t, erpWT, "commit", "-m", "unpushed on feat/m")
	gitRun(t, erpWT, "checkout", "-b", "feat/other", "main")
	gitRun(t, erpWT, "push", "-u", "origin", "feat/other")

	var groups []Worktree
	for _, r := range DiscoverRepos(scriptDir, mainBasis) {
		if r.IsMonorepo {
			groups = append(groups, r.Worktrees...)
		}
	}
	if len(groups) != 1 {
		t.Fatalf("want exactly one entry per branch subdir, got %d: %+v", len(groups), groups)
	}
	if groups[0].Branch != "feat/m" && groups[0].Branch != "feat/other" {
		t.Fatalf("unexpected group branch %q", groups[0].Branch)
	}
	if _, dangerous := DeleteRisk(groups[0].Status); !dangerous {
		t.Fatalf("group with a member off its branch reported %s", groups[0].Status.Slug())
	}
	if n, _, _ := CleanupMergedCleanables(scriptDir, mainBasis, false, nil); n != 0 {
		t.Fatalf("cleanup removed %d", n)
	}
}
