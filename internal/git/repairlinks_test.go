package git

import (
	"path/filepath"
	"testing"
)

// Renaming a mono group then creating any worktree (which prunes) must not
// unregister the group's worktrees.
func TestMonoRenameThenPruneKeepsWorktrees(t *testing.T) {
	scriptDir := twoRepoFixture(t)
	res, err := CreateMonorepoWorktrees([]string{"core", "erp"}, scriptDir, "feat/m", mainBasis, map[string]bool{}, WorkspaceOptions{}, &CreateLog{})
	if err != nil {
		t.Fatal(err)
	}
	group := filepath.Dir(res[0].WorktreePath)
	rr, err := RenameMonorepoWorktrees(scriptDir, group, []string{"core", "erp"}, "feat/m", "feat/m2", false, "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	// Any create runs `git worktree prune` first
	if _, err := CreateSingleRepoWorktree(filepath.Join(scriptDir, "core"), scriptDir, "feat/other", "main", WorkspaceOptions{}, false, &CreateLog{}); err != nil {
		t.Fatal(err)
	}
	for _, m := range MonoMembers(rr.NewPath) {
		if b, err := RunGit(m.Path, "branch", "--show-current"); err != nil || b != "feat/m2" {
			t.Fatalf("member %s broken after prune: branch=%q err=%v", m.Repo, b, err)
		}
	}
	if s := StaleWorktrees(scriptDir, filepath.Join(scriptDir, "core")); len(s) != 0 {
		t.Fatalf("live worktrees reported stale: %+v", s)
	}
}
