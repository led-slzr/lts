package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v in %s: %v\n%s", args, dir, err, out)
	}
}

// initRepo creates a repo with one commit on main.
func initRepo(t *testing.T, root, name string) string {
	t.Helper()
	p := filepath.Join(root, name)
	os.MkdirAll(p, 0755)
	run(t, p, "git", "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(p, "README"), []byte(name), 0644)
	run(t, p, "git", "add", ".")
	run(t, p, "git", "commit", "-q", "-m", "init")
	return p
}

func addWorktree(t *testing.T, repoPath, wtPath, branch string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(wtPath), 0755)
	run(t, repoPath, "git", "worktree", "add", "-q", "-b", branch, wtPath)
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestReplaceToken(t *testing.T) {
	cases := []struct{ in, old, new, want string }{
		{`"path": "login"`, "login", "feat-login", `"path": "feat-login"`},
		{`"path": "feat-login"`, "login", "feat-login", `"path": "feat-login"`}, // idempotent
		{`core - login (login)`, "login", "feat-login", `core - feat-login (feat-login)`},
		{`core-login-2`, "core-login", "core-feat-login", `core-login-2`},
		{`'apps/login/.env'`, "login", "feat-login", `'apps/feat-login/.env'`},
	}
	for _, c := range cases {
		if got := replaceToken(c.in, c.old, c.new); got != c.want {
			t.Errorf("replaceToken(%q,%q,%q) = %q, want %q", c.in, c.old, c.new, got, c.want)
		}
	}
}

func TestReplaceDisplaySuffixIdempotent(t *testing.T) {
	in := `{ "name": "core - queries-only", "path": "core-queries-only" } ... 'Terminal (queries-only)' ${workspaceFolder:core - queries-only}`
	want := `{ "name": "core - led-queries-only", "path": "core-queries-only" } ... 'Terminal (led-queries-only)' ${workspaceFolder:core - led-queries-only}`
	got := replaceDisplaySuffix(in, "queries-only", "led-queries-only")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if again := replaceDisplaySuffix(got, "queries-only", "led-queries-only"); again != got {
		t.Fatalf("not idempotent: %q", again)
	}
}

func TestIsLegacyDirName(t *testing.T) {
	if !isLegacyDirName("core-login", "core", "feat/login") {
		t.Error("core-login should be legacy for feat/login")
	}
	if !isLegacyDirName("core-login-2", "core", "feat/login") {
		t.Error("core-login-2 should be legacy (collision counter)")
	}
	if isLegacyDirName("feat-login", "core", "feat/login") {
		t.Error("feat-login is the new convention, not legacy")
	}
	if isLegacyDirName("feat-old-thing", "core", "fix/new-thing") {
		t.Error("switched branch must not look legacy")
	}
	if isLegacyDirName("core-login-abc", "core", "feat/login") {
		t.Error("non-numeric suffix is not a collision counter")
	}
}

// A single-repo worktree whose branch was switched in place must NOT be renamed.
func TestSingleRepo_BranchSwitchInPlace_NotRenamed(t *testing.T) {
	root := t.TempDir()
	repo := initRepo(t, root, "core")
	ltsPath := filepath.Join(root, "core-lts")
	wt := filepath.Join(ltsPath, "feat-staging-tracker")
	addWorktree(t, repo, wt, "feat/staging-tracker")
	generateIndividualWorkspace(ltsPath, "feat-staging-tracker", "pnpm", "claude", "code", false)
	wsBefore := readFile(t, filepath.Join(ltsPath, "feat-staging-tracker.code-workspace"))

	// Claude Code switches branch inside the worktree
	run(t, wt, "git", "checkout", "-q", "-b", "fix/tracker-basemap")

	if NeedsMigration(root) {
		t.Fatal("NeedsMigration should be false after an in-place branch switch")
	}
	if n := MigrateDirectoryStructure(root); n != 0 {
		t.Fatalf("migrated %d, want 0", n)
	}
	if !exists(wt) {
		t.Fatal("worktree directory was renamed")
	}
	if exists(filepath.Join(ltsPath, "fix-tracker-basemap")) {
		t.Fatal("new-style directory was created")
	}
	if NeedsWorkspaceRepair(root) {
		t.Fatal("workspace should not need repair")
	}
	RepairWorkspaceContents(root)
	if got := readFile(t, filepath.Join(ltsPath, "feat-staging-tracker.code-workspace")); got != wsBefore {
		t.Fatal("workspace file was modified")
	}
}

// A legacy "<repo>-<suffix>" single-repo worktree is still migrated.
func TestSingleRepo_LegacyDir_Migrated(t *testing.T) {
	root := t.TempDir()
	repo := initRepo(t, root, "core")
	ltsPath := filepath.Join(root, "core-lts")
	wt := filepath.Join(ltsPath, "core-login")
	addWorktree(t, repo, wt, "feat/login")
	generateIndividualWorkspace(ltsPath, "core-login", "pnpm", "claude", "code", false)

	if !NeedsMigration(root) {
		t.Fatal("legacy dir should need migration")
	}
	if n := MigrateDirectoryStructure(root); n != 1 {
		t.Fatalf("migrated %d, want 1", n)
	}
	newWt := filepath.Join(ltsPath, "feat-login")
	if !exists(newWt) || exists(wt) {
		t.Fatal("worktree not moved to feat-login")
	}
	ws := readFile(t, filepath.Join(ltsPath, "feat-login.code-workspace"))
	if !strings.Contains(ws, `"path": "feat-login"`) {
		t.Fatalf("workspace path not updated:\n%s", ws)
	}
	if NeedsMigration(root) {
		t.Fatal("still needs migration after migrating")
	}
}

func setupMonorepo(t *testing.T, root, subdir, branch string, wtNames map[string]string) (ltsPath, subdirPath string) {
	t.Helper()
	for _, r := range []string{"core", "erp-ui"} {
		initRepo(t, root, r)
	}
	ltsPath = filepath.Join(root, "core-erp-ui-lts")
	os.MkdirAll(ltsPath, 0755)
	os.WriteFile(filepath.Join(ltsPath, ".lts-type"), []byte("monorepo\n"), 0644)
	os.WriteFile(filepath.Join(ltsPath, ".lts-repos"), []byte("core\nerp-ui\n"), 0644)
	subdirPath = filepath.Join(ltsPath, subdir)
	var pairs []string
	for _, r := range []string{"core", "erp-ui"} {
		addWorktree(t, filepath.Join(root, r), filepath.Join(subdirPath, wtNames[r]), branch)
		pairs = append(pairs, r+":"+wtNames[r])
	}
	return
}

// A monorepo group whose branch was switched in place must be left alone entirely.
func TestMonorepo_BranchSwitchInPlace_NotRenamed(t *testing.T) {
	root := t.TempDir()
	_, subdirPath := setupMonorepo(t, root, "fix-subs", "fix/subs",
		map[string]string{"core": "core-fix-subs", "erp-ui": "erp-ui-fix-subs"})
	generateMonorepoWorkspace(subdirPath, "fix-subs", []string{"core:core-fix-subs", "erp-ui:erp-ui-fix-subs"}, "claude", "code", false)
	wsPath := filepath.Join(subdirPath, "monorepo-fix-subs.code-workspace")
	before := readFile(t, wsPath)

	for _, w := range []string{"core-fix-subs", "erp-ui-fix-subs"} {
		run(t, filepath.Join(subdirPath, w), "git", "checkout", "-q", "-b", "feat/other-thing")
	}

	if NeedsMigration(root) {
		t.Fatal("NeedsMigration should be false after in-place branch switch")
	}
	if n := MigrateDirectoryStructure(root); n != 0 {
		t.Fatalf("migrated %d, want 0", n)
	}
	if NeedsWorkspaceRepair(root) {
		t.Fatal("NeedsWorkspaceRepair should be false")
	}
	RepairWorkspaceContents(root)
	if !exists(subdirPath) || !exists(filepath.Join(subdirPath, "core-fix-subs")) {
		t.Fatal("directories were renamed")
	}
	if after := readFile(t, wsPath); after != before {
		t.Fatalf("workspace modified:\n%s", after)
	}
}

// A legacy monorepo layout migrates, and running migration repeatedly is idempotent
// (no "feat-feat-login" stacking).
func TestMonorepo_Legacy_MigratedIdempotently(t *testing.T) {
	root := t.TempDir()
	ltsPath, subdirPath := setupMonorepo(t, root, "core-erp-ui-login", "feat/login",
		map[string]string{"core": "core-login", "erp-ui": "erp-ui-login"})
	generateMonorepoWorkspace(subdirPath, "login", []string{"core:core-login", "erp-ui:erp-ui-login"}, "claude", "code", false)

	if !NeedsMigration(root) {
		t.Fatal("legacy layout should need migration")
	}
	MigrateDirectoryStructure(root)
	RepairWorkspaceContents(root)

	newSubdir := filepath.Join(ltsPath, "feat-login")
	if !exists(filepath.Join(newSubdir, "core-feat-login")) || !exists(filepath.Join(newSubdir, "erp-ui-feat-login")) {
		t.Fatal("worktrees not migrated to new names")
	}
	wsPath := filepath.Join(newSubdir, "monorepo-feat-login.code-workspace")
	ws := readFile(t, wsPath)
	for _, want := range []string{
		`"name": "core - feat-login", "path": "core-feat-login"`,
		`"name": "erp-ui - feat-login", "path": "erp-ui-feat-login"`,
		`Terminal (feat-login)`,
		`${workspaceFolder:core - feat-login}`,
	} {
		if !strings.Contains(ws, want) {
			t.Errorf("workspace missing %q:\n%s", want, ws)
		}
	}
	if strings.Contains(ws, "feat-feat-") || strings.Contains(ws, `- login`) {
		t.Fatalf("stacked or stale names:\n%s", ws)
	}
	// worktrees must still be valid
	for _, r := range []string{"core", "erp-ui"} {
		run(t, filepath.Join(newSubdir, r+"-feat-login"), "git", "status")
	}

	// Second and third passes: nothing should change
	if NeedsMigration(root) || NeedsWorkspaceRepair(root) {
		t.Fatal("should be settled after one migration")
	}
	for i := 0; i < 2; i++ {
		MigrateDirectoryStructure(root)
		RepairWorkspaceContents(root)
	}
	if again := readFile(t, wsPath); again != ws {
		t.Fatalf("workspace changed on repeated migration:\n%s", again)
	}
}

// Workspaces already damaged by older versions ("fix-fix-x", "led-led-led-x") get repaired.
func TestMonorepo_RepairStackedDisplayNames(t *testing.T) {
	root := t.TempDir()
	_, subdirPath := setupMonorepo(t, root, "led-queries-only", "led/queries-only",
		map[string]string{"core": "core-led-queries-only", "erp-ui": "erp-ui-led-queries-only"})
	generateMonorepoWorkspace(subdirPath, "led-queries-only", []string{"core:core-led-queries-only", "erp-ui:erp-ui-led-queries-only"}, "claude", "code", false)
	wsPath := filepath.Join(subdirPath, "monorepo-led-queries-only.code-workspace")
	good := readFile(t, wsPath)

	// Simulate the old bug: three stacked prefixes in every display context
	damaged := replaceDisplaySuffix(good, "led-queries-only", "led-led-led-led-queries-only")
	if damaged == good {
		t.Fatal("test setup: damage not applied")
	}
	os.WriteFile(wsPath, []byte(damaged), 0644)

	if !NeedsWorkspaceRepair(root) {
		t.Fatal("stacked display names should trigger repair")
	}
	if n := RepairWorkspaceContents(root); n != 1 {
		t.Fatalf("repaired %d, want 1", n)
	}
	if got := readFile(t, wsPath); got != good {
		t.Fatalf("repair did not restore original:\n%s", got)
	}
	if NeedsWorkspaceRepair(root) {
		t.Fatal("still needs repair after repairing")
	}
}

// A worktree created by LTS (recorded in .lts-meta.json) whose branch was
// switched to one whose legacy form coincides with its directory name must
// still not be renamed. Repo "core", dir "core-login" (new-style for
// core/login), branch switched to feat/login → legacy form is "core-login".
func TestSingleRepo_MetaGuard_BranchPrefixEqualsRepoName(t *testing.T) {
	root := t.TempDir()
	repo := initRepo(t, root, "core")
	ltsPath := filepath.Join(root, "core-lts")
	wt := filepath.Join(ltsPath, "core-login")
	addWorktree(t, repo, wt, "core/login")
	recordWorktreeCreated(ltsPath, "core-login", 1700000000)

	run(t, wt, "git", "checkout", "-q", "-b", "feat/login")

	if NeedsMigration(root) {
		t.Fatal("meta-recorded worktree must never be treated as legacy")
	}
	MigrateDirectoryStructure(root)
	if !exists(wt) {
		t.Fatal("worktree was renamed")
	}
}

// Without meta the same layout is legacy and migrates, and meta follows the rename.
func TestSingleRepo_Legacy_MetaFollowsRename(t *testing.T) {
	root := t.TempDir()
	repo := initRepo(t, root, "core")
	ltsPath := filepath.Join(root, "core-lts")
	addWorktree(t, repo, filepath.Join(ltsPath, "core-login"), "feat/login")
	if n := MigrateDirectoryStructure(root); n != 1 {
		t.Fatalf("migrated %d, want 1", n)
	}
	if worktreeCreatedAt(ltsPath, "core-login") != 0 {
		t.Fatal("stale meta entry left behind")
	}
}

// Developer-customised folder names in a monorepo workspace are preserved by repair.
func TestMonorepo_Repair_PreservesCustomFolderNames(t *testing.T) {
	root := t.TempDir()
	_, subdirPath := setupMonorepo(t, root, "fix-login", "fix/login",
		map[string]string{"core": "core-fix-login", "erp-ui": "erp-ui-fix-login"})
	generateMonorepoWorkspace(subdirPath, "fix-login", []string{"core:core-fix-login", "erp-ui:erp-ui-fix-login"}, "claude", "code", false)
	wsPath := filepath.Join(subdirPath, "monorepo-fix-login.code-workspace")
	ws := readFile(t, wsPath)

	// Developer edits: a custom name for core, plus an extra folder with a dash in its name
	custom := strings.Replace(ws, `"name": "core - fix-login"`, `"name": "core - my-api"`, 1)
	custom = strings.Replace(custom, `"folders": [`, `"folders": [
    { "name": "docs - notes", "path": "../../docs" },`, 1)
	os.WriteFile(wsPath, []byte(custom), 0644)

	if NeedsWorkspaceRepair(root) {
		t.Fatal("custom names must not look stale")
	}
	RepairWorkspaceContents(root)
	if got := readFile(t, wsPath); got != custom {
		t.Fatalf("repair modified a customised workspace:\n%s", got)
	}
}

// In a legacy group where one repo's worktree was switched to another branch,
// nothing in the group is renamed (no half-migrated group).
func TestMonorepo_Legacy_OneRepoSwitched_GroupPinned(t *testing.T) {
	root := t.TempDir()
	ltsPath, subdirPath := setupMonorepo(t, root, "core-erp-ui-login", "feat/login",
		map[string]string{"core": "core-login", "erp-ui": "erp-ui-login"})
	run(t, filepath.Join(subdirPath, "erp-ui-login"), "git", "checkout", "-q", "-b", "fix/other")

	if NeedsMigration(root) {
		t.Fatal("a mixed group must not be reported as needing migration")
	}
	MigrateDirectoryStructure(root)
	if exists(filepath.Join(ltsPath, "feat-login")) {
		t.Fatal("branch subdir was renamed although the group is mixed")
	}
	for _, d := range []string{"core-login", "erp-ui-login"} {
		if !exists(filepath.Join(subdirPath, d)) {
			t.Fatalf("%s was renamed: group is half-migrated", d)
		}
	}
	// A detached sibling (mid-rebase) pins the group the same way
	run(t, filepath.Join(subdirPath, "erp-ui-login"), "git", "checkout", "-q", "--detach")
	if NeedsMigration(root) || MigrateDirectoryStructure(root) != 0 {
		t.Fatal("detached sibling must pin the group")
	}
}

// Switching a worktree to the basis branch must not make it renamed or cleanable.
func TestSingleRepo_SwitchToMain_NotRenamedNotCleanable(t *testing.T) {
	root := t.TempDir()
	repo := initRepo(t, root, "core")
	ltsPath := filepath.Join(root, "core-lts")
	wt := filepath.Join(ltsPath, "feat-login")
	addWorktree(t, repo, wt, "feat/login")
	// main is checked out in the primary repo; detach it so the worktree can take main
	run(t, repo, "git", "checkout", "-q", "--detach")
	run(t, wt, "git", "checkout", "-q", "main")

	if NeedsMigration(root) {
		t.Fatal("should not migrate")
	}
	status, _ := GetWorktreeStatus(wt, "main")
	if status == StatusMergedCleanable {
		t.Fatal("worktree on main must not be cleanable")
	}
}

func TestIsStackedPrefix(t *testing.T) {
	yes := [][2]string{{"fix-fix-login", "fix-login"}, {"led-led-led-queries-only", "led-queries-only"}}
	no := [][2]string{{"fix-login", "fix-login"}, {"api-fix-login", "fix-login"}, {"fixfix-login", "fix-login"}, {"login", "login"}, {"x-login", "login"}}
	for _, c := range yes {
		if !isStackedPrefix(c[0], c[1]) {
			t.Errorf("%q should be stacked for %q", c[0], c[1])
		}
	}
	for _, c := range no {
		if isStackedPrefix(c[0], c[1]) {
			t.Errorf("%q should NOT be stacked for %q", c[0], c[1])
		}
	}
}

// Pre-meta worktree (nothing in .lts-meta.json) on a slash-less branch: its
// new-style dir name equals the legacy form of "<anything>/<same-name>". One
// startup check certifies it, after which a branch switch cannot rename it.
func TestSingleRepo_PreMeta_SlashlessBranch_CertifiedThenSwitched(t *testing.T) {
	root := t.TempDir()
	repo := initRepo(t, root, "core")
	ltsPath := filepath.Join(root, "core-lts")
	wt := filepath.Join(ltsPath, "core-login")
	addWorktree(t, repo, wt, "core/login") // new-style dir for core/login, no meta
	if NeedsMigration(root) {
		t.Fatal("consistent dir must not need migration")
	}
	if !namingVerified(ltsPath, "core-login") {
		t.Fatal("startup check should certify the dir")
	}
	run(t, wt, "git", "checkout", "-q", "-b", "feat/login")
	if NeedsMigration(root) || MigrateDirectoryStructure(root) != 0 || !exists(wt) {
		t.Fatal("certified dir was renamed after branch switch")
	}
}

func TestMonorepo_PreMeta_SlashlessBranch_CertifiedThenSwitched(t *testing.T) {
	root := t.TempDir()
	_, subdirPath := setupMonorepo(t, root, "login", "login",
		map[string]string{"core": "core-login", "erp-ui": "erp-ui-login"})
	if NeedsMigration(root) {
		t.Fatal("consistent group must not need migration")
	}
	for _, d := range []string{"core-login", "erp-ui-login"} {
		run(t, filepath.Join(subdirPath, d), "git", "checkout", "-q", "-b", "feat/login")
	}
	if NeedsMigration(root) || MigrateDirectoryStructure(root) != 0 {
		t.Fatal("certified group was migrated after branch switch")
	}
	if !exists(filepath.Join(subdirPath, "core-login")) {
		t.Fatal("worktree renamed")
	}
}

// A sibling "-2" worktree for the same repo must not hijack the workspace path,
// and a developer-chosen sub-path must survive repair.
func TestMonorepo_Repair_LeavesResolvablePathsAlone(t *testing.T) {
	root := t.TempDir()
	_, subdirPath := setupMonorepo(t, root, "feat-login", "feat/login",
		map[string]string{"core": "core-feat-login", "erp-ui": "erp-ui-feat-login"})
	generateMonorepoWorkspace(subdirPath, "feat-login", []string{"core:core-feat-login", "erp-ui:erp-ui-feat-login"}, "claude", "code", false)
	wsPath := filepath.Join(subdirPath, "monorepo-feat-login.code-workspace")

	// second core worktree in the same group (manual git worktree add)
	addWorktree(t, filepath.Join(root, "core"), filepath.Join(subdirPath, "core-feat-login-2"), "feat/login-2")
	// developer points erp-ui folder at a sub-path
	ws := strings.Replace(readFile(t, wsPath), `"path": "erp-ui-feat-login"`, `"path": "erp-ui-feat-login/src"`, 1)
	os.MkdirAll(filepath.Join(subdirPath, "erp-ui-feat-login", "src"), 0755)
	os.WriteFile(wsPath, []byte(ws), 0644)

	if NeedsWorkspaceRepair(root) {
		t.Fatal("resolvable paths must not look stale")
	}
	RepairWorkspaceContents(root)
	if got := readFile(t, wsPath); got != ws {
		t.Fatalf("repair changed resolvable paths:\n%s", got)
	}
}

// A customised name that merely ends in the expected suffix is not "stacked".
func TestMonorepo_Repair_KeepsSuffixLikeCustomName(t *testing.T) {
	root := t.TempDir()
	_, subdirPath := setupMonorepo(t, root, "fix-login", "fix/login",
		map[string]string{"core": "core-fix-login", "erp-ui": "erp-ui-fix-login"})
	generateMonorepoWorkspace(subdirPath, "fix-login", []string{"core:core-fix-login", "erp-ui:erp-ui-fix-login"}, "claude", "code", false)
	wsPath := filepath.Join(subdirPath, "monorepo-fix-login.code-workspace")
	ws := strings.Replace(readFile(t, wsPath), `"name": "core - fix-login"`, `"name": "core - api-fix-login"`, 1)
	os.WriteFile(wsPath, []byte(ws), 0644)
	if NeedsWorkspaceRepair(root) {
		t.Fatal("custom name flagged as stale")
	}
}
