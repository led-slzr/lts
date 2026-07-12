package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The original hard-coded palette — Classic LTS must stay byte-identical
// to what the UI shipped with before themes existed.
func TestClassicIsTheOriginalPalette(t *testing.T) {
	want := map[string]string{
		ClassicLTS.Bg: "#000000", ClassicLTS.Text: "#FFFFFF", ClassicLTS.Dim: "#666666",
		ClassicLTS.Gray: "#555555", ClassicLTS.BtnBg: "#111111",
		ClassicLTS.Accent: "#00AA00", ClassicLTS.AccentDark: "#006400",
		ClassicLTS.Clean: "#5F875F", ClassicLTS.Danger: "#CC3333", ClassicLTS.Warn: "#CCAA00",
		ClassicLTS.Info: "#3388FF", ClassicLTS.Changed: "#00CCCC",
		ClassicLTS.Special: "#CC55CC", ClassicLTS.Session: "#00CCCC",
	}
	for got, expected := range want {
		if got != expected {
			t.Errorf("classic slot drifted: got %s want %s", got, expected)
		}
	}
}

func TestThemeRegistryKeysUniqueAndComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, th := range Themes {
		if th.Key == "" || th.Name == "" {
			t.Errorf("theme %q/%q missing key or name", th.Key, th.Name)
		}
		if seen[th.Key] {
			t.Errorf("duplicate theme key %q", th.Key)
		}
		seen[th.Key] = true
		for slot, v := range map[string]string{
			"Bg": th.Bg, "Text": th.Text, "Dim": th.Dim, "Gray": th.Gray, "BtnBg": th.BtnBg,
			"Accent": th.Accent, "AccentDark": th.AccentDark, "Clean": th.Clean,
			"Danger": th.Danger, "Warn": th.Warn, "Info": th.Info, "Changed": th.Changed,
			"Special": th.Special, "Session": th.Session,
		} {
			if len(v) != 7 || v[0] != '#' {
				t.Errorf("theme %s slot %s not a #RRGGBB hex: %q", th.Key, slot, v)
			}
		}
	}
}

func TestThemeByKeyFallsBackToClassic(t *testing.T) {
	if got := ThemeByKey("no-such-theme"); got.Key != "classic-lts" {
		t.Errorf("unknown key should fall back to classic, got %s", got.Key)
	}
	if got := ThemeByKey(""); got.Key != "classic-lts" {
		t.Errorf("empty key should fall back to classic, got %s", got.Key)
	}
}

// ApplyTheme must hot-swap both the color vars and the derived styles.
func TestApplyThemeRebuildsDerivedStyles(t *testing.T) {
	defer ApplyTheme(ClassicLTS)

	candidate := ClassicLTS
	candidate.Key, candidate.Name = "test", "Test"
	candidate.Accent = "#123456"
	ApplyTheme(candidate)

	if ColorGreen != lipgloss.Color("#123456") {
		t.Fatal("color var not swapped")
	}
	if StatusBarStyle.GetForeground() != lipgloss.Color("#123456") {
		t.Fatal("derived style not rebuilt with the new accent")
	}
	ApplyTheme(ClassicLTS)
	if StatusBarStyle.GetForeground() != lipgloss.Color("#00AA00") {
		t.Fatal("restoring classic should rebuild styles back")
	}
}

func TestThemeBgSeq(t *testing.T) {
	defer ApplyTheme(ClassicLTS)
	if got := ThemeBgSeq(); got != "\033[48;2;0;0;0m" {
		t.Errorf("classic canvas must be the original black sequence, got %q", got)
	}
	light := ClassicLTS
	light.Key, light.Bg = "test-light", "#FAF8F1"
	ApplyTheme(light)
	if got := ThemeBgSeq(); got != "\033[48;2;250;248;241m" {
		t.Errorf("light canvas sequence wrong: %q", got)
	}
}

// The studio's mouse hit-testing depends on the theme list starting at
// StudioListStartY — pin the render geometry.
func TestStudioRenderGeometry(t *testing.T) {
	defer ApplyTheme(ClassicLTS)
	st := NewThemeStudio("jela-my-love")
	if Themes[st.Cursor].Key != "jela-my-love" {
		t.Fatal("cursor should open on the current theme")
	}
	out := RenderThemeStudio(st, 110, 40)
	lines := strings.Split(out, "\n")

	// Theme row i renders at StudioListStartY+i.
	for i, th := range Themes {
		if !strings.Contains(stripANSI(lines[StudioListStartY+i]), th.Name) {
			t.Errorf("theme %q not on line %d", th.Name, StudioListStartY+i)
		}
	}
	plain := stripANSI(out)
	for _, want := range []string{"Theme Studio", "sample-repo", "⚠ diverged", "merged, cleanable", "never pushed", "enter apply"} {
		if !strings.Contains(plain, want) {
			t.Errorf("studio render missing %q", want)
		}
	}
	// The mini Board card renders at 40 rows (bordered corners present).
	if !strings.Contains(plain, "╭") {
		t.Error("mini board card should render at full height")
	}
	// At a short height the card drops but the strip stays.
	short := stripANSI(RenderThemeStudio(st, 110, 24))
	if strings.Contains(short, "╭") {
		t.Error("mini card should drop on short terminals")
	}
	if !strings.Contains(short, "[r] Refresh") {
		t.Error("footer strip must survive short terminals")
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\033':
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
