//go:build screenshots

package app

// Renders README screenshots as ANSI frames from the real TUI models.
//
//	scripts/screenshots/demo-workspace.sh /tmp/lts-demo
//	LTS_DEMO=/tmp/lts-demo SHOTS=/tmp/lts-shots go test -tags screenshots -run TestScreenshots ./internal/app
//	python3 scripts/screenshots/render.py /tmp/lts-shots docs/images
//
// Each frame is written as <name>.ans (one terminal screen, ANSI colors).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"lts-revamp/internal/config"
	"lts-revamp/internal/core"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

const shotW, shotH = 132, 40

func shotKey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func send(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m
}

func demoModel(t *testing.T, ws string, height int) Model {
	t.Setenv("LTS_CONFIG_DIR", t.TempDir())
	cfg := config.LoadReadOnly(ws)
	cfg.Global.IDECommand = "code"
	cfg.Global.AICliCommand = "claude"
	cfg.Global.Terminal = "ghostty"
	core.ApplyLTSRoots(&cfg)
	m := NewModel(cfg)
	m = send(m, tea.WindowSizeMsg{Width: shotW, Height: height})
	m.initialLoad = false
	m.greeting = "Good evening, Demo — ship something great"
	m.ghState = ui.CloneReady
	m.ghUser = "demo-dev"
	m.diskUsed, m.diskTotal = 412<<30, 994<<30
	repos := git.DiscoverRepos(ws, core.BasisResolver(&m.config))
	core.SortRepos(repos, "name")
	m.repos = repos
	m.tmuxLive = map[string]bool{}
	m.sizeCache = map[string]sizeEntry{}
	sizes := []ui.WTSize{{Total: 1410 << 20, Modules: 1100 << 20, Known: true}, {Total: 96 << 20, Known: true}, {Total: 845 << 20, Modules: 640 << 20, Known: true}}
	i := 0
	for _, r := range repos {
		for _, wt := range r.Worktrees {
			m.sizeCache[wt.Path] = sizeEntry{size: sizes[i%len(sizes)], activity: wt.LastActivity, scannedAt: time.Now()}
			if i == 1 {
				m.tmuxLive[opener.SessionName(wt.Path)] = true
			}
			i++
		}
	}
	m.statusMsg = ""
	m.recomputeLayout()
	return m
}

func TestScreenshots(t *testing.T) {
	ws, out := os.Getenv("LTS_DEMO"), os.Getenv("SHOTS")
	if ws == "" || out == "" {
		t.Skip("set LTS_DEMO and SHOTS")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	os.MkdirAll(out, 0755)
	write := func(name, frame string) {
		if err := os.WriteFile(filepath.Join(out, name+".ans"), []byte(frame), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Board
	m := demoModel(t, ws, shotH)
	write("board", m.View())

	// Explorer: api selected, focus on the sheet, second row
	m = demoModel(t, ws, 30)
	m = send(m, shotKey("shift+tab"))
	m.statusMsg = ""
	for i, r := range m.repos {
		if r.Name == "api" {
			m.explorer.SelectedRepo = i
		}
	}
	m = send(m, shotKey("right"), shotKey("down"))
	write("explorer", m.View())

	// Create worktree: api + web (mono group), typing a branch
	m = demoModel(t, ws, shotH)
	m = send(m, shotKey("n"))
	for i, r := range m.modal.Repos {
		if r.Name == "api" || r.Name == "web" {
			m.modal.Selected[i] = true
		}
	}
	m = send(m, shotKey("enter"))
	for _, r := range "feat/" {
		m = send(m, shotKey(string(r)))
	}
	write("create", m.View())

	// Setup wizard
	s := ui.NewSetup()
	ns, _ := s.Update(tea.WindowSizeMsg{Width: shotW, Height: shotH})
	write("setup", ns.(ui.SetupModel).View())
	// Theme Studio on a non-default theme
	m = demoModel(t, ws, shotH)
	m, _ = openThemeStudio(m)
	for i := 0; i < 5; i++ {
		m = send(m, shotKey("down"))
	}
	write("themes", m.View())

	ui.ApplyTheme(ui.ThemeByKey("classic-lts"))
}
