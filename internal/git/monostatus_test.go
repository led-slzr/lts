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
