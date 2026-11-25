package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lts-revamp/internal/lock"
)

// fixture builds a workspace with repos backed by local bare origins.
type fixture struct {
	t   *testing.T
	ws  string
	cfg string
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newFixture(t *testing.T, repos ...string) *fixture {
	t.Helper()
	base, _ := filepath.EvalSymlinks(t.TempDir())
	f := &fixture{t: t, ws: filepath.Join(base, "ws"), cfg: filepath.Join(base, "cfg")}
	os.MkdirAll(f.ws, 0755)
	t.Setenv("LTS_CONFIG_DIR", f.cfg)
	t.Setenv("LTS_DIR", "")
	for _, r := range repos {
		origin := filepath.Join(base, "origins", r+".git")
		os.MkdirAll(origin, 0755)
		gitT(t, origin, "init", "-q", "--bare")
		gitT(t, f.ws, "clone", "-q", origin, r)
		dir := filepath.Join(f.ws, r)
		gitT(t, dir, "checkout", "-q", "-b", "main")
		os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+r+"\n"), 0644)
		gitT(t, dir, "add", ".")
		gitT(t, dir, "commit", "-q", "-m", "init")
		gitT(t, dir, "push", "-q", "-u", "origin", "main")
	}
	return f
}

func (f *fixture) repo(name string) string { return filepath.Join(f.ws, name) }

// run executes the CLI in the fixture workspace; never interactive.
func (f *fixture) run(args ...string) (string, string, int) {
	f.t.Helper()
	var out, errb bytes.Buffer
	stdout, stderr, stdin = &out, &errb, strings.NewReader("")
	no := false
	forceInteractive = &no
	defer func() { stdout, stderr, stdin, forceInteractive = os.Stdout, os.Stderr, os.Stdin, nil }()
	code := Execute(append([]string{"--dir", f.ws}, args...), func(string) int { return 0 })
	return out.String(), errb.String(), code
}

func (f *fixture) runJSON(args ...string) (map[string]any, int) {
	f.t.Helper()
	out, errs, code := f.run(append(args, "--json")...)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		f.t.Fatalf("invalid JSON from %v: %v\nstdout:%s\nstderr:%s", args, err, out, errs)
	}
	return m, code
}

func (f *fixture) addWorktree(repo, branch string) string {
	f.t.Helper()
	p := filepath.Join(f.ws, repo+"-lts", strings.ReplaceAll(branch, "/", "-"))
	os.MkdirAll(filepath.Dir(p), 0755)
	gitT(f.t, f.repo(repo), "worktree", "add", "-q", "-b", branch, p)
	return p
}

func TestLsStatusPath(t *testing.T) {
	f := newFixture(t, "core", "erp")
	wt := f.addWorktree("core", "feat/login")

	m, code := f.runJSON("ls")
	if code != 0 || m["ok"] != true || m["schema_version"].(float64) != 1 {
		t.Fatalf("ls: code=%d %v", code, m)
	}
	out, _, _ := f.run("ls")
	if !strings.Contains(out, "core-lts/feat-login") {
		t.Fatalf("ls text missing id:\n%s", out)
	}

	out, _, code = f.run("path", "feat/login")
	if code != 0 || strings.TrimSpace(out) != wt {
		t.Fatalf("path = %q (code %d), want %s", out, code, wt)
	}

	_, errs, code := f.run("path", "feat/logn")
	if code != ExitNotFound || !strings.Contains(errs, "feat/login") {
		t.Fatalf("typo should exit 3 with a suggestion: code=%d stderr=%s", code, errs)
	}

	os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("x"), 0644)
	m, code = f.runJSON("status", "core-lts/feat-login")
	wts := m["worktrees"].([]any)
	st := wts[0].(map[string]any)["status"].(map[string]any)
	if code != 0 || st["state"] != "new_dirty" || st["dangerous_to_delete"] != true {
		t.Fatalf("status = %v", st)
	}

	m, code = f.runJSON("ls", "nope")
	if code != ExitNotFound || m["ok"] != false || m["error"].(map[string]any)["code"] != CodeNotFound {
		t.Fatalf("unknown repo: code=%d %v", code, m)
	}
}

func TestUnknownCommandAndFlag(t *testing.T) {
	f := newFixture(t, "core")
	_, errs, code := f.run("stauts")
	if code != ExitUsage || !strings.Contains(errs, "status") {
		t.Fatalf("unknown command: code=%d stderr=%s", code, errs)
	}
	m, code := f.runJSON("ls", "--hiden")
	if code != ExitUsage || !strings.Contains(strings.Join(toStrings(m["error"].(map[string]any)["suggestions"]), ","), "--hidden") {
		t.Fatalf("unknown flag: code=%d %v", code, m)
	}
}

func TestReadOnlyCommandsWriteNothing(t *testing.T) {
	f := newFixture(t, "core")
	f.run("ls")
	f.run("status", "--all")
	f.run("config")
	if _, err := os.Stat(f.cfg); !os.IsNotExist(err) {
		t.Fatal("read-only commands must not create the global config")
	}
	if _, err := os.Stat(filepath.Join(f.ws, ".lts.conf")); !os.IsNotExist(err) {
		t.Fatal("read-only commands must not create .lts.conf")
	}
}

func toStrings(v any) []string {
	var out []string
	if arr, ok := v.([]any); ok {
		for _, x := range arr {
			out = append(out, x.(string))
		}
	}
	return out
}

func TestConfigSet(t *testing.T) {
	f := newFixture(t, "core")
	if _, _, code := f.run("config", "set", "COPY_MCP_JSON", "yes"); code != 0 {
		t.Fatalf("set global: %d", code)
	}
	out, _, _ := f.run("config", "get", "COPY_MCP_JSON")
	if strings.TrimSpace(out) != "true" {
		t.Fatalf("get after set = %q", out)
	}
	if _, _, code := f.run("config", "set", "PACKAGE_MANAGER", "pip"); code != ExitUsage {
		t.Fatalf("invalid enum should be usage error, got %d", code)
	}
	_, errs, code := f.run("config", "set", "BASIS_BRANCH", "mian", "-r", "core")
	if code != ExitNotFound || !strings.Contains(errs, "main") {
		t.Fatalf("missing basis branch: code=%d stderr=%s", code, errs)
	}
	f.addWorktree("core", "feat/a")
	root := t.TempDir()
	if _, _, code := f.run("config", "set", "LTS_ROOT", root, "-r", "core"); code != ExitGuard {
		t.Fatalf("LTS_ROOT change with live worktrees must be refused, got %d", code)
	}
}

func TestCreateReuseRenameRebase(t *testing.T) {
	f := newFixture(t, "core", "erp")
	out, errs, code := f.run("create", "feat/a", "-r", "core", "--no-install")
	wantPath := filepath.Join(f.ws, "core-lts", "feat-a")
	if code != 0 || strings.TrimSpace(out) != wantPath {
		t.Fatalf("create: code=%d out=%q stderr=%s", code, out, errs)
	}
	if _, _, code := f.run("create", "feat/a", "-r", "core"); code != ExitExists {
		t.Fatalf("second create should exit 7, got %d", code)
	}
	out, _, code = f.run("create", "feat/a", "-r", "core", "--reuse")
	if code != 0 || strings.TrimSpace(out) != wantPath {
		t.Fatalf("--reuse: code=%d out=%q", code, out)
	}

	// dry-run changes nothing and reports main-checkout side effects
	os.WriteFile(filepath.Join(f.repo("erp"), "README.md"), []byte("dirty\n"), 0644)
	m, code := f.runJSON("create", "feat/b", "-r", "erp", "--dry-run")
	plan, _ := json.Marshal(m["plan"])
	if code != 0 || !strings.Contains(string(plan), "stash") || !strings.Contains(string(plan), "side_effect") {
		t.Fatalf("dry-run plan: code=%d %s", code, plan)
	}
	if fileExists(filepath.Join(f.ws, "erp-lts")) {
		t.Fatal("dry-run must not create anything")
	}
	gitT(t, f.repo("erp"), "checkout", "README.md")

	// mono group
	m, code = f.runJSON("create", "feat/m", "-r", "core,erp", "--no-install")
	if code != 0 || m["group"] != true || m["path"] != filepath.Join(f.ws, "core+erp-lts", "feat-m") {
		t.Fatalf("mono create: code=%d %v", code, m)
	}

	// rename
	out, _, code = f.run("rename", "feat/a", "feat/a2")
	if code != 0 || strings.TrimSpace(out) != filepath.Join(f.ws, "core-lts", "feat-a2") {
		t.Fatalf("rename: code=%d out=%q", code, out)
	}
	if _, _, code := f.run("rename", "feat/a2", "feat/m"); code != ExitExists {
		t.Fatalf("rename onto an existing branch should exit 7, got %d", code)
	}

	// rebase conflict → exit 6, worktree restored
	wt := filepath.Join(f.ws, "core-lts", "feat-a2")
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("branch side\n"), 0644)
	gitT(t, wt, "commit", "-qam", "branch")
	os.WriteFile(filepath.Join(f.repo("core"), "README.md"), []byte("main side\n"), 0644)
	gitT(t, f.repo("core"), "commit", "-qam", "main")
	os.WriteFile(filepath.Join(wt, "notes.txt"), []byte("untracked\n"), 0644)
	_, _, code = f.run("rebase", "feat/a2", "--no-install")
	if code != ExitConflict {
		t.Fatalf("conflicting rebase should exit 6, got %d", code)
	}
	if !fileExists(filepath.Join(wt, "notes.txt")) {
		t.Fatal("untracked file lost")
	}
	m, code = f.runJSON("rebase", "feat/m", "--no-install")
	if code != 0 || len(m["members"].([]any)) != 2 {
		t.Fatalf("mono rebase: code=%d %v", code, m)
	}
	if _, _, code := f.run("rebase", "feat/m", "--member", "nope"); code != ExitNotFound {
		t.Fatalf("unknown --member should exit 3, got %d", code)
	}
}

func TestBusyRepoFailsFastOrWaits(t *testing.T) {
	f := newFixture(t, "core")
	f.addWorktree("core", "feat/a")
	h, err := lock.TryAcquire(f.ws, []string{"core"}, "lts TUI: Rebasing feat/a...")
	if err != nil {
		t.Fatal(err)
	}
	m, code := f.runJSON("rename", "feat/a", "feat/b")
	e := m["error"].(map[string]any)
	if code != ExitError || e["code"] != CodeBusy || !strings.Contains(e["message"].(string), "TUI") {
		t.Fatalf("busy rename: code=%d %v", code, m)
	}
	m, _ = f.runJSON("ls")
	wt := m["repos"].([]any)[0].(map[string]any)["worktrees"].([]any)[0].(map[string]any)
	if wt["busy"] != true {
		t.Fatalf("ls should report busy: %v", wt)
	}
	go func() { time.Sleep(500 * time.Millisecond); h.Release() }()
	if _, errs, code := f.run("--wait=5s", "rename", "feat/a", "feat/b"); code != 0 {
		t.Fatalf("--wait should succeed after release: code=%d %s", code, errs)
	}
}

func TestDeleteGuards(t *testing.T) {
	f := newFixture(t, "core")
	wt := f.addWorktree("core", "feat/dirty")
	os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("x"), 0644)

	// dangerous: --yes alone is refused, nothing deleted
	if _, _, code := f.run("delete", "feat/dirty", "--yes"); code != ExitGuard {
		t.Fatalf("dirty delete with --yes only should exit 4, got %d", code)
	}
	if !fileExists(wt) {
		t.Fatal("guard must leave the worktree intact")
	}
	// --force without --yes still needs confirmation (no TTY)
	if _, _, code := f.run("delete", "feat/dirty", "--force"); code != ExitGuard {
		t.Fatalf("--force without --yes should exit 4, got %d", code)
	}
	if _, _, code := f.run("delete", "feat/dirty", "--force", "--yes"); code != 0 {
		t.Fatalf("--force --yes should delete, got %d", code)
	}
	if fileExists(wt) {
		t.Fatal("worktree should be gone")
	}

	// protected branch: worktree removed, branch kept
	gitT(t, f.repo("core"), "branch", "develop")
	p := filepath.Join(f.ws, "core-lts", "develop")
	gitT(t, f.repo("core"), "worktree", "add", "-q", p, "develop")
	if _, _, code := f.run("delete", "develop", "--yes", "--remote"); code != 0 {
		t.Fatalf("protected delete: %d", code)
	}
	if out := gitT(t, f.repo("core"), "branch", "--list", "develop"); !strings.Contains(out, "develop") {
		t.Fatal("protected branch must never be deleted")
	}
}

func TestCleanupMerged(t *testing.T) {
	f := newFixture(t, "core")
	merged := f.addWorktree("core", "feat/merged")
	gitT(t, merged, "push", "-q", "-u", "origin", "feat/merged")
	open := f.addWorktree("core", "feat/open")
	os.WriteFile(filepath.Join(open, "f.txt"), []byte("x"), 0644)
	gitT(t, open, "add", ".")
	gitT(t, open, "commit", "-qm", "work")

	m, code := f.runJSON("cleanup-merged", "--dry-run")
	c := toStrings(m["candidates"])
	if code != 0 || len(c) != 1 || c[0] != "core-lts/feat-merged" {
		t.Fatalf("candidates = %v (code %d)", c, code)
	}
	if _, _, code := f.run("cleanup-merged"); code != ExitGuard {
		t.Fatalf("cleanup without --yes should exit 4, got %d", code)
	}
	if _, _, code := f.run("cleanup-merged", "--yes"); code != 0 {
		t.Fatalf("cleanup --yes: %d", code)
	}
	if fileExists(merged) || !fileExists(open) {
		t.Fatal("cleanup must remove only the merged worktree")
	}
}

func TestHelpSurfaces(t *testing.T) {
	f := newFixture(t, "core")
	m, code := f.runJSON("help")
	cmdSpec := m["command"].(map[string]any)
	var names []string
	for _, c := range cmdSpec["commands"].([]any) {
		names = append(names, c.(map[string]any)["name"].(string))
	}
	for _, want := range []string{"ls", "status", "create", "delete", "cleanup-merged", "config", "agents", "path", "pr"} {
		if !contains(names, want) {
			t.Errorf("help --json missing %q (have %v)", want, names)
		}
	}
	if code != 0 || m["exit_codes"].(map[string]any)["4"] == nil {
		t.Fatalf("help --json: code=%d", code)
	}
	out, _, code := f.run("help", "agents")
	if code != 0 || !strings.Contains(out, "EXIT CODES") || !strings.Contains(out, "--dry-run") {
		t.Fatalf("help agents: code=%d", code)
	}
	for _, c := range []string{"create", "delete", "status"} {
		out, _, _ := f.run(c, "--help")
		if !strings.Contains(out, "Exit codes:") || !strings.Contains(out, "Examples:") {
			t.Errorf("%s --help lacks exit codes or examples", c)
		}
	}
}

func TestHooks(t *testing.T) {
	f := newFixture(t, "core")
	wt := f.addWorktree("core", "feat/h")
	run := func(event, input string) string {
		var out bytes.Buffer
		stdout, stdin = &out, strings.NewReader(input)
		defer func() { stdout, stdin = os.Stdout, os.Stdin }()
		if code := Execute([]string{"hook", event}, nil); code != 0 {
			t.Fatalf("hook %s exit %d", event, code)
		}
		return out.String()
	}
	out := run("session-start", `{"cwd":"`+wt+`"}`)
	var m map[string]map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("hook output not JSON: %q", out)
	}
	ctx := m["hookSpecificOutput"]["additionalContext"].(string)
	if !strings.Contains(ctx, "core-lts/feat-h") || !strings.Contains(ctx, "--dry-run") {
		t.Fatalf("context = %q", ctx)
	}
	if out := run("session-start", `{"cwd":"`+t.TempDir()+`"}`); out != "" {
		t.Fatalf("outside LTS the hook must print nothing, got %q", out)
	}
	// worktree-create: clean main checkout → LTS worktree; rerun reuses it
	p1 := run("worktree-create", `{"cwd":"`+f.repo("core")+`","name":"agent-x1"}`)
	p2 := run("worktree-create", `{"cwd":"`+f.repo("core")+`","name":"agent-x1"}`)
	want := filepath.Join(f.ws, "core-lts", "agent-x1")
	if !strings.Contains(p1, want) || p1 != p2 {
		t.Fatalf("worktree-create: %q / %q", p1, p2)
	}
	// dirty main checkout → step aside
	os.WriteFile(filepath.Join(f.repo("core"), "README.md"), []byte("dirty\n"), 0644)
	if out := run("worktree-create", `{"cwd":"`+f.repo("core")+`","name":"agent-x2"}`); out != "" {
		t.Fatalf("dirty main checkout must fall back, got %q", out)
	}
}

func TestRemoteOnlyCommitsBlockRemoteDelete(t *testing.T) {
	f := newFixture(t, "core")
	wt := f.addWorktree("core", "feat/shared")
	gitT(t, wt, "push", "-q", "-u", "origin", "feat/shared")
	origin := gitT(t, f.repo("core"), "remote", "get-url", "origin")
	other := filepath.Join(t.TempDir(), "other")
	gitT(t, filepath.Dir(other), "clone", "-q", origin, other)
	gitT(t, other, "checkout", "-q", "feat/shared")
	os.WriteFile(filepath.Join(other, "theirs.txt"), []byte("x"), 0644)
	gitT(t, other, "add", ".")
	gitT(t, other, "commit", "-qm", "colleague")
	gitT(t, other, "push", "-q", "origin", "feat/shared")
	gitT(t, wt, "fetch", "-q", "origin")

	if _, _, code := f.run("delete", "feat/shared", "--yes", "--remote"); code != ExitGuard {
		t.Fatalf("delete --remote over remote-only commits should exit 4, got %d", code)
	}
	if _, _, code := f.run("rename", "feat/shared", "feat/shared2", "--remote"); code != ExitGuard {
		t.Fatalf("rename --remote over remote-only commits should exit 4, got %d", code)
	}
	m, _ := f.runJSON("cleanup-merged", "--dry-run")
	if len(toStrings(m["candidates"])) != 0 {
		t.Fatalf("behind-upstream branch must not be a cleanup candidate: %v", m["candidates"])
	}
}

func TestKeepBranchLowersRiskToUncommittedOnly(t *testing.T) {
	f := newFixture(t, "core")
	wt := f.addWorktree("core", "feat/k")
	os.WriteFile(filepath.Join(wt, "c.txt"), []byte("x"), 0644)
	gitT(t, wt, "add", ".")
	gitT(t, wt, "commit", "-qm", "unpushed")
	if _, _, code := f.run("delete", "feat/k", "--yes"); code != ExitGuard {
		t.Fatalf("unpushed commit without --keep-branch must be refused, got %d", code)
	}
	if _, errs, code := f.run("delete", "feat/k", "--yes", "--keep-branch"); code != 0 {
		t.Fatalf("--keep-branch keeps the commit, so --yes should suffice: %d %s", code, errs)
	}
	if out := gitT(t, f.repo("core"), "log", "--oneline", "-1", "feat/k"); !strings.Contains(out, "unpushed") {
		t.Fatal("kept branch must still hold the commit")
	}
	m, _ := f.runJSON("help", "nope")
	if m["ok"] != false {
		t.Fatal("help --json on an unknown topic must fail")
	}
}

func TestOpenUsesWorkspaceFile(t *testing.T) {
	f := newFixture(t, "core")
	if _, _, code := f.run("create", "feat/o", "-r", "core", "--no-install"); code != 0 {
		t.Fatal("create failed")
	}
	logf := filepath.Join(t.TempDir(), "ide.log")
	script := filepath.Join(t.TempDir(), "ide.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"$@\" > "+logf+"\n"), 0755)
	if _, _, code := f.run("config", "set", "IDE_COMMAND", script); code != 0 {
		t.Fatal("config set failed")
	}
	if _, errs, code := f.run("open", "feat/o", "--ide"); code != 0 {
		t.Fatalf("open: %d %s", code, errs)
	}
	var got []byte
	for i := 0; i < 50 && len(got) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		got, _ = os.ReadFile(logf)
	}
	if !strings.Contains(string(got), "feat-o.code-workspace") {
		t.Fatalf("IDE should get the workspace file, got %q", got)
	}
	if _, _, code := f.run("open", "feat/o", "--ide", "--terminal"); code != ExitUsage {
		t.Fatalf("two modes should be a usage error, got %d", code)
	}
}

func TestWatchEmitsAddedAndRemoved(t *testing.T) {
	f := newFixture(t, "core")
	done := make(chan string)
	go func() {
		out, _, _ := f.run("watch", "--json", "--interval", "500ms", "--max-polls", "8")
		done <- out
	}()
	time.Sleep(700 * time.Millisecond)
	wt := f.addWorktree("core", "feat/w")
	time.Sleep(1200 * time.Millisecond)
	gitT(t, f.repo("core"), "worktree", "remove", "--force", wt)
	out := <-done
	var types []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("not NDJSON: %q", line)
		}
		types = append(types, ev["type"].(string))
	}
	joined := strings.Join(types, ",")
	if !strings.HasPrefix(joined, "ready") || !strings.Contains(joined, "worktree_added") || !strings.Contains(joined, "worktree_removed") {
		t.Fatalf("events = %s", joined)
	}
}

func TestHookWorktreeRemoveKeepsUnsavedWork(t *testing.T) {
	f := newFixture(t, "core")
	run := func(input string) {
		stdin = strings.NewReader(input)
		var out, errb bytes.Buffer
		stdout, stderr = &out, &errb
		defer func() { stdout, stderr, stdin = os.Stdout, os.Stderr, os.Stdin }()
		Execute([]string{"hook", "worktree-remove"}, nil)
	}
	clean := f.addWorktree("core", "agent-clean")
	dirty := f.addWorktree("core", "agent-dirty")
	os.WriteFile(filepath.Join(dirty, "work.txt"), []byte("x"), 0644)
	run(`{"worktree_path":"` + clean + `"}`)
	run(`{"worktree_path":"` + dirty + `"}`)
	if fileExists(clean) {
		t.Fatal("a clean, new worktree should be removed")
	}
	if !fileExists(filepath.Join(dirty, "work.txt")) {
		t.Fatal("a worktree with unsaved work must be kept")
	}
	run(`{"worktree_path":"` + f.repo("core") + `"}`) // main checkout: never touched
	if !fileExists(filepath.Join(f.repo("core"), "README.md")) {
		t.Fatal("main checkout touched")
	}
}

func TestHibernateGuards(t *testing.T) {
	f := newFixture(t, "core", "old")
	if _, _, code := f.run("hibernate", "old", "--manual", "--dry-run"); code != ExitGuard {
		t.Fatalf("disabled hibernate must exit 4, got %d", code)
	}
	f.run("config", "set", "ENABLE_HIBERNATE", "true")
	if _, _, code := f.run("hibernate", "old"); code != ExitGuard {
		t.Fatalf("strict mode without GitHub must exit 4, got %d", code)
	}
	if _, _, code := f.run("hibernate", "old", "--manual"); code != ExitGuard {
		t.Fatalf("no confirmation must exit 4, got %d", code)
	}
	if _, _, code := f.run("hibernate", "old", "--manual", "--confirm", "ol"); code != ExitGuard || !fileExists(f.repo("old")) {
		t.Fatalf("wrong confirmation must refuse, got %d", code)
	}
	if _, errs, code := f.run("hibernate", "old", "--manual", "--confirm", "old"); code != 0 || fileExists(f.repo("old")) {
		t.Fatalf("confirmed manual hibernate should remove the repo: %d %s", code, errs)
	}
	f.addWorktree("core", "feat/x")
	if _, _, code := f.run("hibernate", "core", "--manual", "--confirm", "core"); code != ExitGuard || !fileExists(f.repo("core")) {
		t.Fatalf("manual mode with live worktrees must refuse, got %d", code)
	}
}
