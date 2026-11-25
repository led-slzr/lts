package config

import (
	"strings"
	"testing"
)

func TestGlobalSetGetRoundTrip(t *testing.T) {
	g := DefaultGlobal()
	for _, d := range GlobalKeys() {
		if _, ok := g.Get(d.Key); !ok {
			t.Errorf("Get(%s) not implemented", d.Key)
		}
	}
	cases := []struct{ key, in, want string }{
		{"PACKAGE_MANAGER", "PNPM", "pnpm"},
		{"COPY_MCP_JSON", "yes", "true"},
		{"TMUX_AI_PANE_WIDTH", "60", "60"},
		{"ide_command", "nvim", "nvim"},
		{"AI_CLI_COMMAND", "", ""},
	}
	for _, c := range cases {
		if err := g.Set(c.key, c.in); err != nil {
			t.Fatalf("Set(%s,%q): %v", c.key, c.in, err)
		}
		if got, _ := g.Get(c.key); got != c.want {
			t.Errorf("Get(%s) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestGlobalSetRejectsInvalid(t *testing.T) {
	g := DefaultGlobal()
	bad := []struct{ key, val string }{
		{"PACKAGE_MANAGER", "pip"},
		{"TMUX_AI_PANE_WIDTH", "95"},
		{"COPY_ENV_FILES", "maybe"},
		{"IDE_COMMAND", ""},
		{"NOPE", "x"},
	}
	for _, b := range bad {
		if err := g.Set(b.key, b.val); err == nil {
			t.Errorf("Set(%s,%q) accepted, want error", b.key, b.val)
		}
	}
}

// Two processes (TUI + CLI) holding their own Config must not revert each
// other's saved values.
func TestWritesMergeWithOtherProcesses(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("LTS_CONFIG_DIR", t.TempDir())
	tui := LoadReadOnly(ws)
	tui.SetRepoBasisBranch("core", "main") // TUI has an in-memory snapshot
	cli := LoadReadOnly(ws)
	root := t.TempDir()
	if err := cli.SetRepoKey("core", "LTS_ROOT", root); err != nil {
		t.Fatal(err)
	}
	if err := cli.UpdateGlobal(func(g *GlobalConfig) error { return g.Set("COPY_MCP_JSON", "true") }); err != nil {
		t.Fatal(err)
	}
	// The stale TUI now saves unrelated things
	tui.SetRepoLastRefresh("core", 42)
	tui.UpdateGlobal(func(g *GlobalConfig) error { g.Layout = "explorer"; return nil })

	fresh := LoadReadOnly(ws)
	if got := fresh.Local["CORE"].LTSRoot; got != root {
		t.Fatalf("LTS_ROOT reverted: %q", got)
	}
	if fresh.Local["CORE"].LastRefresh != 42 || !fresh.Global.CopyMCPJson || fresh.Global.Layout != "explorer" {
		t.Fatalf("lost a write: %+v / mcp=%v layout=%s", fresh.Local["CORE"], fresh.Global.CopyMCPJson, fresh.Global.Layout)
	}
}

func TestValuesCannotInjectKeys(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("LTS_CONFIG_DIR", t.TempDir())
	c := LoadReadOnly(ws)
	if err := c.SetRepoKey("app", "SETUP_SCRIPT", "true\nERP_LTS_ROOT=\"/elsewhere"); err == nil {
		t.Fatal("a value with a newline must be rejected")
	}
	g := DefaultGlobal()
	if err := g.Set("IDE_COMMAND", "code\nTHEME=\"x"); err == nil {
		t.Fatal("global value with a newline must be rejected")
	}
	// A long (but legal-length) line must not truncate parsing of later keys
	c.SetRepoKey("aaa", "SETUP_SCRIPT", strings.Repeat("x", MaxValueLen))
	c.SetRepoKey("zzz", "BASIS_BRANCH", "dev")
	if got := LoadReadOnly(ws).Local["ZZZ"].BasisBranch; got != "dev" {
		t.Fatalf("keys after a long line were lost: %q", got)
	}
}
