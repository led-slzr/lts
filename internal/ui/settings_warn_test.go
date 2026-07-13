package ui

import (
	"strings"
	"testing"

	"lts-revamp/internal/config"
)

func TestToolWarningFlagsMissingPrograms(t *testing.T) {
	cases := []struct {
		key, value, want string
	}{
		{"PACKAGE_MANAGER", "auto", ""}, // auto never warns
		{"PACKAGE_MANAGER", "definitely-missing-pm", "definitely-missing-pm not found"},
		{"REPO_PACKAGE_MANAGER", "default", ""}, // routed to global
		{"REPO_PACKAGE_MANAGER", "missing-pm-xyz", "missing-pm-xyz not found"},
		{"AI_CLI_COMMAND", "no-such-cli --flag", "no-such-cli not found"},
		{"AI_CLI_COMMAND", "", ""},     // unset stays quiet
		{"IDE_COMMAND", "git", ""},     // present binary: no warn
		{"SORT_ORDER", "activity", ""}, // non-tool keys never warn
	}
	for _, c := range cases {
		if got := toolWarning(c.key, c.value); got != c.want {
			t.Errorf("toolWarning(%s, %q) = %q, want %q", c.key, c.value, got, c.want)
		}
	}
}

func TestToolWarningTmuxUsesDetection(t *testing.T) {
	orig := TmuxAvailableFn
	defer func() { TmuxAvailableFn = orig }()
	TmuxAvailableFn = func() bool { return false }
	if got := toolWarning("TERMINAL_MULTIPLEXER", "tmux"); got != "tmux not found" {
		t.Errorf("missing tmux must warn, got %q", got)
	}
	if got := toolWarning("TERMINAL_MULTIPLEXER", "none"); got != "" {
		t.Errorf("none never warns, got %q", got)
	}
	TmuxAvailableFn = func() bool { return true }
	if got := toolWarning("TERMINAL_MULTIPLEXER", "tmux"); got != "" {
		t.Errorf("installed tmux must not warn, got %q", got)
	}
}

// A misconfigured value renders its warning inline on the settings screen.
func TestSettingsRendersInlineWarning(t *testing.T) {
	cfg := config.Config{Global: config.DefaultGlobal(), Local: map[string]config.RepoLocalConfig{}}
	cfg.Global.PackageManager = "missing-pm-xyz"
	s := NewSettings(&cfg, nil)
	s.ViewWidth, s.ViewHeight = 120, 60
	if !strings.Contains(stripANSI(s.View(120, 60)), "⚠ missing-pm-xyz not found") {
		t.Fatal("settings view should carry the inline not-found warning")
	}
}
