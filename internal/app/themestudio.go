package app

import (
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// Theme Studio wiring: opened from the Settings "Theme" row. Browsing
// hot-swaps the live theme (the studio screen is its own preview);
// enter persists the choice, esc restores the saved theme. Either way
// the user lands back in Settings where they came from.

func openThemeStudio(m Model) (Model, tea.Cmd) {
	m.settings.Active = false
	m.themeStudio = ui.NewThemeStudio(m.config.Global.Theme)
	return m, nil
}

func handleThemeStudioKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.themeStudio.Cursor > 0 {
			m.themeStudio.Cursor--
			ui.ApplyTheme(ui.Themes[m.themeStudio.Cursor])
		}
		return m, nil
	case "down", "j":
		if m.themeStudio.Cursor < len(ui.Themes)-1 {
			m.themeStudio.Cursor++
			ui.ApplyTheme(ui.Themes[m.themeStudio.Cursor])
		}
		return m, nil
	case "enter":
		t := ui.Themes[m.themeStudio.Cursor]
		ui.ApplyTheme(t)
		m.themeStudio.Active = false
		m.config.Global.Theme = t.Key
		m.config.SaveGlobal()
		m = m.reopenSettings()
		m.settings.SaveStatus = "Theme: " + t.Name
		return m, nil
	case "esc":
		ui.ApplyTheme(ui.ThemeByKey(m.themeStudio.SavedKey))
		m.themeStudio.Active = false
		return m.reopenSettings(), nil
	}
	return m, nil
}

// handleThemeStudioMouse: hovering a theme row tries it on live, clicking
// selects it the same way — enter/esc still decide.
func handleThemeStudioMouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) {
	row := msg.Y - ui.StudioListStartY
	inList := msg.X < ui.StudioListWidth && row >= 0 && row < len(ui.Themes)

	m.themeStudio.Hovered = -1
	if inList {
		m.themeStudio.Hovered = row
		if row != m.themeStudio.Cursor &&
			(msg.Action == tea.MouseActionMotion ||
				(msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft)) {
			m.themeStudio.Cursor = row
			ui.ApplyTheme(ui.Themes[row])
		}
	}
	return m, nil
}

// reopenSettings returns to the Settings screen the studio was opened from.
func (m Model) reopenSettings() Model {
	var repoNames []string
	for _, r := range m.repos {
		if !r.IsMonorepo {
			repoNames = append(repoNames, r.Name)
		}
	}
	m.settings = ui.NewSettings(&m.config, repoNames)
	m.settings.ViewHeight = m.height
	m.settings.ViewWidth = m.width
	return m
}
