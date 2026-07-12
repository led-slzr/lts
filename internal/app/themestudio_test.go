package app

import (
	"strings"
	"testing"

	"lts-revamp/internal/config"
	"lts-revamp/internal/ui"

	"github.com/charmbracelet/lipgloss"
)

func classicAccent() lipgloss.Color { return lipgloss.Color(ui.ClassicLTS.Accent) }

func TestThemeStudioOpensFromSettingsOnSavedTheme(t *testing.T) {
	defer ui.ApplyTheme(ui.ClassicLTS)
	m := testModel()
	m.config.Global.Theme = ui.JelaMyLove.Key
	m.settings = ui.NewSettings(&m.config, nil)

	updated, _ := m.Update(ui.SettingsActionMsg{Action: "THEME_STUDIO_ACTION"})
	m2 := updated.(Model)
	if !m2.themeStudio.Active || m2.settings.Active {
		t.Fatal("studio should open and settings should close")
	}
	if ui.Themes[m2.themeStudio.Cursor].Key != ui.JelaMyLove.Key {
		t.Fatalf("cursor should start on the saved theme, got %s", ui.Themes[m2.themeStudio.Cursor].Key)
	}
	if m2.themeStudio.SavedKey != ui.JelaMyLove.Key {
		t.Fatal("saved key must be recorded for esc-restore")
	}
}

func TestThemeStudioBrowseAppliesLiveAndEscRestores(t *testing.T) {
	defer ui.ApplyTheme(ui.ClassicLTS)
	m := testModel()
	m.themeStudio = ui.NewThemeStudio("classic-lts")

	m2, _ := handleThemeStudioKey(m, key("down")) // → Led There Be Light
	if ui.ColorGreen == classicAccent() {
		t.Fatal("browsing must hot-swap the live theme")
	}
	if ui.Themes[m2.themeStudio.Cursor].Key != ui.LedThereBeLight.Key {
		t.Fatalf("cursor should be on the second theme, got %s", ui.Themes[m2.themeStudio.Cursor].Key)
	}

	m3, _ := handleThemeStudioKey(m2, key("esc"))
	if m3.themeStudio.Active {
		t.Fatal("esc should close the studio")
	}
	if ui.ColorGreen != classicAccent() {
		t.Fatal("esc must restore the saved theme")
	}
	if !m3.settings.Active {
		t.Fatal("closing the studio should land back in settings")
	}
}

func TestThemeStudioEnterCommitsAndPersists(t *testing.T) {
	defer ui.ApplyTheme(ui.ClassicLTS)
	t.Setenv("HOME", t.TempDir())
	m := testModel()
	m.themeStudio = ui.NewThemeStudio("classic-lts")

	m2, _ := handleThemeStudioKey(m, key("down"))
	m2, _ = handleThemeStudioKey(m2, key("down")) // → Jela My Love
	m3, _ := handleThemeStudioKey(m2, key("enter"))

	if m3.themeStudio.Active || !m3.settings.Active {
		t.Fatal("enter should close the studio and reopen settings")
	}
	if m3.config.Global.Theme != ui.JelaMyLove.Key {
		t.Fatalf("theme not committed to config: %q", m3.config.Global.Theme)
	}
	if reloaded := config.Load(m3.config.WorkDir); reloaded.Global.Theme != ui.JelaMyLove.Key {
		t.Fatalf("theme not persisted: %q", reloaded.Global.Theme)
	}
	if !strings.Contains(m3.settings.SaveStatus, "Jela My Love") {
		t.Errorf("settings should confirm the applied theme, got %q", m3.settings.SaveStatus)
	}
}
