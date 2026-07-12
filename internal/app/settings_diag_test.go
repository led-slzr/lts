package app

import (
	"strings"
	"testing"

	"lts-revamp/internal/config"
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDiagnosticsTabItems(t *testing.T) {
	cfg := config.Config{Global: config.DefaultGlobal(), Local: map[string]config.RepoLocalConfig{}, WorkDir: t.TempDir()}
	s := ui.NewSettings(&cfg, []string{"core", "goforms"})

	// Cycle to the Diagnostics tab (last one)
	for s.ActiveTab != ui.TabDiagnostics {
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	var labels []string
	byLabel := map[string]string{}
	for _, item := range s.Items {
		labels = append(labels, item.Label)
		byLabel[item.Label] = item.Value
	}
	for _, want := range []string{"Git", "Working Directory", "Repositories", "Config File", "Build", "Binary", "Last Update Check", "Check for Update", "Reset LTS (Open Setup Wizard)"} {
		if _, ok := byLabel[want]; !ok {
			t.Errorf("Diagnostics missing %q (have %v)", want, labels)
		}
	}
	// This machine has a valid git — the check should pass
	if !strings.Contains(byLabel["Git"], "✓") {
		t.Errorf("expected git check to pass, got %q", byLabel["Git"])
	}
	if !strings.Contains(byLabel["Repositories"], "2 found") {
		t.Errorf("expected repo count, got %q", byLabel["Repositories"])
	}
	// Test binaries are dev builds
	if !strings.Contains(byLabel["Build"], "dev") {
		t.Errorf("expected dev build marker, got %q", byLabel["Build"])
	}
}
