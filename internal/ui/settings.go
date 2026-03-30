package ui

import (
	"fmt"
	"lts-revamp/internal/config"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SettingKind int

const (
	SettingEnum    SettingKind = iota // cycle through options
	SettingText                      // free text input
	SettingDisplay                   // read-only
)

type SettingsItem struct {
	Section  string   // "Global" or "Local"
	Label    string   // display label
	Key      string   // config key
	Value    string   // current value
	Kind     SettingKind
	Options  []string // for Enum kind
	RepoName string   // empty for global, repo name for local
}

type SettingsModel struct {
	Active    bool
	Items     []SettingsItem
	CursorIdx int
	Editing   bool
	EditInput textinput.Model
	Config    *config.Config
	Scroll    int    // scroll offset for long lists
	SaveError string // shown if save fails
}

// Messages
type SettingsSavedMsg struct{}

func NewSettings(cfg *config.Config, repoNames []string) SettingsModel {
	ti := textinput.New()
	ti.CharLimit = 100
	ti.Width = 40

	s := SettingsModel{
		Active:    true,
		Config:    cfg,
		EditInput: ti,
	}
	s.buildItems(repoNames)
	return s
}

func (s *SettingsModel) buildItems(repoNames []string) {
	s.Items = nil

	// Global settings
	s.Items = append(s.Items,
		SettingsItem{Section: "Global", Label: "IDE Command", Key: "IDE_COMMAND",
			Value: s.Config.Global.IDECommand, Kind: SettingEnum,
			Options: []string{"windsurf", "code", "cursor", "zed"}},
		SettingsItem{Section: "Global", Label: "AI CLI Command", Key: "AI_CLI_COMMAND",
			Value: s.Config.Global.AICliCommand, Kind: SettingText},
		SettingsItem{Section: "Global", Label: "Package Manager", Key: "PACKAGE_MANAGER",
			Value: s.Config.Global.PackageManager, Kind: SettingEnum,
			Options: []string{"pnpm", "npm", "yarn", "bun"}},
		SettingsItem{Section: "Global", Label: "Auto Refresh", Key: "AUTO_REFRESH",
			Value: s.Config.Global.AutoRefresh, Kind: SettingEnum,
			Options: []string{"15M", "30M", "1H", "6H", "12H", "24H"}},
		SettingsItem{Section: "Global", Label: "Terminal", Key: "TERMINAL",
			Value: s.Config.Global.Terminal, Kind: SettingEnum,
			Options: []string{"ghostty", "iterm", "terminal", "wezterm", "alacritty"}},
	)

	// Local settings per repo
	for _, repo := range repoNames {
		key := strings.ToUpper(repo)
		rc, ok := s.Config.Local[key]
		if !ok {
			rc = config.DefaultRepoLocal()
		}

		// Last refresh as human-readable
		lastRefreshStr := "never"
		if rc.LastRefresh > 0 {
			t := time.Unix(rc.LastRefresh, 0)
			dur := time.Since(t)
			if dur < time.Minute {
				lastRefreshStr = "just now"
			} else if dur < time.Hour {
				lastRefreshStr = fmt.Sprintf("%dm ago", int(dur.Minutes()))
			} else if dur < 24*time.Hour {
				lastRefreshStr = fmt.Sprintf("%dh ago", int(dur.Hours()))
			} else {
				lastRefreshStr = fmt.Sprintf("%dd ago", int(dur.Hours()/24))
			}
		}

		s.Items = append(s.Items,
			SettingsItem{Section: "Local (" + repo + ")", Label: "Basis Branch", Key: "BASIS_BRANCH",
				Value: rc.BasisBranch, Kind: SettingText, RepoName: repo},
			SettingsItem{Section: "Local (" + repo + ")", Label: "Last Refresh", Key: "LAST_REFRESH",
				Value: lastRefreshStr, Kind: SettingDisplay, RepoName: repo},
		)
	}
}

func (s SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.Editing {
			return s.handleEditKey(msg)
		}
		return s.handleNavKey(msg)
	}
	if s.Editing {
		var cmd tea.Cmd
		s.EditInput, cmd = s.EditInput.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s SettingsModel) handleNavKey(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		s.Active = false
		return s, nil
	case "up", "k":
		s.moveCursor(-1)
	case "down", "j":
		s.moveCursor(1)
	case "enter", " ":
		item := &s.Items[s.CursorIdx]
		switch item.Kind {
		case SettingEnum:
			// Cycle to next option
			for i, opt := range item.Options {
				if opt == item.Value {
					item.Value = item.Options[(i+1)%len(item.Options)]
					s.applyChange(*item)
					return s, nil
				}
			}
			// Not found, set to first
			if len(item.Options) > 0 {
				item.Value = item.Options[0]
				s.applyChange(*item)
			}
		case SettingText:
			s.Editing = true
			s.EditInput.SetValue(item.Value)
			s.EditInput.Focus()
			return s, textinput.Blink
		}
	}
	return s, nil
}

func (s *SettingsModel) moveCursor(delta int) {
	s.CursorIdx += delta
	if s.CursorIdx < 0 {
		s.CursorIdx = 0
	}
	if s.CursorIdx >= len(s.Items) {
		s.CursorIdx = len(s.Items) - 1
	}
	// Skip display-only items (loop to handle consecutive ones)
	for s.Items[s.CursorIdx].Kind == SettingDisplay {
		if delta > 0 && s.CursorIdx < len(s.Items)-1 {
			s.CursorIdx++
		} else if delta < 0 && s.CursorIdx > 0 {
			s.CursorIdx--
		} else {
			break // at boundary, can't skip further
		}
	}
}

func (s SettingsModel) handleEditKey(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		s.Editing = false
		item := &s.Items[s.CursorIdx]
		item.Value = strings.TrimSpace(s.EditInput.Value())
		s.applyChange(*item)
		return s, nil
	case "esc":
		s.Editing = false
		return s, nil
	default:
		var cmd tea.Cmd
		s.EditInput, cmd = s.EditInput.Update(msg)
		return s, cmd
	}
}

func (s *SettingsModel) applyChange(item SettingsItem) {
	s.SaveError = ""
	if item.RepoName == "" {
		// Global setting
		switch item.Key {
		case "IDE_COMMAND":
			s.Config.Global.IDECommand = item.Value
		case "AI_CLI_COMMAND":
			s.Config.Global.AICliCommand = item.Value
		case "PACKAGE_MANAGER":
			s.Config.Global.PackageManager = item.Value
		case "AUTO_REFRESH":
			s.Config.Global.AutoRefresh = item.Value
		case "TERMINAL":
			s.Config.Global.Terminal = item.Value
		}
		if err := s.Config.SaveGlobal(); err != nil {
			s.SaveError = "Failed to save: " + err.Error()
		}
	} else {
		// Local setting
		switch item.Key {
		case "BASIS_BRANCH":
			s.Config.SetRepoBasisBranch(item.RepoName, item.Value)
		}
		if err := s.Config.SaveLocal(); err != nil {
			s.SaveError = "Failed to save: " + err.Error()
		}
	}
}

func (s SettingsModel) View(width, height int) string {
	if !s.Active {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Background(ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Background(ColorBlack)
	cyanStyle := lipgloss.NewStyle().Foreground(ColorCyan).Background(ColorBlack)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorMagenta).Background(ColorBlack).Bold(true)
	activeStyle := lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorBlack).Bold(true)
	editStyle := lipgloss.NewStyle().Foreground(ColorYellow).Background(ColorBlack)

	var lines []string
	lines = append(lines, titleStyle.Render("Settings"))
	lines = append(lines, "")

	lastSection := ""
	for i, item := range s.Items {
		// Section divider
		if item.Section != lastSection {
			if lastSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, sectionStyle.Render("── "+item.Section+" ──"))
			lastSection = item.Section
		}

		isCursor := i == s.CursorIdx
		label := item.Label
		value := item.Value

		// Format value display
		var valueFmt string
		switch item.Kind {
		case SettingEnum:
			// Show all options with active highlighted
			var opts []string
			for _, opt := range item.Options {
				if opt == value {
					opts = append(opts, cyanStyle.Render("["+opt+"]"))
				} else {
					opts = append(opts, dimStyle.Render(opt))
				}
			}
			valueFmt = strings.Join(opts, " ")
		case SettingText:
			if s.Editing && isCursor {
				valueFmt = s.EditInput.View()
			} else {
				valueFmt = cyanStyle.Render(value)
			}
		case SettingDisplay:
			valueFmt = dimStyle.Render(value)
		}

		cursor := "  "
		if isCursor {
			cursor = "▸ "
			line := activeStyle.Render(cursor+label+": ") + valueFmt
			if item.Kind == SettingEnum && !s.Editing {
				line += editStyle.Render("  ⏎ cycle")
			} else if item.Kind == SettingText && !s.Editing {
				line += editStyle.Render("  ⏎ edit")
			}
			lines = append(lines, line)
		} else {
			line := dimStyle.Render(cursor) + whiteStyle.Render(label+": ") + valueFmt
			lines = append(lines, line)
		}
	}

	lines = append(lines, "")
	if s.SaveError != "" {
		errorStyle := lipgloss.NewStyle().Foreground(ColorRed).Background(ColorBlack)
		lines = append(lines, errorStyle.Render(s.SaveError))
	}
	lines = append(lines, dimStyle.Render("↑/↓ navigate • enter edit/cycle • esc close"))

	content := strings.Join(lines, "\n")

	modal := ModalStyle.Width(60).Render(content)

	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}
