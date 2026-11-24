package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
