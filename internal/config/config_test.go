package config

import "testing"

func TestUsageLabels(t *testing.T) {
	cases := []struct {
		ide, aiCli, terminal      string
		wantIDE, wantAI, wantTerm string
	}{
		{"windsurf", "claude", "ghostty", "Windsurf", "Claude", "Ghostty"},
		{"code", "claude --dangerously-skip-permissions", "iterm", "VSCode", "Claude", "iTerm"},
		{"cursor", "opencode", "wezterm", "Cursor", "Opencode", "WezTerm"},
		{"", "", "", "IDE", "AI CLI", "Terminal"},
		{"zed", "aider", "terminal", "Zed", "Aider", "Terminal"},
	}
	for _, tc := range cases {
		c := Config{Global: GlobalConfig{IDECommand: tc.ide, AICliCommand: tc.aiCli, Terminal: tc.terminal}}
		if got := c.IDELabel(); got != tc.wantIDE {
			t.Errorf("IDELabel(%q) = %q, want %q", tc.ide, got, tc.wantIDE)
		}
		if got := c.AICliLabel(); got != tc.wantAI {
			t.Errorf("AICliLabel(%q) = %q, want %q", tc.aiCli, got, tc.wantAI)
		}
		if got := c.TerminalLabel(); got != tc.wantTerm {
			t.Errorf("TerminalLabel(%q) = %q, want %q", tc.terminal, got, tc.wantTerm)
		}
	}
}

func TestRepoPackageManagerOverride(t *testing.T) {
	dir := t.TempDir()
	c := Config{Global: DefaultGlobal(), Local: map[string]RepoLocalConfig{}, WorkDir: dir}

	if got := c.GetRepoPackageManager("core"); got != "auto" {
		t.Errorf("expected global default auto, got %q", got)
	}
	if err := c.SetRepoPackageManager("core", "bun"); err != nil {
		t.Fatal(err)
	}
	if got := c.GetRepoPackageManager("core"); got != "bun" {
		t.Errorf("expected override bun, got %q", got)
	}

	// survives a reload from disk
	reloaded := map[string]RepoLocalConfig{}
	loadLocal(dir, reloaded)
	if reloaded["CORE"].PackageManager != "bun" {
		t.Errorf("expected persisted override, got %+v", reloaded["CORE"])
	}

	// empty resets to the global default and drops the key from disk
	if err := c.SetRepoPackageManager("core", ""); err != nil {
		t.Fatal(err)
	}
	if got := c.GetRepoPackageManager("core"); got != "auto" {
		t.Errorf("expected fallback to auto after reset, got %q", got)
	}
	reloaded = map[string]RepoLocalConfig{}
	loadLocal(dir, reloaded)
	if reloaded["CORE"].PackageManager != "" {
		t.Errorf("expected no persisted override after reset, got %q", reloaded["CORE"].PackageManager)
	}
}
