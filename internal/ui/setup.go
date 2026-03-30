package ui

import (
	"lts-revamp/internal/config"
	"lts-revamp/internal/version"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type setupOption struct {
	Label   string
	Value   string
	IsCustom bool // true = "Type here" option
}

type setupStep struct {
	Question string
	Options  []setupOption
	ConfigKey string
}

// SetupModel is the first-run configuration wizard.
type SetupModel struct {
	steps     []setupStep
	stepIdx   int
	cursorIdx int
	custom    bool // typing custom value
	input     textinput.Model
	config    config.GlobalConfig
	width     int
	height    int
}

func NewSetup() SetupModel {
	ti := textinput.New()
	ti.CharLimit = 100
	ti.Width = 40
	ti.Placeholder = "type your command..."

	return SetupModel{
		steps: []setupStep{
			{
				Question:  "What IDE do you use?",
				ConfigKey: "IDE_COMMAND",
				Options: []setupOption{
					{Label: "Windsurf", Value: "windsurf"},
					{Label: "VS Code", Value: "code"},
					{Label: "Cursor", Value: "cursor"},
					{Label: "Zed", Value: "zed"},
					{Label: "Other (type command)", Value: "", IsCustom: true},
				},
			},
			{
				Question:  "What terminal do you use?",
				ConfigKey: "TERMINAL",
				Options: []setupOption{
					{Label: "Default (system)", Value: "terminal"},
					{Label: "Ghostty", Value: "ghostty"},
					{Label: "iTerm2", Value: "iterm"},
					{Label: "WezTerm", Value: "wezterm"},
					{Label: "Alacritty", Value: "alacritty"},
					{Label: "Kitty", Value: "kitty"},
					{Label: "Other (type command)", Value: "", IsCustom: true},
				},
			},
			{
				Question:  "What AI code assistant do you use?",
				ConfigKey: "AI_CLI_COMMAND",
				Options: []setupOption{
					{Label: "Claude", Value: "claude"},
					{Label: "Claude (skip permissions)", Value: "claude --dangerously-skip-permissions"},
					{Label: "OpenCode", Value: "opencode"},
					{Label: "None", Value: ""},
					{Label: "Other (type command)", Value: "", IsCustom: true},
				},
			},
			{
				Question:  "What package manager do you prefer?",
				ConfigKey: "PACKAGE_MANAGER",
				Options: []setupOption{
					{Label: "pnpm", Value: "pnpm"},
					{Label: "npm", Value: "npm"},
					{Label: "yarn", Value: "yarn"},
					{Label: "bun", Value: "bun"},
				},
			},
			{
				Question:  "Auto-refresh interval for repos?",
				ConfigKey: "AUTO_REFRESH",
				Options: []setupOption{
					{Label: "Every 30 minutes", Value: "30M"},
					{Label: "Every hour", Value: "1H"},
					{Label: "Every 6 hours", Value: "6H"},
					{Label: "Every 24 hours", Value: "24H"},
					{Label: "Every 15 minutes", Value: "15M"},
				},
			},
		},
		config: config.DefaultGlobal(),
		input:  ti,
	}
}

func (s SetupModel) Init() tea.Cmd {
	return nil
}

func (s SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case tea.KeyMsg:
		if s.custom {
			return s.handleCustomInput(msg)
		}
		return s.handleSelection(msg)
	}

	if s.custom {
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s SetupModel) handleSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	step := s.steps[s.stepIdx]

	switch msg.String() {
	case "ctrl+c":
		return s, tea.Quit
	case "up", "k":
		if s.cursorIdx > 0 {
			s.cursorIdx--
		}
	case "down", "j":
		if s.cursorIdx < len(step.Options)-1 {
			s.cursorIdx++
		}
	case "enter", " ":
		opt := step.Options[s.cursorIdx]
		if opt.IsCustom {
			s.custom = true
			s.input.SetValue("")
			s.input.Focus()
			return s, textinput.Blink
		}
		s.applyOption(opt.Value)
		return s.advance()
	}
	return s, nil
}

func (s SetupModel) handleCustomInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		value := strings.TrimSpace(s.input.Value())
		if value == "" {
			return s, nil
		}
		s.custom = false
		s.applyOption(value)
		return s.advance()
	case "esc":
		s.custom = false
		return s, nil
	default:
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return s, cmd
	}
}

func (s *SetupModel) applyOption(value string) {
	step := s.steps[s.stepIdx]
	switch step.ConfigKey {
	case "IDE_COMMAND":
		s.config.IDECommand = value
	case "TERMINAL":
		s.config.Terminal = value
	case "AI_CLI_COMMAND":
		s.config.AICliCommand = value
	case "PACKAGE_MANAGER":
		s.config.PackageManager = value
	case "AUTO_REFRESH":
		s.config.AutoRefresh = value
	}
}

func (s SetupModel) advance() (tea.Model, tea.Cmd) {
	s.stepIdx++
	s.cursorIdx = 0
	s.custom = false

	if s.stepIdx >= len(s.steps) {
		// Save the config and quit the setup program
		cfg := config.Config{
			Global: s.config,
			Local:  make(map[string]config.RepoLocalConfig),
		}
		cfg.SaveGlobal()
		return s, tea.Quit
	}
	return s, nil
}

func (s SetupModel) View() string {
	if s.width == 0 || s.stepIdx >= len(s.steps) {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Background(ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	questionStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Background(ColorBlack)
	activeStyle := lipgloss.NewStyle().Foreground(ColorCyan).Background(ColorBlack).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	progressStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	bannerStyle := lipgloss.NewStyle().Foreground(ColorDarkGreen).Background(ColorBlack).Bold(true)

	var lines []string

	// Banner — only show if terminal is tall enough
	step := s.steps[s.stepIdx]
	optionCount := len(step.Options)
	// Modal needs: border(2) + padding(2) + progress(1) + blanks(4) + question(1) + options + help(1) = 11 + options
	// Banner adds 8 lines (6 banner + version + blank)
	needsHeight := 11 + optionCount
	hasBanner := s.height >= needsHeight+12 // 12 = banner(8) + title(2) + breathing room(2)
	if hasBanner {
		for _, line := range ltsBanner {
			lines = append(lines, bannerStyle.Render(line))
		}
		lines = append(lines, dimStyle.Render("v"+version.Version))
		lines = append(lines, "")
		lines = append(lines, titleStyle.Render("Welcome! Let's configure LTS."))
	} else {
		lines = append(lines, titleStyle.Render("LTS")+" "+dimStyle.Render("v"+version.Version)+" "+titleStyle.Render("— Setup"))
	}
	lines = append(lines, "")

	// Progress
	progress := progressStyle.Render(
		strings.Repeat("●", s.stepIdx+1) + strings.Repeat("○", len(s.steps)-s.stepIdx-1) +
			"  " + string(rune('0'+s.stepIdx+1)) + "/" + string(rune('0'+len(s.steps))),
	)
	lines = append(lines, progress)
	lines = append(lines, "")

	// Question
	lines = append(lines, questionStyle.Render(step.Question))
	lines = append(lines, "")

	// Options
	for i, opt := range step.Options {
		cursor := "  "
		if i == s.cursorIdx {
			cursor = "▸ "
			if opt.IsCustom && s.custom {
				lines = append(lines, activeStyle.Render(cursor+opt.Label+": ")+s.input.View())
			} else {
				lines = append(lines, activeStyle.Render(cursor+opt.Label))
			}
		} else {
			lines = append(lines, inactiveStyle.Render(cursor+opt.Label))
		}
	}

	lines = append(lines, "")
	if s.custom {
		lines = append(lines, dimStyle.Render("enter confirm • esc back"))
	} else {
		lines = append(lines, dimStyle.Render("↑/↓ navigate • enter select"))
	}

	content := strings.Join(lines, "\n")

	modalWidth := 56
	if s.width-4 < modalWidth {
		modalWidth = s.width - 4
	}
	if modalWidth < 40 {
		modalWidth = 40
	}
	modal := ModalStyle.Width(modalWidth).Render(content)

	return lipgloss.Place(
		s.width, s.height,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().Background(ColorBlack).Render(modal),
	)
}
