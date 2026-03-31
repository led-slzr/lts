package app

import (
	"fmt"
	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/ui"
	"lts-revamp/internal/update"
	"os/exec"
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

	// Header hover states
	versionHovered bool
	hoveredUsage   opener.ClickUsage // -1 = none

	// History suggestion hover (empty state)
	hoveredHistory int // -1 = none

	// Relaunch: set to a directory path to relaunch LTS there after quit
	RelaunchDir string

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

	ui.CurrentWorkDir = cfg.WorkDir

	return Model{
		config:           cfg,
		focusedCard:      -1,
		focusedWT:        -1,
		hoveredBtn:       ui.BtnNone,
		hoveredUsage:     -1,
		hoveredHistory:   -1,
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
		Loading:        m.loading,
		Frame:          m.loaderFrame,
		StatusMsg:      m.statusMsg,
		VersionHovered: m.versionHovered,
		HoveredUsage:   m.hoveredUsage,
	})
	m.headerH = lipgloss.Height(m.headerView)
	yPos += m.headerH

	// Grid — pass virtual yPos (header + scroll offset applied later)
	// Hit zones use absolute virtual coordinates; mouse handler adds scrollY
	m.gridResult = ui.LayoutGrid(m.repos, m.width, yPos, m.focusedCard, m.focusedWT, m.hoveredBtn, m.hoveredHistory)
	gridH := lipgloss.Height(m.gridResult.View)
	yPos += gridH

	// Status legend and create button (only when repos exist)
	if len(m.repos) > 0 {
		legend := ui.RenderStatusLegend(m.width)
		yPos += lipgloss.Height(legend)

		m.createBtnY = yPos
		createBtn := ui.RenderCreateButton(m.width, false)
		yPos += lipgloss.Height(createBtn)
	} else {
		m.createBtnY = 0
	}

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
		checkMigrationCmd(&m.config),
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

	case MigrationCheckMsg:
		if msg.Needed {
			m.statusMsg = "Improving directory structure, please wait..."
			m.recomputeLayout()
			return m, doMigrationCmd(&m.config)
		}
		return m, loadReposCmd(&m.config)

	case MigrationDoneMsg:
		m.statusMsg = ""
		return m, loadReposCmd(&m.config)

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
		// Save to history if repos were found
		if len(m.repos) > 0 {
			go config.SaveHistory(m.config.WorkDir, len(m.repos))
		}
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

// modalMetrics computes the screen position of modal content.
// modalRendered is the styled modal box (before lipgloss.Place), screenH is terminal height.
// Returns: contentStartY (first content line), modalTop, modalH.
func modalMetrics(modalRendered string, screenH int) (contentStartY, modalTop, modalH int) {
	modalH = lipgloss.Height(modalRendered)
	modalTop = (screenH - modalH) / 2
	contentStartY = modalTop + 2 // border(1) + padding(1)
	return
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.settings.Active {
		var cmd tea.Cmd
		m.settings, cmd = m.settings.Update(msg)
		m.syncSettingsConfig()
		return m, cmd
	}

	// Context menu: hover to highlight, click to execute, click elsewhere to close
	if m.contextMenu.Active {
		menuRendered := ui.RenderContextMenu(m.contextMenu, m.width, m.height)
		contentStartY, _, _ := modalMetrics(menuRendered, m.height)
		itemStartY := contentStartY + 2 // skip title + empty line

		hoveredItem := msg.Y - itemStartY
		if hoveredItem >= 0 && hoveredItem < len(m.contextMenu.Items) {
			m.contextMenu.CursorIdx = hoveredItem
		}

		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if hoveredItem >= 0 && hoveredItem < len(m.contextMenu.Items) {
				item := m.contextMenu.Items[hoveredItem]
				m.contextMenu.Active = false
				return executeContextAction(m, item.Action, m.contextMenu.RepoIdx, m.contextMenu.WTIdx)
			}
			m.contextMenu.Active = false
		}
		return m, nil
	}

	// Y/N dialogs: click on [Y] or [N] buttons
	if m.openPromptActive {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			modal := m.renderOpenPromptDialog()
			_, modalTop, modalH := modalMetrics(modal, m.height)
			ynY := modalTop + modalH - 3 // last content line before padding+border
			if msg.Y == ynY {
				modalLeft := (m.width - lipgloss.Width(modal)) / 2
				relX := msg.X - modalLeft
				if relX >= 0 && relX < 20 {
					return handleOpenPromptKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
				} else {
					return handleOpenPromptKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
				}
			}
		}
		return m, nil
	}

	if m.cleanupConfirmActive {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			modal := m.renderCleanupConfirmDialog()
			_, modalTop, modalH := modalMetrics(modal, m.height)
			ynY := modalTop + modalH - 3

			if msg.Y == ynY {
				modalLeft := (m.width - lipgloss.Width(modal)) / 2
				relX := msg.X - modalLeft
				if relX >= 0 && relX < 20 {
					return handleCleanupConfirmKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
				} else {
					return handleCleanupConfirmKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
				}
			}
			// [d] toggle is 2 lines above Y/N (toggle + empty + Y/N)
			if msg.Y == ynY-2 {
				return handleCleanupConfirmKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
			}
		}
		return m, nil
	}

	if m.deleteConfirmActive {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			modal := m.renderDeleteConfirmDialog()
			_, modalTop, modalH := modalMetrics(modal, m.height)
			ynY := modalTop + modalH - 3

			if !m.deleteDangerous && msg.Y == ynY {
				modalLeft := (m.width - lipgloss.Width(modal)) / 2
				relX := msg.X - modalLeft
				if relX >= 0 && relX < 20 {
					return confirmDelete(m)
				} else {
					return cancelDelete(m)
				}
			}
			// Remote toggle: 2 lines above Y/N (simple) or 4 lines above bottom hint (dangerous)
			toggleY := ynY - 2
			if m.deleteDangerous {
				toggleY = ynY - 4
			}
			if msg.Y == toggleY {
				if m.deleteRepoIdx >= 0 && m.deleteRepoIdx < len(m.repos) {
					repo := m.repos[m.deleteRepoIdx]
					if m.deleteWTIdx >= 0 && m.deleteWTIdx < len(repo.Worktrees) {
						if deleteHasRemote(repo.Worktrees[m.deleteWTIdx].Status) {
							m.deleteRemoteBranch = !m.deleteRemoteBranch
							return m, nil
						}
					}
				}
			}
		}
		return m, nil
	}

	if m.modal.Active {
		modal := m.modal.View(m.width, m.height)
		contentStartY, _, _ := modalMetrics(modal, m.height)

		switch m.modal.Step {
		case ui.ModalSelectRepos:
			repoStartY := contentStartY + 4 // title(1) + empty(1) + description(1) + empty(1)
			hoveredRepo := msg.Y - repoStartY
			if hoveredRepo >= 0 && hoveredRepo < len(m.modal.Repos) {
				m.modal.CursorIdx = hoveredRepo
			}

			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				clickedRepo := msg.Y - repoStartY
				if clickedRepo >= 0 && clickedRepo < len(m.modal.Repos) {
					m.modal.CursorIdx = clickedRepo
					if m.modal.Selected[clickedRepo] {
						delete(m.modal.Selected, clickedRepo)
					} else {
						m.modal.Selected[clickedRepo] = true
					}
				}
			}

		case ui.ModalEnterBranch:
			// Branch list mouse handling
			branchStartY := contentStartY + m.modal.BranchListContentOffset()
			visible := ui.BranchListMaxVisible
			total := len(m.modal.FilteredBranches)
			if total < visible {
				visible = total
			}
			maxScroll := total - visible
			if maxScroll < 0 {
				maxScroll = 0
			}

			modalW := lipgloss.Width(modal)
			modalLeft := (m.width - modalW) / 2
			contentLeft := modalLeft + 3 // border(1) + padding(2)

			hasScrollbar := total > visible
			// List width matches render: fullW(54) - 3(gap+scroll+pad) = 51, or 54 if no scrollbar
			listW := 54
			if hasScrollbar {
				listW = 54 - 3
			}
			listRight := contentLeft + listW - 1
			scrollbarX := contentLeft + listW + 1 // after the 1-char space gap

			rowInList := msg.Y - branchStartY
			onScrollbar := hasScrollbar && msg.X >= scrollbarX && msg.X <= scrollbarX+1
			inList := msg.X >= contentLeft && msg.X <= listRight && rowInList >= 0 && rowInList < visible

			// Scrollbar interaction
			m.modal.ScrollbarHovered = false
			if onScrollbar && rowInList >= 0 && rowInList < visible {
				isDrag := msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
				isHeldMotion := msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft
				isDragging := (isDrag || isHeldMotion) && maxScroll > 0

				// Show thick thumb when hovering the thumb OR actively dragging
				if isDragging {
					m.modal.ScrollbarHovered = true
				} else {
					thumbLen := visible * visible / total
					if thumbLen < 1 {
						thumbLen = 1
					}
					if thumbLen > visible {
						thumbLen = visible
					}
					thumbStart := 0
					trackSpace := visible - thumbLen
					if maxScroll > 0 && trackSpace > 0 {
						thumbStart = m.modal.BranchScroll * trackSpace / maxScroll
					}
					if rowInList >= thumbStart && rowInList < thumbStart+thumbLen {
						m.modal.ScrollbarHovered = true
					}
				}

				if isDragging {
					if visible > 1 {
						m.modal.BranchScroll = rowInList * maxScroll / (visible - 1)
					}
					if m.modal.BranchScroll > maxScroll {
						m.modal.BranchScroll = maxScroll
					}
				}
			} else if inList {
				// Hover: highlight branch items
				m.modal.BranchHovered = m.modal.BranchScroll + rowInList

				// Click: fill input with branch name
				if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
					idx := m.modal.BranchScroll + rowInList
					if idx < total {
						m.modal.Input.SetValue(m.modal.FilteredBranches[idx].Name)
						m.modal.Input.CursorEnd()
						m.modal.FilterBranches()
					}
				}
			} else {
				m.modal.BranchHovered = -1
			}

			// Mouse wheel: scroll branch list
			if msg.Action == tea.MouseActionPress {
				if msg.Button == tea.MouseButtonWheelUp && m.modal.BranchScroll > 0 {
					m.modal.BranchScroll--
				}
				if msg.Button == tea.MouseButtonWheelDown && m.modal.BranchScroll < maxScroll {
					m.modal.BranchScroll++
				}
			}

		case ui.ModalConfirm:
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				var cmd tea.Cmd
				m.modal, cmd = m.modal.Update(tea.KeyMsg{Type: tea.KeyEnter})
				return m, cmd
			}
		}
		return m, nil
	}

	if m.renameActive {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.renameWTIdx >= 0 && renameHasRemote(m) {
				modal := m.renderRenameDialog()
				_, modalTop, modalH := modalMetrics(modal, m.height)
				toggleY := modalTop + modalH - 3 - 2 // 2 lines above bottom hint
				if msg.Y == toggleY {
					m.renameRemoteBranch = !m.renameRemoteBranch
					return m, nil
				}
			}
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
		wasVersionHovered := m.versionHovered
		prevHoveredUsage := m.hoveredUsage
		m.versionHovered = false
		m.hoveredUsage = -1

		// Check version label hover (fixed screen position in header)
		vx, vy, vw := ui.VersionHitZone()
		if y == vy && x >= vx && x < vx+vw {
			m.versionHovered = true
			m.recomputeLayout()
			return m, nil
		}

		// Check click usage toggle hover (fixed screen position in header)
		usageY, usageZones := ui.ClickUsageHitZones(m.width, m.config.AICliLabel())
		if y == usageY {
			for _, z := range usageZones {
				if x >= z.X && x < z.X+z.W {
					m.hoveredUsage = z.Usage
					break
				}
			}
		}

		// Recompute header if any header hover state changed
		if wasVersionHovered != m.versionHovered || prevHoveredUsage != m.hoveredUsage {
			m.recomputeLayout()
		}

		// Check footer buttons (footer is at fixed screen position, 1 line tall)
		screenFooterY := m.headerH + m.contentHeight - m.scrollY
		if y == screenFooterY && screenFooterY > 0 {
			m.focusedCard = -1
			m.focusedWT = -1
			if len(m.repos) > 0 {
				m.hoveredBtn = ui.GetFooterButtonAtX(x, m.width)
			} else {
				m.hoveredBtn = ui.GetFooterMinimalButtonAtX(x, m.width)
			}
			return m, nil
		}

		// Hit test grid (using virtual Y)
		repoIdx, wtIdx, btn := ui.HitTest(m.gridResult.HitZones, x, virtualY)
		m.focusedCard = repoIdx
		m.focusedWT = wtIdx
		if btn != ui.BtnNone {
			m.hoveredBtn = btn
		}

		// History suggestion hover (empty state)
		prevHistory := m.hoveredHistory
		m.hoveredHistory = ui.HistoryHitTest(m.gridResult.HitZones, x, virtualY)
		if prevHistory != m.hoveredHistory {
			m.recomputeLayout()
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

		// Check create button hover (virtual Y + X bounds, only when repos exist)
		if len(m.repos) > 0 && virtualY >= m.createBtnY && virtualY < m.createBtnY+3 && m.createBtnY > 0 {
			cbX, cbW := ui.CreateBtnHitZone(m.width)
			if x >= cbX && x < cbX+cbW {
				m.hoveredBtn = ui.BtnCreateWT
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

		// Check footer at click position (fixed screen position, 1 line tall)
		screenFooterY := m.headerH + m.contentHeight - m.scrollY
		if y == screenFooterY && screenFooterY > 0 {
			if len(m.repos) > 0 {
				m.hoveredBtn = ui.GetFooterButtonAtX(x, m.width)
			} else {
				m.hoveredBtn = ui.GetFooterMinimalButtonAtX(x, m.width)
			}
		}

		// Check create button (virtual Y + X bounds)
		if virtualY >= m.createBtnY && virtualY < m.createBtnY+3 && m.createBtnY > 0 {
			cbX, cbW := ui.CreateBtnHitZone(m.width)
			if x >= cbX && x < cbX+cbW {
				m.hoveredBtn = ui.BtnCreateWT
			}
		}

		// Check version label click (fixed screen position in header)
		vx, vy, vw := ui.VersionHitZone()
		if y == vy && x >= vx && x < vx+vw {
			url := ui.ReleaseURL()
			cmd := exec.Command("open", url)
			_ = cmd.Start()
			m.statusMsg = "Opened release page"
			return m, clearStatusCmd()
		}

		// Check click usage toggle click (fixed screen position in header)
		usageY, usageZones := ui.ClickUsageHitZones(m.width, m.config.AICliLabel())
		if y == usageY {
			for _, z := range usageZones {
				if x >= z.X && x < z.X+z.W && z.Usage != m.clickUsage {
					m.clickUsage = z.Usage
					m.statusMsg = fmt.Sprintf("Click usage: %s", m.clickUsage)
					m.recomputeLayout()
					return m, clearStatusCmd()
				}
			}
		}

		// History suggestion click (empty state)
		histIdx := ui.HistoryHitTest(m.gridResult.HitZones, x, virtualY)
		if histIdx >= 0 {
			suggestions := config.GetHistorySuggestions(m.config.WorkDir)
			if histIdx < len(suggestions) {
				m.RelaunchDir = suggestions[histIdx].Path
				return m, tea.Quit
			}
		}

		if m.loading {
			return m, nil
		}

		// Footer buttons (refresh/cleanup only when repos exist)
		if m.hoveredBtn == ui.BtnRefreshAll && len(m.repos) > 0 {
			logFn, startCmd := m.beginLoading("Refreshing all repos...")
			return m, tea.Batch(startCmd, refreshAllCmd(logFn, &m.config))
		}
		if m.hoveredBtn == ui.BtnCleanupMerged && len(m.repos) > 0 {
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

		// Create button (only when repos exist)
		if m.hoveredBtn == ui.BtnCreateWT && len(m.repos) > 0 {
			m.modal = ui.NewModal(m.repos, m.config.WorkDir)
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

		// Click on repo header (not on button) — open main repo
		if m.focusedCard >= 0 && m.focusedWT == -2 && m.hoveredBtn == ui.BtnNone {
			repo := m.repos[m.focusedCard]
			if repo.Path != "" {
				err := opener.OpenRepo(repo.Path, m.clickUsage, m.config.Global.IDECommand, m.config.Global.AICliCommand, m.config.Global.Terminal)
				if err != nil {
					m.statusMsg = fmt.Sprintf("Failed to open: %s", err.Error())
				} else {
					m.statusMsg = fmt.Sprintf("Opened %s in %s", repo.Name, m.clickUsage)
				}
				return m, clearStatusCmd()
			}
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
	hasRepos := len(m.repos) > 0
	var scrollable []string
	scrollable = append(scrollable, m.gridResult.View)
	if hasRepos {
		scrollable = append(scrollable, ui.RenderStatusLegend(m.width))
		scrollable = append(scrollable, ui.RenderCreateButton(m.width, m.hoveredBtn == ui.BtnCreateWT))
	}
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

	// Footer — fixed at bottom (minimal when no repos)
	if hasRepos {
		sections = append(sections, ui.RenderFooter(m.width, m.hoveredBtn))
	} else {
		sections = append(sections, ui.RenderFooterMinimal(m.width, m.hoveredBtn))
	}

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
	placeDialog := func(modal string) string {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	// Context menu
	if m.contextMenu.Active {
		dialog := ui.RenderContextMenuPlaced(m.contextMenu, m.width, m.height)
		return paintBlack(dialog, m.width, m.height)
	}

	// Rename dialog
	if m.renameActive {
		return paintBlack(placeDialog(m.renderRenameDialog()), m.width, m.height)
	}

	// Open workspace prompt
	if m.openPromptActive {
		return paintBlack(placeDialog(m.renderOpenPromptDialog()), m.width, m.height)
	}

	// Cleanup confirmation
	if m.cleanupConfirmActive {
		return paintBlack(placeDialog(m.renderCleanupConfirmDialog()), m.width, m.height)
	}

	// Delete confirmation
	if m.deleteConfirmActive {
		return paintBlack(placeDialog(m.renderDeleteConfirmDialog()), m.width, m.height)
	}

	// Settings
	if m.settings.Active {
		dialog := m.settings.View(m.width, m.height)
		return paintBlack(dialog, m.width, m.height)
	}

	// Create worktree modal
	if m.modal.Active {
		dialog := m.modal.ViewPlaced(m.width, m.height)
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

	return ui.ModalStyle.Width(50).Render(content)
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

	return ui.ModalStyle.Width(50).Render(content)
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

	return ui.ModalStyle.Width(56).Render(content)
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

	return ui.ModalStyle.Width(56).Render(content)
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

func checkMigrationCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		needed := git.NeedsMigration(cfg.WorkDir) || git.NeedsWorkspaceRepair(cfg.WorkDir)
		return MigrationCheckMsg{Needed: needed}
	}
}

func doMigrationCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		count := git.MigrateDirectoryStructure(cfg.WorkDir)
		// Also repair workspace file contents that may have been left stale
		count += git.RepairWorkspaceContents(cfg.WorkDir)
		return MigrationDoneMsg{Count: count}
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

func rebaseCmd(logFn git.LogFunc, wtPath, mainBranch, pkgManager string, repoIdx, wtIdx int) tea.Cmd {
	return func() tea.Msg {
		err := git.RebaseWorktree(wtPath, mainBranch, pkgManager, logFn)
		return RebaseDoneMsg{RepoIdx: repoIdx, WTIdx: wtIdx, Err: err}
	}
}

func deleteCmd(logFn git.LogFunc, repoPath, wtPath, branch string, deleteRemote bool, repoIdx, wtIdx int) tea.Cmd {
	return func() tea.Msg {
		err := git.DeleteWorktree(repoPath, wtPath, branch, deleteRemote, logFn)
		return DeleteDoneMsg{RepoIdx: repoIdx, WTIdx: wtIdx, Err: err}
	}
}

func deleteMonorepoCmd(logFn git.LogFunc, scriptDir, branchSubdir, branch string, repoNames []string, deleteRemote bool, repoIdx, wtIdx int) tea.Cmd {
	return func() tea.Msg {
		err := git.DeleteMonorepoWorktree(scriptDir, branchSubdir, branch, repoNames, deleteRemote, logFn)
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
