package app

import (
	"fmt"
	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/ui"
	"lts-revamp/internal/update"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	config      config.Config
	repos       []git.Repo
	width       int
	height      int
	clickUsage  opener.ClickUsage
	focusedCard int
	focusedWT   int // -1=card border, -2=header, 0+=worktree index
	hoveredBtn  ui.HoverButton
	loading     bool
	statusMsg   string
	statusGen   int // incremented on each statusMsg change; used to avoid stale clears

	// Pre-computed layout (computed in Update, used in View)
	gridResult    ui.GridResult
	headerView    string
	headerH       int
	footerY       int
	createBtnY    int
	scrollY       int // vertical scroll offset for main content area (lines)
	contentHeight int // total height of scrollable content (grid + legend + create btn)

	// Modal
	modal    ui.ModalModel
	settings ui.SettingsModel

	// Rename input
	renameActive       bool
	renameInput        textinput.Model
	renameRepoIdx      int
	renameWTIdx        int
	renameRemoteBranch bool // true = also rename remote branch (push new, delete old)

	// Loading animation
	initialLoad bool // true until first ReposLoadedMsg
	loaderFrame int

	// Context menu
	contextMenu ui.ContextMenuModel

	// Post-creation prompt
	openPromptActive  bool
	openPromptResults []*git.CreateResult

	// Delete confirmation
	deleteConfirmActive bool
	deleteRepoIdx       int
	deleteWTIdx         int
	deleteTypedInput    textinput.Model // for "type DELETE" confirmation
	deleteDangerous     bool            // true = requires typing DELETE
	deleteRemoteBranch  bool            // true = also delete remote branch (for merged)

	// Cleanup confirmation
	cleanupConfirmActive bool
	cleanupRemoteBranch  bool // true = also delete remote branches during cleanup

	// Log panel
	logPanel ui.LogPanelModel
	logChan  <-chan LogEntryMsg // active log channel (nil when no operation streaming)
}

func NewModel(cfg config.Config) Model {
	ti := textinput.New()
	ti.Placeholder = "new-branch-name"
	ti.CharLimit = 100
	ti.Width = 40

	di := textinput.New()
	di.Placeholder = "DELETE"
	di.CharLimit = 6
	di.Width = 10

	return Model{
		config:           cfg,
		focusedCard:      -1,
		focusedWT:        -1,
		hoveredBtn:       ui.BtnNone,
		renameInput:      ti,
		deleteTypedInput: di,
		initialLoad:      true,
		logPanel:         ui.NewLogPanel(),
	}
}

// recomputeLayout recalculates grid, hit zones, and section Y positions.
// Must be called from Update (not View) so the state persists.
func (m *Model) recomputeLayout() {
	if m.width == 0 {
		return
	}

	yPos := 0

	// Header (includes status line) — fixed, not scrollable
	m.headerView = ui.RenderHeader(m.width, m.clickUsage, m.config.AICliLabel(), ui.HeaderOpts{
		Loading:   m.loading,
		Frame:     m.loaderFrame,
		StatusMsg: m.statusMsg,
	})
	m.headerH = lipgloss.Height(m.headerView)
	yPos += m.headerH

	// Grid — pass virtual yPos (header + scroll offset applied later)
	// Hit zones use absolute virtual coordinates; mouse handler adds scrollY
	m.gridResult = ui.LayoutGrid(m.repos, m.width, yPos, m.focusedCard, m.focusedWT, m.hoveredBtn)
	gridH := lipgloss.Height(m.gridResult.View)
	yPos += gridH

	// Status legend
	legend := ui.RenderStatusLegend(m.width)
	yPos += lipgloss.Height(legend)

	// Create button
	m.createBtnY = yPos
	createBtn := ui.RenderCreateButton(m.width, false)
	yPos += lipgloss.Height(createBtn)

	// Total scrollable content height (everything between header and footer)
	m.contentHeight = yPos - m.headerH

	// Footer — fixed at bottom
	// footerY is where the footer renders on screen (after visible content)
	m.footerY = yPos

	// Clamp scroll
	m.clampScroll()
}

// clampScroll ensures scrollY stays within valid bounds.
func (m *Model) clampScroll() {
	// Available viewport height = terminal height - header - footer(~1 line) - 1
	viewportH := m.height - m.headerH - 2
	if viewportH < 1 {
		viewportH = 1
	}
	maxScroll := m.contentHeight - viewportH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollY > maxScroll {
		m.scrollY = maxScroll
	}
	if m.scrollY < 0 {
		m.scrollY = 0
	}
}

// syncSettingsConfig copies config changes from the settings model's config pointer
// back into the model's config. Required because Model uses value receivers, so
// the settings pointer becomes detached from m.config after each Update copy.
func (m *Model) syncSettingsConfig() {
	if m.settings.Config != nil {
		m.config.Global = m.settings.Config.Global
		m.config.Local = m.settings.Config.Local
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		loadReposCmd(&m.config),
		tea.SetWindowTitle("LTS - Led's Tree Script"),
		loaderTickCmd(),
	}
	if m.config.Global.CheckForUpdates && update.ShouldCheck(m.config.Global.LastUpdateCheck) {
		cmds = append(cmds, updateCheckCmd(m.config.Global.AutoUpdate))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.settings.ViewHeight = msg.Height
		m.recomputeLayout()
		return m, nil

	case tea.KeyMsg:
		// Forward to settings if active
		if m.settings.Active {
			var cmd tea.Cmd
			m.settings, cmd = m.settings.Update(msg)
			// Sync config changes from settings back to model
			m.syncSettingsConfig()
			if !m.settings.Active {
				// Settings closed — reload repos in case basis branch changed
				m.recomputeLayout()
				return m, loadReposCmd(&m.config)
			}
			return m, cmd
		}
		updated, cmd := handleKeyPress(m, msg)
		updated.recomputeLayout()
		return updated, cmd

	case LoaderTickMsg:
		if m.initialLoad || m.loading {
			m.loaderFrame++
			m.recomputeLayout()
			return m, loaderTickCmd()
		}
		return m, nil

	case tea.MouseMsg:
		result, cmd := m.handleMouse(msg)
		resultModel := result.(Model)
		resultModel.recomputeLayout()
		return resultModel, cmd

	case ui.SettingsSavedMsg:
		// Setting changed — reload repos to reflect new config immediately
		m.recomputeLayout()
		return m, loadReposCmd(&m.config)

	case ui.SettingsSaveClearMsg:
		// Forward clear to settings model
		if m.settings.Active {
			var cmd tea.Cmd
			m.settings, cmd = m.settings.Update(msg)
			return m, cmd
		}
		return m, nil

	case ReposLoadedMsg:
		m.repos = msg.Repos
		m.loading = false
		m.initialLoad = false
		if msg.Err != nil {
			m.statusMsg = "Error loading repos: " + msg.Err.Error()
		}
		// Initialize local config for all discovered repos
		var repoNames []string
		for _, r := range m.repos {
			if !r.IsMonorepo {
				repoNames = append(repoNames, r.Name)
			}
		}
		m.config.InitLocalForRepos(repoNames)
		m.recomputeLayout()
		return m, nil

	case RefreshDoneMsg:
		m.loading = false
		if msg.Err != nil {
			m.statusMsg = "Refresh error: " + msg.Err.Error()
		} else {
			if len(msg.Failed) > 0 {
				m.statusMsg = fmt.Sprintf("Refreshed %d repos (failed: %s)", msg.Count, strings.Join(msg.Failed, ", "))
			} else {
				m.statusMsg = fmt.Sprintf("Refreshed %d repos", msg.Count)
			}
			// Update last refresh for successful repos only
			for _, r := range m.repos {
				if r.IsMonorepo {
					continue
				}
				isFailed := false
				for _, f := range msg.Failed {
					if f == r.Name {
						isFailed = true
						break
					}
				}
				if !isFailed {
					m.config.SetRepoLastRefresh(r.Name, time.Now().Unix())
				}
			}
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case SingleRefreshDoneMsg:
		m.loading = false
		if msg.Err != nil {
			m.statusMsg = "Refresh error: " + msg.Err.Error()
		} else {
			m.statusMsg = "Repo refreshed"
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case RebaseDoneMsg:
		m.loading = false
		if msg.Err != nil {
			m.statusMsg = "Rebase error: " + msg.Err.Error()
		} else {
			m.statusMsg = "Rebase successful"
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case DeleteDoneMsg:
		m.loading = false
		m.focusedWT = -1
		if msg.Err != nil {
			m.statusMsg = "Delete error: " + msg.Err.Error()
		} else {
			m.statusMsg = "Worktree deleted"
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case CreateDoneMsg:
		m.loading = false
		if msg.Err != nil {
			m.statusMsg = "Create error: " + msg.Err.Error()
			m.recomputeLayout()
			return m, tea.Batch(
				loadReposCmd(&m.config),
				clearStatusCmd(),
			)
		}
		names := make([]string, len(msg.Results))
		for i, r := range msg.Results {
			names[i] = r.RepoName
		}
		m.statusMsg = fmt.Sprintf("Created %s on %s", strings.Join(names, ", "), msg.Branch)
		m.openPromptActive = true
		m.openPromptResults = msg.Results
		m.recomputeLayout()
		return m, loadReposCmd(&m.config)

	case CleanupMergedDoneMsg:
		m.loading = false
		if msg.Err != nil {
			m.statusMsg = "Cleanup error: " + msg.Err.Error()
		} else if msg.Cleaned == 0 {
			m.statusMsg = "No merged cleanables found"
		} else {
			m.statusMsg = fmt.Sprintf("Cleaned %d merged worktrees", msg.Cleaned)
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case RenameDoneMsg:
		m.loading = false
		if msg.Err != nil {
			m.statusMsg = "Rename error: " + msg.Err.Error()
		} else {
			m.statusMsg = "Branch renamed"
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case MigrateDoneMsg:
		m.loading = false
		if msg.Err != nil {
			m.statusMsg = "Migration error: " + msg.Err.Error()
			m.recomputeLayout()
			return m, tea.Batch(
				loadReposCmd(&m.config),
				clearStatusCmd(),
			)
		}
		m.statusMsg = fmt.Sprintf("Migrated %s to LTS worktree", msg.Result.Branch)
		m.openPromptActive = true
		m.openPromptResults = []*git.CreateResult{msg.Result}
		m.recomputeLayout()
		return m, loadReposCmd(&m.config)

	case LogEntryMsg:
		m.logPanel.Add(msg.Context, msg.Message, msg.IsError)
		m.recomputeLayout()
		// Re-subscribe to get the next log entry
		if m.logChan != nil {
			return m, listenForLogs(m.logChan)
		}
		return m, nil

	case StatusClearMsg:
		// Gen 0 = legacy (always clear). Gen > 0 = only clear if matching current gen.
		if !m.loading && (msg.Gen == 0 || msg.Gen == m.statusGen) {
			m.statusMsg = ""
			m.recomputeLayout()
		}
		return m, nil

	case UpdateCheckMsg:
		m.config.SetLastUpdateCheck(time.Now().Unix())
		r := msg.Result
		if r.Err != nil {
			// Silently ignore update check errors — don't disrupt the user
			return m, nil
		}
		if r.Updated {
			m.statusMsg = fmt.Sprintf("Updated to v%s — restart to apply", r.LatestVersion)
			m.statusGen++
			m.recomputeLayout()
			return m, clearStatusAfter(m.statusGen, 10*time.Second)
		}
		if r.UpdateAvail {
			m.statusMsg = fmt.Sprintf("New version available: v%s (current: v%s)", r.LatestVersion, r.CurrentVersion)
			m.statusGen++
			m.recomputeLayout()
			return m, clearStatusAfter(m.statusGen, 10*time.Second)
		}
		return m, nil

	case ui.ModalCreateMsg:
		if len(msg.RepoNames) > 0 {
			logFn, startCmd := m.beginLoading("Creating worktree...")
			return m, tea.Batch(startCmd, createWorktreeCmd(logFn, msg.RepoNames, msg.Branch, &m.config))
		}
		return m, nil

	case ui.ModalCancelMsg:
		return m, nil
	}

	// Forward to modal if active
	if m.modal.Active {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.settings.Active {
		var cmd tea.Cmd
		m.settings, cmd = m.settings.Update(msg)
		m.syncSettingsConfig()
		return m, cmd
	}
	if m.modal.Active || m.renameActive || m.deleteConfirmActive || m.openPromptActive || m.cleanupConfirmActive {
		return m, nil
	}
	// Context menu: close on click, block all other mouse events
	if m.contextMenu.Active {
		if msg.Action == tea.MouseActionPress {
			m.contextMenu.Active = false
		}
		return m, nil
	}

	// Mouse wheel: scroll grid area or log panel
	if msg.Action == tea.MouseActionPress {
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			// Determine which area the cursor is in
			screenFooterY := m.headerH + m.contentHeight - m.scrollY
			inLogArea := m.logPanel.Visible && msg.Y > screenFooterY+1

			if inLogArea {
				if msg.Button == tea.MouseButtonWheelUp {
					m.logPanel.ScrollUp(1)
				} else {
					m.logPanel.ScrollDown(1)
				}
			} else {
				// Scroll main content
				if msg.Button == tea.MouseButtonWheelUp {
					m.scrollY -= 2
				} else {
					m.scrollY += 2
				}
				m.clampScroll()
				m.recomputeLayout()
			}
			return m, nil
		}
	}

	x, y := msg.X, msg.Y
	// Translate screen Y to virtual Y for hit testing (account for scroll)
	virtualY := y + m.scrollY

	switch msg.Action {
	case tea.MouseActionMotion:
		m.hoveredBtn = ui.BtnNone

		// Check footer buttons (footer is at fixed screen position)
		screenFooterY := m.headerH + m.contentHeight - m.scrollY
		if y >= screenFooterY && screenFooterY > 0 {
			m.focusedCard = -1
			m.focusedWT = -1
			m.hoveredBtn = ui.GetFooterButtonAtX(x, m.width)
			return m, nil
		}

		// Hit test grid (using virtual Y)
		repoIdx, wtIdx, btn := ui.HitTest(m.gridResult.HitZones, x, virtualY)
		m.focusedCard = repoIdx
		m.focusedWT = wtIdx
		if btn != ui.BtnNone {
			m.hoveredBtn = btn
		}

		// Detect inline buttons when hovering repo header or worktree (suppress during loading)
		// Skip for migration cards — they don't have inline context buttons
		isMigrationCard := repoIdx >= 0 && repoIdx < len(m.repos) && m.repos[repoIdx].NeedsMigration
		if !isMigrationCard && !m.loading && repoIdx >= 0 && (wtIdx == -2 || wtIdx >= 0) && m.gridResult.CardWidth > 0 {
			cardX := m.getCardScreenX(repoIdx)
			inlineBtn := ui.DetectInlineButton(x, cardX, m.gridResult.CardWidth, wtIdx)
			if inlineBtn != ui.BtnNone {
				m.hoveredBtn = inlineBtn
			}
		}

		return m, nil

	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}

		// Hit-test at click position (using virtual Y)
		repoIdx, wtIdx, btn := ui.HitTest(m.gridResult.HitZones, x, virtualY)
		m.focusedCard = repoIdx
		m.focusedWT = wtIdx
		if btn != ui.BtnNone {
			m.hoveredBtn = btn
		}

		// Detect inline buttons on click (suppress during loading)
		// Skip for migration cards — they don't have inline context buttons
		isMigrationCard := repoIdx >= 0 && repoIdx < len(m.repos) && m.repos[repoIdx].NeedsMigration
		if !isMigrationCard && !m.loading && repoIdx >= 0 && (wtIdx == -2 || wtIdx >= 0) && m.gridResult.CardWidth > 0 {
			cardX := m.getCardScreenX(repoIdx)
			inlineBtn := ui.DetectInlineButton(x, cardX, m.gridResult.CardWidth, wtIdx)
			if inlineBtn != ui.BtnNone {
				m.hoveredBtn = inlineBtn
			}
		}

		// Check footer at click position (fixed screen position)
		screenFooterY := m.headerH + m.contentHeight - m.scrollY
		if y >= screenFooterY && screenFooterY > 0 {
			m.hoveredBtn = ui.GetFooterButtonAtX(x, m.width)
		}

		// Check create button (virtual Y)
		if virtualY >= m.createBtnY && virtualY < m.createBtnY+3 && m.createBtnY > 0 {
			m.hoveredBtn = ui.BtnCreateWT
		}

		if m.loading {
			return m, nil
		}

		// Footer buttons
		if m.hoveredBtn == ui.BtnRefreshAll {
			logFn, startCmd := m.beginLoading("Refreshing all repos...")
			return m, tea.Batch(startCmd, refreshAllCmd(logFn, &m.config))
		}
		if m.hoveredBtn == ui.BtnCleanupMerged {
			m.cleanupConfirmActive = true
			m.cleanupRemoteBranch = false
			m.statusMsg = "Cleanup merged worktrees? [Y]es / [N]o"
			return m, nil
		}
		if m.hoveredBtn == ui.BtnSettings {
			var repoNames []string
			for _, r := range m.repos {
				if !r.IsMonorepo {
					repoNames = append(repoNames, r.Name)
				}
			}
			m.settings = ui.NewSettings(&m.config, repoNames)
			m.settings.ViewHeight = m.height
			return m, nil
		}
		if m.hoveredBtn == ui.BtnExit {
			return m, tea.Quit
		}

		// Create button
		if m.hoveredBtn == ui.BtnCreateWT {
			m.modal = ui.NewModal(m.repos)
			return m, textinput.Blink
		}

		// Migrate button
		if m.hoveredBtn == ui.BtnMigrate && m.focusedCard >= 0 && m.focusedCard < len(m.repos) {
			repo := m.repos[m.focusedCard]
			if repo.NeedsMigration && repo.Path != "" {
				logFn, startCmd := m.beginLoading("Migrating "+repo.Name+"...")
				return m, tea.Batch(startCmd, migrateCmd(logFn, repo.Path, &m.config))
			}
		}

		// Context menu trigger [▸] — open context menu (not for migration cards)
		if m.hoveredBtn == ui.BtnContextMenu && m.focusedCard >= 0 && !isMigrationCard {
			repo := m.repos[m.focusedCard]
			if m.focusedWT == -2 {
				// Repo header context menu
				m.contextMenu = ui.ContextMenuModel{
					Active:  true,
					Items:   ui.RepoContextItems(repo.IsMonorepo),
					RepoIdx: m.focusedCard,
					WTIdx:   -2,
					X:       x, Y: y,
				}
			} else if m.focusedWT >= 0 && m.focusedWT < len(repo.Worktrees) {
				// Worktree context menu
				m.contextMenu = ui.ContextMenuModel{
					Active:  true,
					Items:   ui.WorktreeContextItems(),
					RepoIdx: m.focusedCard,
					WTIdx:   m.focusedWT,
					X:       x, Y: y,
				}
			}
			return m, nil
		}

		// Click on worktree (not on button) — open it
		if m.focusedCard >= 0 && m.focusedWT >= 0 && m.hoveredBtn == ui.BtnNone {
			repo := m.repos[m.focusedCard]
			if m.focusedWT < len(repo.Worktrees) {
				wt := repo.Worktrees[m.focusedWT]
				err := opener.OpenWorktree(wt.Path, m.clickUsage, m.config.Global.IDECommand, m.config.Global.AICliCommand, m.config.Global.Terminal)
				if err != nil {
					m.statusMsg = fmt.Sprintf("Failed to open: %s", err.Error())
				} else {
					m.statusMsg = fmt.Sprintf("Opened %s in %s", wt.Branch, m.clickUsage)
				}
				return m, clearStatusCmd()
			}
		}
	}

	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Minimum terminal size check
	minWidth, minHeight := 60, 20
	if m.width < minWidth || m.height < minHeight {
		msg := ui.RenderResizePrompt(m.width, m.height, minWidth, minHeight)
		return paintBlack(msg, m.width, m.height)
	}

	// Show tree growth animation during initial load
	if m.initialLoad {
		header := ui.RenderHeader(m.width, m.clickUsage, m.config.AICliLabel())
		loader := ui.RenderLoader(m.width, m.loaderFrame, "Discovering repositories")
		content := header + "\n" + loader
		return paintBlack(content, m.width, m.height)
	}

	var sections []string

	// Header (pre-computed, includes status line) — fixed at top
	sections = append(sections, m.headerView)

	// --- Scrollable content area ---
	var scrollable []string
	scrollable = append(scrollable, m.gridResult.View)
	scrollable = append(scrollable, ui.RenderStatusLegend(m.width))
	scrollable = append(scrollable, ui.RenderCreateButton(m.width, m.hoveredBtn == ui.BtnCreateWT))
	scrollContent := strings.Join(scrollable, "\n")

	// Apply vertical scroll: slice visible lines from the scrollable content
	scrollLines := strings.Split(scrollContent, "\n")
	viewportH := m.height - m.headerH - 2 // reserve: footer(1) + buffer(1)
	if viewportH < 1 {
		viewportH = 1
	}

	// Check if scroll indicator will be needed (reserve 1 line for it)
	needsIndicator := len(scrollLines) > viewportH
	if needsIndicator && viewportH > 2 {
		viewportH-- // reserve line for scroll indicator
	}

	startLine := m.scrollY
	if startLine > len(scrollLines) {
		startLine = len(scrollLines)
	}
	endLine := startLine + viewportH
	if endLine > len(scrollLines) {
		endLine = len(scrollLines)
	}
	visibleLines := scrollLines[startLine:endLine]

	// Scroll indicator
	canScrollUp := m.scrollY > 0
	canScrollDown := endLine < len(scrollLines)
	if canScrollUp || canScrollDown {
		indicator := ui.RenderScrollIndicator(m.width, canScrollUp, canScrollDown)
		visibleLines = append(visibleLines, indicator)
	}

	sections = append(sections, strings.Join(visibleLines, "\n"))

	// Footer — fixed at bottom
	sections = append(sections, ui.RenderFooter(m.width, m.hoveredBtn))

	// Log panel (fills remaining space below footer)
	if m.logPanel.Visible && len(m.logPanel.Entries) > 0 {
		footerScreenY := m.headerH + len(visibleLines) + 1
		availHeight := m.height - footerScreenY - 2
		if availHeight > 3 {
			logView := ui.RenderLogPanel(m.logPanel, m.width, availHeight)
			if logView != "" {
				sections = append(sections, logView)
			}
		}
	}

	content := strings.Join(sections, "\n")

	// --- Overlay dialogs (rendered on top of content) ---

	// Context menu
	if m.contextMenu.Active {
		dialog := ui.RenderContextMenu(m.contextMenu, m.width, m.height)
		return paintBlack(dialog, m.width, m.height)
	}

	// Rename dialog
	if m.renameActive {
		dialog := m.renderRenameDialog()
		return paintBlack(dialog, m.width, m.height)
	}

	// Open workspace prompt
	if m.openPromptActive {
		dialog := m.renderOpenPromptDialog()
		return paintBlack(dialog, m.width, m.height)
	}

	// Cleanup confirmation
	if m.cleanupConfirmActive {
		dialog := m.renderCleanupConfirmDialog()
		return paintBlack(dialog, m.width, m.height)
	}

	// Delete confirmation
	if m.deleteConfirmActive {
		dialog := m.renderDeleteConfirmDialog()
		return paintBlack(dialog, m.width, m.height)
	}

	// Settings
	if m.settings.Active {
		dialog := m.settings.View(m.width, m.height)
		return paintBlack(dialog, m.width, m.height)
	}

	// Create worktree modal
	if m.modal.Active {
		dialog := m.modal.View(m.width, m.height)
		return paintBlack(dialog, m.width, m.height)
	}

	return paintBlack(content, m.width, m.height)
}

func (m Model) renderRenameDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorGreen).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)

	title := "Rename Branch"
	context := ""
	isBasisChange := m.renameWTIdx == -2
	if isBasisChange && m.renameRepoIdx >= 0 && m.renameRepoIdx < len(m.repos) {
		title = "Change Basis Branch"
		context = "Repository: " + m.repos[m.renameRepoIdx].Name
	} else if m.renameRepoIdx >= 0 && m.renameRepoIdx < len(m.repos) && m.renameWTIdx >= 0 && m.renameWTIdx < len(m.repos[m.renameRepoIdx].Worktrees) {
		repo := m.repos[m.renameRepoIdx]
		wt := repo.Worktrees[m.renameWTIdx]
		context = "Current: " + wt.Branch
		if repo.IsMonorepo {
			title = "Rename Branch (all " + fmt.Sprintf("%d", len(repo.RepoNames)) + " repos)"
		}
	}

	content := titleStyle.Render(title) + "\n\n"
	if context != "" {
		content += dimStyle.Render(context) + "\n\n"
	}
	content += m.renameInput.View() + "\n"

	// Show remote branch toggle for actual renames (not basis branch changes)
	if !isBasisChange && renameHasRemote(m) {
		content += "\n"
		if m.renameRemoteBranch {
			content += whiteStyle.Render("  [ctrl+d] ✓ Rename remote branch") + "\n"
		} else {
			content += dimStyle.Render("  [ctrl+d] Rename remote branch") + "\n"
		}
	}

	content += "\n"
	content += dimStyle.Render("enter confirm • esc cancel")

	modal := ui.ModalStyle.Width(50).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m Model) renderOpenPromptDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorGreen).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)

	content := titleStyle.Render("Worktree Created") + "\n\n"
	for _, r := range m.openPromptResults {
		content += whiteStyle.Render("  "+r.RepoName) + dimStyle.Render(" → "+r.Branch) + "\n"
	}
	content += "\n" + dimStyle.Render("Open workspace in IDE?") + "\n\n"
	content += whiteStyle.Render("[Y]") + dimStyle.Render("es  ") + whiteStyle.Render("[N]") + dimStyle.Render("o")

	modal := ui.ModalStyle.Width(50).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

// deleteWarning returns a warning message and whether the status is dangerous (requires typing DELETE).
func deleteWarning(status git.WTStatus) (warning string, dangerous bool) {
	switch status {
	case git.StatusChanged:
		return "Uncommitted changes will be LOST", true
	case git.StatusDiverged:
		return "CRITICAL: Unpushed commits will be permanently lost", true
	case git.StatusToPush:
		return "Unpushed commits will be LOST", true
	case git.StatusNoRemote:
		return "Branch was never pushed — ALL work will be LOST", true
	case git.StatusMergedDirty:
		return "Uncommitted changes will be LOST (branch is merged)", true
	case git.StatusNewDirty:
		return "Uncommitted changes will be LOST (new branch)", true
	case git.StatusMissing:
		return "Worktree directory is missing — will clean up git references", false
	default:
		return "", false
	}
}

// deleteHasRemote returns true if the worktree status implies a remote branch exists.
func deleteHasRemote(status git.WTStatus) bool {
	switch status {
	case git.StatusNew, git.StatusNewDirty, git.StatusNoRemote:
		return false
	default:
		return true
	}
}

func (m Model) renderDeleteConfirmDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorRed).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)
	warnStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorYellow).Background(ui.ColorBlack)
	critStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorRed).Background(ui.ColorBlack)

	branchName := "unknown"
	var wt git.Worktree
	hasWT := false
	if m.deleteRepoIdx >= 0 && m.deleteRepoIdx < len(m.repos) {
		repo := m.repos[m.deleteRepoIdx]
		if m.deleteWTIdx >= 0 && m.deleteWTIdx < len(repo.Worktrees) {
			wt = repo.Worktrees[m.deleteWTIdx]
			branchName = wt.Branch
			hasWT = true
		}
	}

	content := titleStyle.Render("Delete Worktree") + "\n\n"
	content += dimStyle.Render("This will remove the worktree and delete the local branch:") + "\n\n"
	content += whiteStyle.Render("  "+branchName) + "\n"

	if hasWT {
		warning, dangerous := deleteWarning(wt.Status)
		if warning != "" {
			content += "\n"
			if dangerous {
				content += critStyle.Render("  ⚠ "+warning) + "\n"
			} else {
				content += warnStyle.Render("  ⚠ "+warning) + "\n"
			}
		}

		// Offer remote branch deletion toggle when remote exists
		if deleteHasRemote(wt.Status) {
			toggleKey := "d"
			if m.deleteDangerous {
				toggleKey = "ctrl+d"
			}
			content += "\n"
			if m.deleteRemoteBranch {
				content += whiteStyle.Render("  ["+toggleKey+"] ✓ Also delete remote branch") + "\n"
			} else {
				content += dimStyle.Render("  ["+toggleKey+"] Also delete remote branch") + "\n"
			}
		}
	}

	content += "\n"
	if m.deleteDangerous {
		content += warnStyle.Render("  Type DELETE to confirm:") + "\n\n"
		content += "  " + m.deleteTypedInput.View() + "\n\n"
		content += dimStyle.Render("  enter confirm • esc cancel")
	} else {
		content += whiteStyle.Render("[Y]") + dimStyle.Render("es  ") + whiteStyle.Render("[N]") + dimStyle.Render("o")
	}

	modal := ui.ModalStyle.Width(56).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m Model) renderCleanupConfirmDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorGreen).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)

	content := titleStyle.Render("Cleanup Merged Worktrees") + "\n\n"
	content += dimStyle.Render("This will remove all merged worktrees and their local branches.") + "\n"

	content += "\n"
	if m.cleanupRemoteBranch {
		content += whiteStyle.Render("  [d] ✓ Also delete remote branches") + "\n"
	} else {
		content += dimStyle.Render("  [d] Also delete remote branches") + "\n"
	}

	content += "\n"
	content += whiteStyle.Render("[Y]") + dimStyle.Render("es  ") + whiteStyle.Render("[N]") + dimStyle.Render("o")

	modal := ui.ModalStyle.Width(56).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

// getCardScreenX computes the screen X position of a card by its repo index.
func (m Model) getCardScreenX(repoIdx int) int {
	cols := m.gridResult.Cols
	if cols <= 0 {
		cols = 1
	}
	col := repoIdx % cols
	return ui.MarginH + col*(m.gridResult.CardWidth+ui.CardGap)
}

// paintBlack ensures the entire screen has a black background by:
// 1. Replacing every ANSI reset with reset+black-bg so gaps between styled text stay black
// 2. Starting each line with black background
// 3. Padding each line to full terminal width
// 4. Filling remaining vertical space with black lines
func paintBlack(content string, width, height int) string {
	blackBg := "\033[48;2;0;0;0m"

	// After every ANSI reset, re-apply black background
	content = strings.ReplaceAll(content, "\033[0m", "\033[0m"+blackBg)

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		visWidth := lipgloss.Width(line)
		pad := width - visWidth
		if pad < 0 {
			pad = 0
		}
		// Prepend black bg, append padding spaces to fill width
		lines[i] = blackBg + line + strings.Repeat(" ", pad)
	}
	// Fill remaining vertical space
	emptyLine := blackBg + strings.Repeat(" ", width)
	for len(lines) < height {
		lines = append(lines, emptyLine)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// Commands

// basisResolver creates a BasisBranchResolver from the config.
func basisResolver(cfg *config.Config) git.BasisBranchResolver {
	return func(repoName string) string {
		return cfg.GetRepoBasisBranch(repoName)
	}
}

func loadReposCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		repos := git.DiscoverRepos(cfg.WorkDir, basisResolver(cfg))
		return ReposLoadedMsg{Repos: repos}
	}
}

// newLogChan creates a log channel and a LogFunc that writes to it.
func newLogChan() (git.LogFunc, chan LogEntryMsg) {
	ch := make(chan LogEntryMsg, 32)
	fn := func(ctx, msg string, isError bool) {
		ch <- LogEntryMsg{Context: ctx, Message: msg, IsError: isError}
	}
	return fn, ch
}

// listenForLogs returns a tea.Cmd that reads one LogEntryMsg from the channel.
// The Update handler re-subscribes after each message.
func listenForLogs(ch <-chan LogEntryMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// beginLoading sets loading state and starts the spinner tick + log stream.
// Returns (logFn, batchCmd) where batchCmd includes the listen + tick commands.
func (m *Model) beginLoading(statusMsg string) (git.LogFunc, tea.Cmd) {
	m.loading = true
	m.statusMsg = statusMsg
	logFn, listenCmd := m.startLogStream()
	return logFn, tea.Batch(listenCmd, loaderTickCmd())
}

// startLogStream sets up log streaming on the model and returns the initial listener command.
func (m *Model) startLogStream() (git.LogFunc, tea.Cmd) {
	logFn, ch := newLogChan()
	m.logChan = ch
	return logFn, listenForLogs(ch)
}

func refreshAllCmd(logFn git.LogFunc, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		count, failed, err := git.RefreshAllRepos(cfg.WorkDir, basisResolver(cfg), logFn)
		return RefreshDoneMsg{Count: count, Failed: failed, Err: err}
	}
}

func singleRefreshCmd(logFn git.LogFunc, repoPath, basisBranch string, repoIdx int) tea.Cmd {
	return func() tea.Msg {
		err := git.RefreshRepo(repoPath, basisBranch, logFn)
		return SingleRefreshDoneMsg{RepoIdx: repoIdx, Err: err}
	}
}

func rebaseCmd(logFn git.LogFunc, wtPath, mainBranch string, repoIdx, wtIdx int) tea.Cmd {
	return func() tea.Msg {
		err := git.RebaseWorktree(wtPath, mainBranch, logFn)
		return RebaseDoneMsg{RepoIdx: repoIdx, WTIdx: wtIdx, Err: err}
	}
}

func deleteCmd(logFn git.LogFunc, repoPath, wtPath, branch string, deleteRemote bool, repoIdx, wtIdx int) tea.Cmd {
	return func() tea.Msg {
		err := git.DeleteWorktree(repoPath, wtPath, branch, deleteRemote, logFn)
		return DeleteDoneMsg{RepoIdx: repoIdx, WTIdx: wtIdx, Err: err}
	}
}

func createWorktreeCmd(logFn git.LogFunc, repoNames []string, branch string, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		log := &git.CreateLog{Stream: logFn}
		basis := "main"
		if len(repoNames) == 1 {
			basis = cfg.GetRepoBasisBranch(repoNames[0])
		}
		results, err := git.CreateMonorepoWorktrees(repoNames, cfg.WorkDir, branch, basis,
			cfg.Global.PackageManager, cfg.Global.AICliCommand, log)
		if err != nil {
			logFn("create", "Failed: "+err.Error(), true)
		} else {
			logFn("create", "Worktree created successfully", false)
		}
		return CreateDoneMsg{Results: results, Branch: branch, Log: log, Err: err}
	}
}

func cleanupCmd(logFn git.LogFunc, cfg *config.Config, deleteRemote bool) tea.Cmd {
	return func() tea.Msg {
		cleaned, err := git.CleanupMergedCleanables(cfg.WorkDir, basisResolver(cfg), deleteRemote, logFn)
		return CleanupMergedDoneMsg{Cleaned: cleaned, Err: err}
	}
}

func renameCmd(logFn git.LogFunc, repoPath, wtPath, oldBranch, newBranch string, renameRemote bool, cfg *config.Config, repoIdx, wtIdx int) tea.Cmd {
	return func() tea.Msg {
		_, err := git.RenameWorktree(repoPath, wtPath, oldBranch, newBranch, renameRemote,
			cfg.Global.PackageManager, cfg.Global.AICliCommand, logFn)
		if err != nil {
			logFn(newBranch, "Rename failed: "+err.Error(), true)
		}
		return RenameDoneMsg{RepoIdx: repoIdx, WTIdx: wtIdx, Err: err}
	}
}

func renameMonorepoCmd(logFn git.LogFunc, branchSubdirPath string, repoNames []string, oldBranch, newBranch string, renameRemote bool, cfg *config.Config, repoIdx, wtIdx int) tea.Cmd {
	return func() tea.Msg {
		_, err := git.RenameMonorepoWorktrees(cfg.WorkDir, branchSubdirPath, repoNames, oldBranch, newBranch, renameRemote,
			cfg.Global.PackageManager, cfg.Global.AICliCommand, logFn)
		if err != nil {
			logFn(newBranch, "Rename failed: "+err.Error(), true)
		}
		return RenameDoneMsg{RepoIdx: repoIdx, WTIdx: wtIdx, Err: err}
	}
}

func migrateCmd(logFn git.LogFunc, repoPath string, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		repoName := filepath.Base(repoPath)
		basis := cfg.GetRepoBasisBranch(repoName)
		result, err := git.MigrateToWorktree(repoPath, cfg.WorkDir, basis,
			cfg.Global.PackageManager, cfg.Global.AICliCommand, logFn)
		if err != nil {
			logFn("migrate", "Failed: "+err.Error(), true)
		} else {
			logFn("migrate", "Migration complete", false)
		}
		return MigrateDoneMsg{Result: result, Err: err}
	}
}

func updateCheckCmd(autoUpdate bool) tea.Cmd {
	return func() tea.Msg {
		var r update.Result
		if autoUpdate {
			r = update.Update()
		} else {
			r = update.Check()
		}
		return UpdateCheckMsg{Result: r}
	}
}

func clearStatusCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return StatusClearMsg{} // Gen 0 = always clears
	})
}

func clearStatusAfter(gen int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return StatusClearMsg{Gen: gen}
	})
}

func loaderTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return LoaderTickMsg{}
	})
}
