package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMonoGroupNamesAreCollisionProof(t *testing.T) {
	cases := map[string][]string{
		"core+erp-ui-lts":           {"erp-ui", "core"},
		"core+kindred%2Blambda-lts": {"kindred+lambda", "core"},
		"kindred+lambda%2Bcore-lts": {"kindred", "lambda+core"},
		"a%25b+c-lts":               {"c", "a%b"},
	}
	for want, repos := range cases {
		if got := MonoGroupDirName(repos); got != want {
			t.Errorf("MonoGroupDirName(%v) = %q, want %q", repos, got, want)
		}
	}
	if MonoGroupDirName([]string{"x", "y-z"}) == MonoGroupDirName([]string{"x-y", "z"}) {
		t.Fatal("the old '-' collision must be gone")
	}
}

// An old "-"-named group dir migrates to "+" naming; its worktrees stay
// registered and keep working through a later prune.
func TestOldGroupDirMigrates(t *testing.T) {
	scriptDir := twoRepoFixture(t) // core, erp
	res, err := CreateMonorepoWorktrees([]string{"core", "erp"}, scriptDir, "feat/g", mainBasis, map[string]bool{}, WorkspaceOptions{}, &CreateLog{})
	if err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(scriptDir, "core+erp-lts")
	oldDir := filepath.Join(scriptDir, "core-erp-lts")
	if filepath.Dir(filepath.Dir(res[0].WorktreePath)) != newDir {
		t.Fatalf("new groups must use + naming, got %s", res[0].WorktreePath)
	}
	// Simulate a pre-v3.2 layout
	if err := os.Rename(newDir, oldDir); err != nil {
		t.Fatal(err)
	}
	RepairLinks(scriptDir, filepath.Join(scriptDir, "core"))
	RepairLinks(scriptDir, filepath.Join(scriptDir, "erp"))
	if !NeedsMigration(scriptDir) {
		t.Fatal("old group naming should need migration")
	}
	renames := MigrateGroupDirNames(scriptDir)
	if len(renames) != 1 || renames[0].Old != oldDir || renames[0].New != newDir {
		t.Fatalf("renames = %+v", renames)
	}
	if NeedsMigration(scriptDir) {
		t.Fatal("migration must be complete")
	}
	// A later create prunes; members must survive
	if _, err := CreateSingleRepoWorktree(filepath.Join(scriptDir, "core"), scriptDir, "feat/other", "main", WorkspaceOptions{}, false, &CreateLog{}); err != nil {
		t.Fatal(err)
	}
	for _, m := range MonoMembers(filepath.Join(newDir, "feat-g")) {
		if b, err := RunGit(m.Path, "branch", "--show-current"); err != nil || b != "feat/g" {
			t.Fatalf("member %s broken after migration: %q %v", m.Repo, b, err)
		}
	}
	found := false
	for _, r := range DiscoverRepos(scriptDir, mainBasis) {
		if r.IsMonorepo && r.Name == "core+erp" && r.LTSDir == "core+erp-lts" && len(r.Worktrees) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("discovery should show the migrated group")
	}
}

// Old group name AND legacy subdir names migrate together in one pass.
func TestOldGroupWithLegacySubdirsMigratesFully(t *testing.T) {
	root := t.TempDir()
	ltsPath, _ := setupMonorepo(t, root, "core-erp-ui-login", "feat/login",
		map[string]string{"core": "core-login", "erp-ui": "erp-ui-login"})
	old := filepath.Join(root, "core-erp-ui-lts")
	if err := os.Rename(ltsPath, old); err != nil {
		t.Fatal(err)
	}
	RepairLinks(root, filepath.Join(root, "core"))
	RepairLinks(root, filepath.Join(root, "erp-ui"))
	MigrateDirectoryStructure(root)
	sub := filepath.Join(root, "core+erp-ui-lts", "feat-login")
	if !exists(filepath.Join(sub, "core-feat-login")) || !exists(filepath.Join(sub, "erp-ui-feat-login")) {
		t.Fatal("group dir and legacy subdirs should both be migrated")
	}
	if b, err := RunGit(filepath.Join(sub, "core-feat-login"), "branch", "--show-current"); err != nil || b != "feat/login" {
		t.Fatalf("migrated member broken: %q %v", b, err)
	}
	if NeedsMigration(root) {
		t.Fatal("second pass must find nothing")
	}
}

// A group must never move into a single repo's LTS dir that shares its name.
func TestGroupRefusesSingleRepoDirWithSameName(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "feat-x"), 0755)
	if err := checkLTSDirOwnership(dir, []string{"core", "erp"}); err == nil {
		t.Fatal("must refuse a dir holding another repo's worktrees")
	}
}
