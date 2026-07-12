package app

import (
	"os"
	"path/filepath"
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

// A clone can finish (and offer an env restore) while the studio is open:
// the prompt must defer to the studio, survive the studio session, then
// defer to the reopened settings, and finally appear.
func TestEnvRestoreDefersToThemeStudio(t *testing.T) {
	defer ui.ApplyTheme(ui.ClassicLTS)
	home := t.TempDir()
	t.Setenv("HOME", home)
	backup := filepath.Join(home, ".config", "lts", "env-backup", "core", "20260701-120000")
	os.MkdirAll(backup, 0755)
	os.WriteFile(filepath.Join(backup, ".env"), []byte("X=1\n"), 0600)

	m := testModel()
	m.themeStudio = ui.NewThemeStudio("classic-lts")
	updated, _ := m.Update(CloneDoneMsg{RepoName: "core", Locked: []string{"core"}})
	m2 := updated.(Model)
	if !m2.envRestoreActive {
		t.Fatal("prompt state should be set even while the studio is open")
	}
	m2.recomputeLayout()
	if view := m2.View(); strings.Contains(stripStudioANSI(view), "Restore .env backup?") {
		t.Fatal("prompt must not render over the studio")
	}

	// Studio esc → settings reopens; prompt still waits behind settings.
	m3, _ := handleThemeStudioKey(m2, key("esc"))
	if !m3.settings.Active || !m3.envRestoreActive {
		t.Fatal("settings should reopen with the prompt still pending")
	}
	// Settings esc → prompt finally has the screen.
	updated, _ = m3.Update(key("esc"))
	m4 := updated.(Model)
	if m4.settings.Active {
		t.Fatal("esc should close settings")
	}
	if !strings.Contains(stripStudioANSI(m4.View()), "Restore .env backup?") {
		t.Fatal("prompt should render once settings closes")
	}
}

func stripStudioANSI(s string) string {
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
