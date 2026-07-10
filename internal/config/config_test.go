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
