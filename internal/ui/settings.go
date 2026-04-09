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
	SettingText                       // free text input
	SettingDisplay                    // read-only
	SettingBool                       // toggle true/false
	SettingAction                     // action button — triggers a command on Enter
)

type SettingsItem struct {
	Section  string   // empty for General, "Local (repo)" for Worktrees
	Label    string   // display label
	Key      string   // config key
	Value    string   // current value
	Kind     SettingKind
	Options  []string // for Enum kind
	RepoName string   // empty for global, repo name for local
}

type SettingsModel struct {
	Active     bool
	Items      []SettingsItem
	CursorIdx  int
	Editing    bool
	EditInput  textinput.Model
	Config     *config.Config
	Scroll     int    // scroll offset for long lists
	SaveError  string // shown if save fails
	SaveStatus string // shown on successful save
	ViewHeight int    // last known terminal height for scroll calc
	ViewWidth  int    // last known terminal width for tab hit-test
	saveGen    int    // generation counter for save status clear timer

	// Tabs
	ActiveTab int      // 0 = General, 1 = Worktrees
	TabNames  []string // ["General", "Worktrees"]
	RepoNames []string // stored for rebuilding items on tab switch
}

// Messages
type SettingsSavedMsg struct{}
type SettingsSaveClearMsg struct {
	Gen int // only clear if this matches current generation
}
type SettingsActionMsg struct {
	Action string // e.g. "CHECK_FOR_UPDATE_ACTION"
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func formatLastRefresh(ts int64) string {
	if ts <= 0 {
		return "never"
	}
	t := time.Unix(ts, 0)
	dur := time.Since(t)
	if dur < time.Minute {
		return "just now"
	} else if dur < time.Hour {
		return fmt.Sprintf("%dm ago", int(dur.Minutes()))
	} else if dur < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(dur.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(dur.Hours()/24))
}

func NewSettings(cfg *config.Config, repoNames []string) SettingsModel {
	ti := textinput.New()
	ti.CharLimit = 100
	ti.Width = 40

	s := SettingsModel{
		Active:    true,
		Config:    cfg,
		EditInput: ti,
		ActiveTab: 0,
		TabNames:  []string{"General", "Worktrees"},
		RepoNames: repoNames,
	}
	s.buildItems(repoNames)
	return s
}

func (s *SettingsModel) buildItems(repoNames []string) {
	s.Items = nil
	s.RepoNames = repoNames

	if s.ActiveTab == 0 {
		// General settings — no section headers (the tab IS the section)
		s.Items = append(s.Items,
			SettingsItem{Label: "IDE Command", Key: "IDE_COMMAND",
				Value: s.Config.Global.IDECommand, Kind: SettingEnum,
				Options: []string{"windsurf", "code", "cursor", "zed"}},
			SettingsItem{Label: "AI CLI Command", Key: "AI_CLI_COMMAND",
				Value: s.Config.Global.AICliCommand, Kind: SettingText},
			SettingsItem{Label: "Package Manager", Key: "PACKAGE_MANAGER",
				Value: s.Config.Global.PackageManager, Kind: SettingEnum,
				Options: []string{"pnpm", "npm", "yarn", "bun"}},
			SettingsItem{Label: "Auto Refresh", Key: "AUTO_REFRESH",
				Value: s.Config.Global.AutoRefresh, Kind: SettingEnum,
				Options: []string{"OFF", "15M", "30M", "1H", "6H", "12H", "24H"}},
			SettingsItem{Label: "Terminal", Key: "TERMINAL",
				Value: s.Config.Global.Terminal, Kind: SettingEnum,
				Options: []string{"ghostty", "iterm", "terminal", "wezterm", "alacritty", "kitty"}},
			SettingsItem{Label: "Check for Updates", Key: "DAILY_CHECK_FOR_UPDATES",
				Value: boolToStr(s.Config.Global.CheckForUpdates), Kind: SettingBool},
			SettingsItem{Label: "Auto Update", Key: "AUTO_UPDATE_NEW_RELEASE",
				Value: boolToStr(s.Config.Global.AutoUpdate), Kind: SettingBool},
			SettingsItem{Label: "Open .env in IDE", Key: "OPEN_ENV_IDE",
				Value: boolToStr(s.Config.Global.OpenEnvInIDE), Kind: SettingBool},
			SettingsItem{Label: "Check for Update", Key: "CHECK_FOR_UPDATE_ACTION",
				Value: "Press enter to check", Kind: SettingAction},
		)
	} else {
		// Worktrees tab: per-repo local settings
		for _, repo := range repoNames {
			key := strings.ToUpper(repo)
			rc, ok := s.Config.Local[key]
			if !ok {
				rc = config.DefaultRepoLocal()
			}

			s.Items = append(s.Items,
				SettingsItem{Section: "Local (" + repo + ")", Label: "Basis Branch", Key: "BASIS_BRANCH",
					Value: rc.BasisBranch, Kind: SettingText, RepoName: repo},
				SettingsItem{Section: "Local (" + repo + ")", Label: "Last Refresh", Key: "LAST_REFRESH",
					Value: formatLastRefresh(rc.LastRefresh), Kind: SettingDisplay, RepoName: repo},
			)
		}
	}
}

func (s SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.Editing {
			return s.handleEditKey(msg)
		}
		return s.handleNavKey(msg)
	case tea.MouseMsg:
		// Tab click detection
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if tabIdx := s.hitTestTab(msg.X, msg.Y); tabIdx >= 0 && tabIdx != s.ActiveTab {
				s.ActiveTab = tabIdx
				s.CursorIdx = 0
				s.Scroll = 0
				s.Editing = false
				s.SaveError = ""
				s.SaveStatus = ""
				s.buildItems(s.RepoNames)
				return s, nil
			}
		}
		if msg.Button == tea.MouseButtonWheelUp {
			if s.Scroll > 0 {
				s.Scroll--
			}
		} else if msg.Button == tea.MouseButtonWheelDown {
			maxScroll := len(s.Items) + len(s.Items)/2
			if s.Scroll < maxScroll {
				s.Scroll++
			}
		}
		return s, nil
	case SettingsSaveClearMsg:
		if msg.Gen == s.saveGen {
			s.SaveStatus = ""
		}
		return s, nil
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
	case "tab":
		s.ActiveTab = (s.ActiveTab + 1) % len(s.TabNames)
		s.CursorIdx = 0
		s.Scroll = 0
		s.Editing = false
		s.SaveError = ""
		s.SaveStatus = ""
		s.buildItems(s.RepoNames)
		return s, nil
	case "up", "k":
		s.moveCursor(-1)
	case "down", "j":
		s.moveCursor(1)
	case "enter", " ":
		if len(s.Items) == 0 {
			return s, nil
		}
		item := &s.Items[s.CursorIdx]
		switch item.Kind {
		case SettingEnum:
			for i, opt := range item.Options {
				if opt == item.Value {
					item.Value = item.Options[(i+1)%len(item.Options)]
					return s, s.applyChange(*item)
				}
			}
			if len(item.Options) > 0 {
				item.Value = item.Options[0]
				return s, s.applyChange(*item)
			}
		case SettingText:
			s.Editing = true
			s.EditInput.SetValue(item.Value)
			s.EditInput.Focus()
			return s, textinput.Blink
		case SettingBool:
			if item.Value == "true" {
				item.Value = "false"
			} else {
				item.Value = "true"
			}
			return s, s.applyChange(*item)
		case SettingAction:
			action := item.Key
			return s, func() tea.Msg { return SettingsActionMsg{Action: action} }
		}
	}
	return s, nil
}

func (s *SettingsModel) moveCursor(delta int) {
	if len(s.Items) == 0 {
		return
	}
	s.CursorIdx += delta
	if s.CursorIdx < 0 {
		s.CursorIdx = 0
	}
	if s.CursorIdx >= len(s.Items) {
		s.CursorIdx = len(s.Items) - 1
	}
	for s.Items[s.CursorIdx].Kind == SettingDisplay {
		if delta > 0 && s.CursorIdx < len(s.Items)-1 {
			s.CursorIdx++
		} else if delta < 0 && s.CursorIdx > 0 {
			s.CursorIdx--
		} else {
			break
		}
	}
	s.ensureCursorVisible()
}

func (s *SettingsModel) ensureCursorVisible() {
	// border(2) + padding(2) + title(2) + tabs(3) + indicators(2) + blank(1) + footer(2)
	maxVisible := s.ViewHeight - 14
	if maxVisible < 5 {
		maxVisible = 5
	}

	cursorLine := s.cursorContentLine()
	if cursorLine < s.Scroll {
		s.Scroll = cursorLine
	}
	if cursorLine >= s.Scroll+maxVisible {
		s.Scroll = cursorLine - maxVisible + 1
	}
	if s.Scroll < 0 {
		s.Scroll = 0
	}
}

func (s *SettingsModel) cursorContentLine() int {
	line := 0
	lastSection := ""
	for i := 0; i <= s.CursorIdx && i < len(s.Items); i++ {
		if s.Items[i].Section != lastSection {
			if lastSection != "" {
				line++ // blank line before new section
			}
			if s.Items[i].Section != "" {
				line++ // section header line
			}
			lastSection = s.Items[i].Section
		}
		if i < s.CursorIdx {
			line++
		}
	}
	return line
}

func (s *SettingsModel) previousValue(item SettingsItem) string {
	if item.RepoName == "" {
		switch item.Key {
		case "IDE_COMMAND":
			return s.Config.Global.IDECommand
		case "AI_CLI_COMMAND":
			return s.Config.Global.AICliCommand
		case "PACKAGE_MANAGER":
			return s.Config.Global.PackageManager
		case "AUTO_REFRESH":
			return s.Config.Global.AutoRefresh
		case "TERMINAL":
			return s.Config.Global.Terminal
		case "DAILY_CHECK_FOR_UPDATES":
			return boolToStr(s.Config.Global.CheckForUpdates)
		case "AUTO_UPDATE_NEW_RELEASE":
			return boolToStr(s.Config.Global.AutoUpdate)
		case "OPEN_ENV_IDE":
			return boolToStr(s.Config.Global.OpenEnvInIDE)
		}
	} else {
		key := strings.ToUpper(item.RepoName)
		if rc, ok := s.Config.Local[key]; ok {
			switch item.Key {
			case "BASIS_BRANCH":
				return rc.BasisBranch
			}
		}
	}
	return ""
}

func (s SettingsModel) handleEditKey(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		s.Editing = false
		item := &s.Items[s.CursorIdx]
		item.Value = strings.TrimSpace(s.EditInput.Value())
		return s, s.applyChange(*item)
	case "esc":
		s.Editing = false
		return s, nil
	default:
		var cmd tea.Cmd
		s.EditInput, cmd = s.EditInput.Update(msg)
		return s, cmd
	}
}

func (s *SettingsModel) applyChange(item SettingsItem) tea.Cmd {
	s.SaveError = ""
	s.SaveStatus = ""

	if item.Kind == SettingText && item.Value == "" && item.Key != "AI_CLI_COMMAND" {
		s.SaveError = item.Label + " cannot be empty"
		s.Items[s.CursorIdx].Value = s.previousValue(item)
		return nil
	}

	var saveErr error
	if item.RepoName == "" {
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
		case "DAILY_CHECK_FOR_UPDATES":
			s.Config.Global.CheckForUpdates = item.Value == "true"
		case "AUTO_UPDATE_NEW_RELEASE":
			s.Config.Global.AutoUpdate = item.Value == "true"
		case "OPEN_ENV_IDE":
			s.Config.Global.OpenEnvInIDE = item.Value == "true"
		}
		saveErr = s.Config.SaveGlobal()
	} else {
		switch item.Key {
		case "BASIS_BRANCH":
			saveErr = s.Config.SetRepoBasisBranch(item.RepoName, item.Value)
		}
	}
	if saveErr != nil {
		s.SaveError = "Failed to save: " + saveErr.Error()
		return nil
	}
	s.SaveStatus = "Saved!"
	s.saveGen++
	gen := s.saveGen
	return tea.Batch(
		func() tea.Msg { return SettingsSavedMsg{} },
		tea.Tick(2*time.Second, func(time.Time) tea.Msg { return SettingsSaveClearMsg{Gen: gen} }),
	)
}

// modalMetrics computes the modal layout dimensions matching View().
func (s *SettingsModel) modalMetrics() (modalWidth, modalLeft, contentLeft, contentTopY int) {
	w := s.ViewWidth
	h := s.ViewHeight
	if w == 0 || h == 0 {
		return 78, 0, 0, 0
	}

	modalWidth = 78
	if w-4 < modalWidth {
		modalWidth = w - 4
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	// ModalStyle: DoubleBorder (1 char each side) + Padding(1, 2)
	renderedW := modalWidth + 2 // border left + right
	modalLeft = (w - renderedW) / 2
	contentLeft = modalLeft + 1 + 2 // border + padding

	// Estimate modal height for vertical centering
	// Content: title(1) + blank(1) + tabbar(1) + separator(1) + blank(1) + items + footer
	contentLines := 5 + len(s.Items) + 3 // rough estimate
	modalContentH := contentLines + 2     // padding top + bottom
	renderedH := modalContentH + 2        // border top + bottom
	if renderedH > h {
		renderedH = h
	}
	modalTopY := (h - renderedH) / 2
	contentTopY = modalTopY + 1 + 1 // border + padding

	return
}

// hitTestTab checks if the mouse click is on a tab and returns the tab index (-1 if none).
func (s *SettingsModel) hitTestTab(mouseX, mouseY int) int {
	_, _, contentLeft, contentTopY := s.modalMetrics()

	// Tab bar is at content line 2 (title=0, blank=1, tabbar=2)
	tabBarY := contentTopY + 2
	if mouseY != tabBarY {
		return -1
	}

	curX := contentLeft
	for i, name := range s.TabNames {
		var tabText string
		if i == s.ActiveTab {
			tabText = " [ " + name + " ] "
		} else {
			tabText = "   " + name + "   "
		}
		tabW := lipgloss.Width(tabText)
		if mouseX >= curX && mouseX < curX+tabW {
			return i
		}
		// Account for the separator "│" between tabs
		if i < len(s.TabNames)-1 {
			curX += tabW + 1 // +1 for "│"
		}
	}
	return -1
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

	// Tab bar
	activeTabStyle := lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack).Bold(true)
	inactiveTabStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	tabHintStyle := lipgloss.NewStyle().Foreground(ColorDarkGreen).Background(ColorBlack)
	sepStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)

	var tabParts []string
	for i, name := range s.TabNames {
		if i == s.ActiveTab {
			tabParts = append(tabParts, activeTabStyle.Render(" [ "+name+" ] "))
		} else {
			tabParts = append(tabParts, inactiveTabStyle.Render("   "+name+"   "))
		}
	}
	tabBar := strings.Join(tabParts, sepStyle.Render("│")) + tabHintStyle.Render("  (tab)")
	lines = append(lines, tabBar)
	lines = append(lines, dimStyle.Render(strings.Repeat("─", 40)))
	lines = append(lines, "")

	// Items
	lastSection := ""
	for i, item := range s.Items {
		// Section divider (only used in Worktrees tab)
		if item.Section != "" && item.Section != lastSection {
			if lastSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, sectionStyle.Render("── "+item.Section+" ──"))
			lastSection = item.Section
		}

		isCursor := i == s.CursorIdx
		label := item.Label

		var valueFmt string
		switch item.Kind {
		case SettingEnum:
			var opts []string
			found := false
			for _, opt := range item.Options {
				if opt == item.Value {
					opts = append(opts, cyanStyle.Render("["+opt+"]"))
					found = true
				} else {
					opts = append(opts, dimStyle.Render(opt))
				}
			}
			if !found && item.Value != "" {
				opts = append([]string{cyanStyle.Render("[" + item.Value + "]")}, opts...)
			}
			valueFmt = strings.Join(opts, " ")
		case SettingText:
			if s.Editing && isCursor {
				valueFmt = s.EditInput.View()
			} else {
				valueFmt = cyanStyle.Render(item.Value)
			}
		case SettingBool:
			if item.Value == "true" {
				valueFmt = cyanStyle.Render("● enabled")
			} else {
				valueFmt = dimStyle.Render("○ disabled")
			}
		case SettingDisplay:
			valueFmt = dimStyle.Render(item.Value)
		case SettingAction:
			valueFmt = dimStyle.Render(item.Value)
		}

		cursor := "  "
		if isCursor {
			cursor = "▸ "
			line := activeStyle.Render(cursor+label+": ") + valueFmt
			if item.Kind == SettingEnum && !s.Editing {
				line += editStyle.Render("  ⏎ cycle")
			} else if item.Kind == SettingText && !s.Editing {
				line += editStyle.Render("  ⏎ edit")
			} else if item.Kind == SettingBool {
				line += editStyle.Render("  ⏎ toggle")
			} else if item.Kind == SettingAction {
				line += editStyle.Render("  ⏎ run")
			}
			lines = append(lines, line)
		} else {
			line := dimStyle.Render(cursor) + whiteStyle.Render(label+": ") + valueFmt
			lines = append(lines, line)
		}
	}

	// Empty state for Worktrees tab
	if s.ActiveTab == 1 && len(s.Items) == 0 {
		lines = append(lines, dimStyle.Render("  No worktrees configured"))
	}

	// Footer lines (always visible, not scrolled)
	var footerLines []string
	if s.SaveError != "" {
		errorStyle := lipgloss.NewStyle().Foreground(ColorRed).Background(ColorBlack)
		footerLines = append(footerLines, errorStyle.Render(s.SaveError))
	} else if s.SaveStatus != "" {
		savedStyle := lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack).Bold(true)
		footerLines = append(footerLines, savedStyle.Render("✓ "+s.SaveStatus))
	}
	footerLines = append(footerLines, dimStyle.Render("↑/↓ navigate • enter edit/cycle • tab switch • esc close"))

	// Apply scroll: reserve space for modal chrome, title, tabs, scroll indicators, and footer
	// Modal border(2) + padding(2) + title(2) + tabs(3) + scroll indicators(2) + blank(1) = 12
	maxVisible := height - 12 - len(footerLines)
	if maxVisible < 5 {
		maxVisible = 5
	}

	// Content lines: everything after title + blank + tabs + separator + blank
	titleLines := lines[:5] // "Settings", blank, tab bar, separator, blank
	contentLines := lines[5:]

	if len(contentLines) > maxVisible {
		end := s.Scroll + maxVisible
		if end > len(contentLines) {
			end = len(contentLines)
			s.Scroll = end - maxVisible
			if s.Scroll < 0 {
				s.Scroll = 0
			}
		}
		visibleContent := contentLines[s.Scroll:end]

		var scrolledLines []string
		scrolledLines = append(scrolledLines, titleLines...)
		if s.Scroll > 0 {
			scrolledLines = append(scrolledLines, dimStyle.Render("  ↑ more"))
		}
		scrolledLines = append(scrolledLines, visibleContent...)
		if end < len(contentLines) {
			scrolledLines = append(scrolledLines, dimStyle.Render("  ↓ more"))
		}
		scrolledLines = append(scrolledLines, "")
		scrolledLines = append(scrolledLines, footerLines...)
		lines = scrolledLines
	} else {
		lines = append(lines, "")
		lines = append(lines, footerLines...)
	}

	content := strings.Join(lines, "\n")

	modalWidth := 78
	if width-4 < modalWidth {
		modalWidth = width - 4
	}
	if modalWidth < 50 {
		modalWidth = 50
	}
	modal := ModalStyle.Width(modalWidth).Render(content)

	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}
