package app

import (
	"fmt"
	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/ui"
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

	// Pre-computed layout (computed in Update, used in View)
	gridResult ui.GridResult
	headerView string
	headerH    int
	footerY    int
	createBtnY int

	// Modal
	modal    ui.ModalModel
	settings ui.SettingsModel

	// Rename input
	renameActive  bool
	renameInput   textinput.Model
	renameRepoIdx int
	renameWTIdx   int

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
}

func NewModel(cfg config.Config) Model {
	ti := textinput.New()
	ti.Placeholder = "new-branch-name"
	ti.CharLimit = 100
	ti.Width = 40

	return Model{
		config:      cfg,
		focusedCard: -1,
		focusedWT:   -1,
		hoveredBtn:  ui.BtnNone,
		renameInput: ti,
		initialLoad: true,
	}
}

// recomputeLayout recalculates grid, hit zones, and section Y positions.
// Must be called from Update (not View) so the state persists.
func (m *Model) recomputeLayout() {
	if m.width == 0 {
		return
	}

	yPos := 0

	// Header
	m.headerView = ui.RenderHeader(m.width, m.clickUsage, m.config.AICliLabel())
	m.headerH = lipgloss.Height(m.headerView)
	yPos += m.headerH

	// Loading indicator
	if m.loading {
		loadingStyle := lipgloss.NewStyle().
			Foreground(ui.ColorYellow).
			Margin(0, ui.MarginH)
		loadingLine := loadingStyle.Render("⏳ " + m.statusMsg)
		yPos += lipgloss.Height(loadingLine)
	}

	// Grid
	m.gridResult = ui.LayoutGrid(m.repos, m.width, yPos, m.focusedCard, m.focusedWT, m.hoveredBtn)
	yPos += lipgloss.Height(m.gridResult.View)

	// Status legend
	legend := ui.RenderStatusLegend(m.width)
	yPos += lipgloss.Height(legend)

	// Create button
	m.createBtnY = yPos
	createBtn := ui.RenderCreateButton(m.width, false)
	yPos += lipgloss.Height(createBtn)

	// Status bar
	if !m.loading && m.statusMsg != "" {
		statusBar := ui.RenderStatusBar(m.statusMsg, m.width)
		yPos += lipgloss.Height(statusBar)
	}

	// Rename input
	if m.renameActive {
		yPos += 1
	}

	// Footer
	m.footerY = yPos
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadReposCmd(&m.config),
		tea.SetWindowTitle("LTS - Led's Tree Script"),
		loaderTickCmd(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recomputeLayout()
		return m, nil

	case tea.KeyMsg:
		// Forward to settings if active
		if m.settings.Active {
			var cmd tea.Cmd
			m.settings, cmd = m.settings.Update(msg)
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
		if m.initialLoad {
			m.loaderFrame++
			return m, loaderTickCmd()
		}
		return m, nil

	case tea.MouseMsg:
		result, cmd := m.handleMouse(msg)
		resultModel := result.(Model)
		resultModel.recomputeLayout()
		return resultModel, cmd

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

	case StatusClearMsg:
		m.statusMsg = ""
		m.recomputeLayout()
		return m, nil

	case ui.ModalCreateMsg:
		if len(msg.RepoNames) > 0 {
			m.loading = true
			m.statusMsg = "Creating worktree..."
			return m, createWorktreeCmd(msg.RepoNames, msg.Branch, &m.config)
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
	if m.modal.Active || m.settings.Active || m.renameActive || m.deleteConfirmActive || m.openPromptActive {
		return m, nil
	}
	// Context menu: close on click, block all other mouse events
	if m.contextMenu.Active {
		if msg.Action == tea.MouseActionPress {
			m.contextMenu.Active = false
		}
		return m, nil
	}

	x, y := msg.X, msg.Y

	switch msg.Action {
	case tea.MouseActionMotion:
		m.hoveredBtn = ui.BtnNone

		// Check footer buttons
		if y >= m.footerY && m.footerY > 0 {
			m.focusedCard = -1
			m.focusedWT = -1
			m.hoveredBtn = ui.GetFooterButtonAtX(x, m.width)
			return m, nil
		}

		// Check create button
		if y >= m.createBtnY && y < m.createBtnY+3 && m.createBtnY > 0 {
			m.focusedCard = -1
			m.focusedWT = -1
			m.hoveredBtn = ui.BtnCreateWT
			return m, nil
		}

		// Hit test grid
		repoIdx, wtIdx, btn := ui.HitTest(m.gridResult.HitZones, x, y)
		m.focusedCard = repoIdx
		m.focusedWT = wtIdx
		if btn != ui.BtnNone {
			m.hoveredBtn = btn
		}

		// Detect inline buttons when hovering repo header or worktree
		if repoIdx >= 0 && (wtIdx == -2 || wtIdx >= 0) && m.gridResult.CardWidth > 0 {
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

		// Hit-test at click position
		repoIdx, wtIdx, btn := ui.HitTest(m.gridResult.HitZones, x, y)
		m.focusedCard = repoIdx
		m.focusedWT = wtIdx
		if btn != ui.BtnNone {
			m.hoveredBtn = btn
		}

		// Detect inline buttons on click
		if repoIdx >= 0 && (wtIdx == -2 || wtIdx >= 0) && m.gridResult.CardWidth > 0 {
			cardX := m.getCardScreenX(repoIdx)
			inlineBtn := ui.DetectInlineButton(x, cardX, m.gridResult.CardWidth, wtIdx)
			if inlineBtn != ui.BtnNone {
				m.hoveredBtn = inlineBtn
			}
		}

		// Check footer/create at click position
		if y >= m.footerY && m.footerY > 0 {
			m.hoveredBtn = ui.GetFooterButtonAtX(x, m.width)
		}
		if y >= m.createBtnY && y < m.createBtnY+3 && m.createBtnY > 0 {
			m.hoveredBtn = ui.BtnCreateWT
		}

		if m.loading {
			return m, nil
		}

		// Footer buttons
		if m.hoveredBtn == ui.BtnRefreshAll {
			m.loading = true
			m.statusMsg = "Refreshing all repos..."
			return m, refreshAllCmd(&m.config)
		}
		if m.hoveredBtn == ui.BtnCleanupMerged {
			m.loading = true
			m.statusMsg = "Cleaning up merged..."
			return m, cleanupCmd(&m.config)
		}
		if m.hoveredBtn == ui.BtnSettings {
			var repoNames []string
			for _, r := range m.repos {
				if !r.IsMonorepo {
					repoNames = append(repoNames, r.Name)
				}
			}
			m.settings = ui.NewSettings(&m.config, repoNames)
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

		// Context menu trigger [▸] — open context menu
		if m.hoveredBtn == ui.BtnContextMenu && m.focusedCard >= 0 {
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

	// Show tree growth animation during initial load
	if m.initialLoad {
		header := ui.RenderHeader(m.width, m.clickUsage, m.config.AICliLabel())
		loader := ui.RenderLoader(m.width, m.loaderFrame, "Discovering repositories")
		content := header + "\n" + loader
		return paintBlack(content, m.width, m.height)
	}

	var sections []string

	// Header (pre-computed)
	sections = append(sections, m.headerView)

	// Loading indicator
	if m.loading {
		loadingStyle := lipgloss.NewStyle().
			Foreground(ui.ColorYellow).
			Background(ui.ColorBlack).
			Margin(0, ui.MarginH)
		sections = append(sections, loadingStyle.Render("⏳ "+m.statusMsg))
	}

	// Grid (pre-computed)
	sections = append(sections, m.gridResult.View)

	// Status color legend
	sections = append(sections, ui.RenderStatusLegend(m.width))

	// Create button
	sections = append(sections, ui.RenderCreateButton(m.width, m.hoveredBtn == ui.BtnCreateWT))

	// Status bar
	if !m.loading && m.statusMsg != "" {
		sections = append(sections, ui.RenderStatusBar(m.statusMsg, m.width))
	}

	// Footer
	sections = append(sections, ui.RenderFooter(m.width, m.hoveredBtn))

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

	title := "Rename Branch"
	context := ""
	if m.renameWTIdx == -2 && m.renameRepoIdx >= 0 && m.renameRepoIdx < len(m.repos) {
		title = "Change Basis Branch"
		context = "Repository: " + m.repos[m.renameRepoIdx].Name
	} else if m.renameRepoIdx >= 0 && m.renameRepoIdx < len(m.repos) && m.renameWTIdx >= 0 && m.renameWTIdx < len(m.repos[m.renameRepoIdx].Worktrees) {
		wt := m.repos[m.renameRepoIdx].Worktrees[m.renameWTIdx]
		context = "Current: " + wt.Branch
	}

	content := titleStyle.Render(title) + "\n\n"
	if context != "" {
		content += dimStyle.Render(context) + "\n\n"
	}
	content += m.renameInput.View() + "\n\n"
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

func (m Model) renderDeleteConfirmDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorRed).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)

	branchName := "unknown"
	if m.deleteRepoIdx >= 0 && m.deleteRepoIdx < len(m.repos) {
		repo := m.repos[m.deleteRepoIdx]
		if m.deleteWTIdx >= 0 && m.deleteWTIdx < len(repo.Worktrees) {
			branchName = repo.Worktrees[m.deleteWTIdx].Branch
		}
	}

	content := titleStyle.Render("Delete Worktree") + "\n\n"
	content += dimStyle.Render("This will remove the worktree and delete the local branch:") + "\n\n"
	content += whiteStyle.Render("  "+branchName) + "\n\n"
	content += whiteStyle.Render("[Y]") + dimStyle.Render("es  ") + whiteStyle.Render("[N]") + dimStyle.Render("o")

	modal := ui.ModalStyle.Width(50).Render(content)
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

func refreshAllCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		count, failed, err := git.RefreshAllRepos(cfg.WorkDir, basisResolver(cfg))
		return RefreshDoneMsg{Count: count, Failed: failed, Err: err}
	}
}

func singleRefreshCmd(repoPath, basisBranch string, repoIdx int) tea.Cmd {
	return func() tea.Msg {
		err := git.RefreshRepo(repoPath, basisBranch)
		return SingleRefreshDoneMsg{RepoIdx: repoIdx, Err: err}
	}
}

func rebaseCmd(wtPath, mainBranch string, repoIdx, wtIdx int) tea.Cmd {
	return func() tea.Msg {
		err := git.RebaseWorktree(wtPath, mainBranch)
		return RebaseDoneMsg{RepoIdx: repoIdx, WTIdx: wtIdx, Err: err}
	}
}

func deleteCmd(repoPath, wtPath, branch string, repoIdx, wtIdx int) tea.Cmd {
	return func() tea.Msg {
		err := git.DeleteWorktree(repoPath, wtPath, branch, false)
		return DeleteDoneMsg{RepoIdx: repoIdx, WTIdx: wtIdx, Err: err}
	}
}

func createWorktreeCmd(repoNames []string, branch string, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		log := &git.CreateLog{}
		basis := "main"
		if len(repoNames) == 1 {
			basis = cfg.GetRepoBasisBranch(repoNames[0])
		}
		results, err := git.CreateMonorepoWorktrees(repoNames, cfg.WorkDir, branch, basis,
			cfg.Global.PackageManager, cfg.Global.AICliCommand, log)
		return CreateDoneMsg{Results: results, Branch: branch, Log: log, Err: err}
	}
}

func cleanupCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		cleaned, err := git.CleanupMergedCleanables(cfg.WorkDir, basisResolver(cfg))
		return CleanupMergedDoneMsg{Cleaned: cleaned, Err: err}
	}
}

func renameCmd(wtPath, newBranch string, repoIdx, wtIdx int) tea.Cmd {
	return func() tea.Msg {
		err := git.RenameWorktreeBranch(wtPath, newBranch)
		return RenameDoneMsg{RepoIdx: repoIdx, WTIdx: wtIdx, Err: err}
	}
}

func clearStatusCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return StatusClearMsg{}
	})
}

func loaderTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return LoaderTickMsg{}
	})
}
