package ui

import (
	"fmt"
	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/version"
	"os"
	"strconv"
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
	Section  string // empty for General, "Local (repo)" for Worktrees
	Label    string // display label
	Key      string // config key
	Value    string // current value
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
	ActiveTab  int      // index into TabNames
	HoveredTab int      // -1 = none
	TabNames   []string // ["Preferences", "Workspace", "Worktrees", "Diagnostics"]
	RepoNames  []string // stored for rebuilding items on tab switch
}

// Tab indices
const (
	TabPreferences = iota
	TabWorkspace
	TabWorktrees
	TabDiagnostics
)

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
		Active:     true,
		Config:     cfg,
		EditInput:  ti,
		ActiveTab:  TabPreferences,
		HoveredTab: -1,
		TabNames:   []string{"Preferences", "Workspace", "Worktrees", "Diagnostics"},
		RepoNames:  repoNames,
	}
	s.buildItems(repoNames)
	return s
}

func (s *SettingsModel) buildItems(repoNames []string) {
	s.Items = nil
	s.RepoNames = repoNames

	switch s.ActiveTab {
	case TabPreferences:
		// Tool preferences — which commands LTS drives
		s.Items = append(s.Items,
			SettingsItem{Label: "IDE Command", Key: "IDE_COMMAND",
				Value: s.Config.Global.IDECommand, Kind: SettingEnum,
				Options: []string{"windsurf", "code", "cursor", "zed"}},
			SettingsItem{Label: "AI CLI Command", Key: "AI_CLI_COMMAND",
				Value: s.Config.Global.AICliCommand, Kind: SettingText},
			SettingsItem{Label: "Terminal", Key: "TERMINAL",
				Value: s.Config.Global.Terminal, Kind: SettingEnum,
				Options: []string{"ghostty", "iterm", "terminal", "wezterm", "alacritty", "kitty"}},
			SettingsItem{Label: "Multiplexer", Key: "TERMINAL_MULTIPLEXER",
				Value: s.Config.Global.Multiplexer, Kind: SettingEnum,
				Options: []string{"none", "tmux"}},
		)
		if s.Config.Global.Multiplexer == "tmux" {
			// Layout of newly created sessions (existing ones keep theirs)
			s.Items = append(s.Items,
				SettingsItem{Label: "Tmux AI Pane Width %", Key: "TMUX_AI_PANE_WIDTH",
					Value: fmt.Sprintf("%d", s.Config.Global.TmuxAIPaneWidth), Kind: SettingText},
				SettingsItem{Label: "Tmux Right Panes", Key: "TMUX_RIGHT_PANES",
					Value: fmt.Sprintf("%d", s.Config.Global.TmuxRightPanes), Kind: SettingEnum,
					Options: []string{"1", "2", "3"}},
			)
		}
		s.Items = append(s.Items,
			SettingsItem{Label: "Default Package Manager", Key: "PACKAGE_MANAGER",
				Value: s.Config.Global.PackageManager, Kind: SettingEnum,
				Options: []string{"pnpm", "npm", "yarn", "bun"}},
			SettingsItem{Label: "Auto Refresh", Key: "AUTO_REFRESH",
				Value: s.Config.Global.AutoRefresh, Kind: SettingEnum,
				Options: []string{"OFF", "15M", "30M", "1H", "6H", "12H", "24H"}},
			SettingsItem{Label: "Check for Updates", Key: "DAILY_CHECK_FOR_UPDATES",
				Value: boolToStr(s.Config.Global.CheckForUpdates), Kind: SettingBool},
			SettingsItem{Label: "Auto Update", Key: "AUTO_UPDATE_NEW_RELEASE",
				Value: boolToStr(s.Config.Global.AutoUpdate), Kind: SettingBool},
		)
	case TabWorkspace:
		// What goes into generated worktrees and workspace files
		s.Items = append(s.Items,
			SettingsItem{Label: "Copy .env Files to Worktree", Key: "COPY_ENV_FILES",
				Value: boolToStr(s.Config.Global.CopyEnvFiles), Kind: SettingBool},
			SettingsItem{Label: "Copy .mcp.json Files to Worktree", Key: "COPY_MCP_JSON",
				Value: boolToStr(s.Config.Global.CopyMCPJson), Kind: SettingBool},
			SettingsItem{Label: "Open .env in IDE", Key: "OPEN_ENV_IDE",
				Value: boolToStr(s.Config.Global.OpenEnvInIDE), Kind: SettingBool},
			SettingsItem{Label: "New Worktree Package Install", Key: "NEW_WT_PACKAGE_INSTALL",
				Value: boolToStr(s.Config.Global.InstallOnCreate), Kind: SettingBool},
		)
	case TabWorktrees:
		// Per-repo local settings
		for _, repo := range repoNames {
			key := strings.ToUpper(repo)
			rc, ok := s.Config.Local[key]
			if !ok {
				rc = config.DefaultRepoLocal()
			}

			pm := rc.PackageManager
			if pm == "" {
				pm = "default"
			}
			s.Items = append(s.Items,
				SettingsItem{Section: "Local (" + repo + ")", Label: "Basis Branch", Key: "BASIS_BRANCH",
					Value: rc.BasisBranch, Kind: SettingText, RepoName: repo},
				SettingsItem{Section: "Local (" + repo + ")", Label: "Package Manager", Key: "REPO_PACKAGE_MANAGER",
					Value: pm, Kind: SettingEnum, RepoName: repo,
					Options: []string{"default", "pnpm", "npm", "yarn", "bun"}},
				SettingsItem{Section: "Local (" + repo + ")", Label: "Last Refresh", Key: "LAST_REFRESH",
					Value: formatLastRefresh(rc.LastRefresh), Kind: SettingDisplay, RepoName: repo},
			)
		}
	case TabDiagnostics:
		s.Items = append(s.Items, s.diagnosticItems()...)
	}
}

// diagnosticItems computes the health checks shown on the Diagnostics tab.
func (s *SettingsModel) diagnosticItems() []SettingsItem {
	gitStatus := "not found ✗ — install git"
	if v, ok := git.GitVersion(); ok {
		gitStatus = v + " ✓"
	} else if v != "" {
		gitStatus = v + " ✗ — LTS needs git 2.17+"
	}

	repoStatus := fmt.Sprintf("%d found ✓", len(s.RepoNames))
	if len(s.RepoNames) == 0 {
		repoStatus = "none found ✗ — run lts in your projects folder (lts --dir <path>)"
	}

	configStatus := "writable ✓"
	if f, err := os.OpenFile(config.GlobalConfigPath(), os.O_WRONLY, 0); err != nil {
		configStatus = "not writable ✗ — " + err.Error()
	} else {
		f.Close()
	}

	build := version.Display()
	if version.IsDev() {
		build += " — built from source"
	} else {
		build += " — official release"
	}

	binPath := "unknown"
	if exe, err := os.Executable(); err == nil {
		binPath = shortenHome(exe)
	}

	tmuxStatus := "not installed"
	if v, ok := opener.TmuxVersion(); ok {
		tmuxStatus = v + " ✓"
	} else if s.Config.Global.Multiplexer == "tmux" {
		tmuxStatus = "not found ✗ — install tmux or set Multiplexer to none"
	}

	return []SettingsItem{
		{Label: "Git", Key: "DIAG_GIT", Value: gitStatus, Kind: SettingDisplay},
		{Label: "Working Directory", Key: "DIAG_WORKDIR", Value: shortenHome(s.Config.WorkDir), Kind: SettingDisplay},
		{Label: "Repositories", Key: "DIAG_REPOS", Value: repoStatus, Kind: SettingDisplay},
		{Label: "Config File", Key: "DIAG_CONFIG", Value: configStatus, Kind: SettingDisplay},
		{Label: "Tmux", Key: "DIAG_TMUX", Value: tmuxStatus, Kind: SettingDisplay},
		{Label: "Build", Key: "DIAG_BUILD", Value: build, Kind: SettingDisplay},
		{Label: "Binary", Key: "DIAG_BINARY", Value: binPath, Kind: SettingDisplay},
		{Label: "Last Update Check", Key: "DIAG_UPDATE", Value: formatLastRefresh(s.Config.Global.LastUpdateCheck), Kind: SettingDisplay},
		{Label: "Check for Update", Key: "CHECK_FOR_UPDATE_ACTION", Value: "Press enter to check", Kind: SettingAction},
		{Label: "Kill All Tmux Sessions", Key: "KILL_TMUX_SESSIONS_ACTION", Value: "Press enter to kill all lts- sessions", Kind: SettingAction},
		{Label: "Reset LTS (Open Setup Wizard)", Key: "RESET_SETUP_ACTION", Value: "Press enter to rerun setup", Kind: SettingAction},
	}
}

// shortenHome replaces the home-directory prefix with ~ for display.
func shortenHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func (s SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.Editing {
			return s.handleEditKey(msg)
		}
		return s.handleNavKey(msg)
	case tea.MouseMsg:
		// Hover: tabs and setting rows
		if msg.Action == tea.MouseActionMotion {
			s.HoveredTab = s.hitTestTab(msg.X, msg.Y)
			if idx := s.hitTestItem(msg.X, msg.Y); idx >= 0 {
				s.CursorIdx = idx
			}
		}
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
			// Click on a setting row activates it (cycle/edit/toggle/run)
			if idx := s.hitTestItem(msg.X, msg.Y); idx >= 0 {
				s.CursorIdx = idx
				return s.activateCursor()
			}
		}
		if msg.Button == tea.MouseButtonWheelUp {
			if s.Scroll > 0 {
				s.Scroll--
			}
		} else if msg.Button == tea.MouseButtonWheelDown {
			_, _, total, maxVis := s.visibleWindow()
			if s.Scroll < total-maxVis {
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
		return s.activateCursor()
	}
	return s, nil
}

// activateCursor performs the enter action for the item under the cursor:
// cycle enums, edit text, toggle bools, run actions.
func (s SettingsModel) activateCursor() (SettingsModel, tea.Cmd) {
	if len(s.Items) == 0 || s.CursorIdx < 0 || s.CursorIdx >= len(s.Items) {
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
	maxVisible := s.maxVisibleRows()

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
	return s.itemContentLine(s.CursorIdx)
}

// itemContentLine returns the content-region row of item idx (section headers
// and blank lines included). Must match View's line construction.
func (s *SettingsModel) itemContentLine(idx int) int {
	line := 0
	lastSection := ""
	for i := 0; i <= idx && i < len(s.Items); i++ {
		if s.Items[i].Section != lastSection {
			if lastSection != "" {
				line++ // blank line before new section
			}
			if s.Items[i].Section != "" {
				line++ // section header line
			}
			lastSection = s.Items[i].Section
		}
		if i < idx {
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
		case "TMUX_AI_PANE_WIDTH":
			return fmt.Sprintf("%d", s.Config.Global.TmuxAIPaneWidth)
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
		case "TERMINAL_MULTIPLEXER":
			s.Config.Global.Multiplexer = item.Value
		case "TMUX_AI_PANE_WIDTH":
			n, err := strconv.Atoi(strings.TrimSpace(item.Value))
			if err != nil || n < 20 || n > 90 {
				s.SaveError = "AI pane width must be a number between 20 and 90"
				s.Items[s.CursorIdx].Value = s.previousValue(item)
				return nil
			}
			s.Config.Global.TmuxAIPaneWidth = n
		case "TMUX_RIGHT_PANES":
			if n, err := strconv.Atoi(item.Value); err == nil {
				s.Config.Global.TmuxRightPanes = n
			}
		case "DAILY_CHECK_FOR_UPDATES":
			s.Config.Global.CheckForUpdates = item.Value == "true"
		case "AUTO_UPDATE_NEW_RELEASE":
			s.Config.Global.AutoUpdate = item.Value == "true"
		case "OPEN_ENV_IDE":
			s.Config.Global.OpenEnvInIDE = item.Value == "true"
		case "NEW_WT_PACKAGE_INSTALL":
			s.Config.Global.InstallOnCreate = item.Value == "true"
		case "COPY_ENV_FILES":
			s.Config.Global.CopyEnvFiles = item.Value == "true"
		case "COPY_MCP_JSON":
			s.Config.Global.CopyMCPJson = item.Value == "true"
		}
		saveErr = s.Config.SaveGlobal()
	} else {
		switch item.Key {
		case "BASIS_BRANCH":
			saveErr = s.Config.SetRepoBasisBranch(item.RepoName, item.Value)
		case "REPO_PACKAGE_MANAGER":
			pm := item.Value
			if pm == "default" {
				pm = ""
			}
			saveErr = s.Config.SetRepoPackageManager(item.RepoName, pm)
		}
	}
	if saveErr != nil {
		s.SaveError = "Failed to save: " + saveErr.Error()
		return nil
	}
	if item.Key == "TERMINAL_MULTIPLEXER" {
		// Reveal/hide the tmux layout settings
		s.buildItems(s.RepoNames)
	}
	s.SaveStatus = "Saved!"
	s.saveGen++
	gen := s.saveGen
	return tea.Batch(
		func() tea.Msg { return SettingsSavedMsg{} },
		tea.Tick(2*time.Second, func(time.Time) tea.Msg { return SettingsSaveClearMsg{Gen: gen} }),
	)
}

// footerCount returns the number of footer lines below the item list.
func (s *SettingsModel) footerCount() int {
	if s.SaveError != "" || s.SaveStatus != "" {
		return 2
	}
	return 1
}

// maxVisibleRows returns how many content rows fit in the scroll viewport.
func (s *SettingsModel) maxVisibleRows() int {
	// Modal border(2) + padding(2) + title(2) + tabs(3) + scroll indicators(2) + blank(1)
	mv := s.ViewHeight - 12 - s.footerCount()
	if mv < 5 {
		mv = 5
	}
	return mv
}

// totalContentRows returns the total scrollable content rows (items plus
// section headers and blank lines, matching View's line construction).
func (s *SettingsModel) totalContentRows() int {
	if len(s.Items) == 0 {
		if s.ActiveTab == TabWorktrees {
			return 1 // "No worktrees configured"
		}
		return 0
	}
	return s.itemContentLine(len(s.Items)-1) + 1
}

// visibleWindow returns the clamped scroll window over the content rows.
func (s *SettingsModel) visibleWindow() (scroll, end, total, maxVis int) {
	total = s.totalContentRows()
	maxVis = s.maxVisibleRows()
	if total <= maxVis {
		return 0, total, total, maxVis
	}
	scroll = s.Scroll
	end = scroll + maxVis
	if end > total {
		end = total
		scroll = end - maxVis
		if scroll < 0 {
			scroll = 0
		}
	}
	return scroll, end, total, maxVis
}

// modalMetrics computes the modal layout dimensions matching View() exactly —
// mouse hit-testing depends on this staying in lockstep with rendering.
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

	// Content: title(1) + blank(1) + tabbar(1) + separator(1) + blank(1)
	// + [↑ more] + visible rows + [↓ more] + blank(1) + footer
	scroll, end, total, maxVis := s.visibleWindow()
	body := end - scroll
	if total > maxVis {
		if scroll > 0 {
			body++ // "↑ more"
		}
		if end < total {
			body++ // "↓ more"
		}
	}
	contentLines := 5 + body + 1 + s.footerCount()
	renderedH := contentLines + 2 + 2 // padding + border
	if renderedH > h {
		renderedH = h
	}
	modalTopY := (h - renderedH) / 2
	contentTopY = modalTopY + 1 + 1 // border + padding

	return
}

// hitTestItem returns the index of the interactive item at screen position
// (mouseX, mouseY), or -1 when the position isn't on an activatable row.
func (s *SettingsModel) hitTestItem(mouseX, mouseY int) int {
	if s.Editing || len(s.Items) == 0 {
		return -1
	}
	modalWidth, modalLeft, _, contentTopY := s.modalMetrics()
	if mouseX < modalLeft || mouseX >= modalLeft+modalWidth+2 {
		return -1
	}
	scroll, end, total, maxVis := s.visibleWindow()
	itemsStart := contentTopY + 5
	if total > maxVis && scroll > 0 {
		itemsStart++ // "↑ more" line
	}
	if mouseY < itemsStart {
		return -1
	}
	row := mouseY - itemsStart + scroll
	if row >= end {
		return -1
	}
	for i := range s.Items {
		if s.itemContentLine(i) == row {
			if s.Items[i].Kind == SettingDisplay {
				return -1
			}
			return i
		}
	}
	return -1
}

// tabCell returns the plain text of tab i exactly as rendered (styles add no
// width) — the single source of truth for tab widths in render and hit-test.
// Kept tight so four tabs fit on one line even at the minimum modal width.
func (s *SettingsModel) tabCell(i int) string {
	if i == s.ActiveTab {
		return "[" + s.TabNames[i] + "]"
	}
	return " " + s.TabNames[i] + " "
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
	for i := range s.TabNames {
		tabW := lipgloss.Width(s.tabCell(i))
		if mouseX >= curX && mouseX < curX+tabW {
			return i
		}
		curX += tabW + 1 // +1 for the "│" separator
	}
	return -1
}

func (s SettingsModel) View(width, height int) string {
	if !s.Active {
		return ""
	}
	// Keep the dimensions the layout helpers use in sync with what we render
	s.ViewWidth, s.ViewHeight = width, height

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
	sepStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)

	var tabParts []string
	for i := range s.TabNames {
		cell := s.tabCell(i)
		switch {
		case i == s.ActiveTab:
			tabParts = append(tabParts, activeTabStyle.Render(cell))
		case i == s.HoveredTab:
			tabParts = append(tabParts, inactiveTabStyle.Underline(true).Render(cell))
		default:
			tabParts = append(tabParts, inactiveTabStyle.Render(cell))
		}
	}
	tabBar := strings.Join(tabParts, sepStyle.Render("│"))
	lines = append(lines, tabBar)
	lines = append(lines, dimStyle.Render(strings.Repeat("─", 30)))
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
	if s.ActiveTab == TabWorktrees && len(s.Items) == 0 {
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

	// Apply scroll — the window math is shared with mouse hit-testing
	// (visibleWindow/modalMetrics) and must stay in lockstep with it.
	scroll, end, _, maxVisible := s.visibleWindow()

	// Content lines: everything after title + blank + tabs + separator + blank
	titleLines := lines[:5] // "Settings", blank, tab bar, separator, blank
	contentLines := lines[5:]

	if len(contentLines) > maxVisible {
		if end > len(contentLines) {
			end = len(contentLines)
		}
		visibleContent := contentLines[scroll:end]

		var scrolledLines []string
		scrolledLines = append(scrolledLines, titleLines...)
		if scroll > 0 {
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

	modalWidth := 78
	if width-4 < modalWidth {
		modalWidth = width - 4
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	// Truncate every line to the inner width so nothing ever wraps — the
	// layout math in modalMetrics/hitTest* assumes one row per line.
	innerWidth := modalWidth - 4 // Padding(1, 2)
	truncStyle := lipgloss.NewStyle().MaxWidth(innerWidth)
	for i := range lines {
		if lipgloss.Width(lines[i]) > innerWidth {
			lines[i] = truncStyle.Render(lines[i])
		}
	}
	content := strings.Join(lines, "\n")

	modal := ModalStyle.Width(modalWidth).Render(content)

	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}
