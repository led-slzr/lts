package app

import (
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/ui"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func handleKeyPress(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	// If open prompt is active
	if m.openPromptActive {
		return handleOpenPromptKey(m, msg)
	}

	// If context menu is active
	if m.contextMenu.Active {
		return handleContextMenuKey(m, msg)
	}

	// If cleanup confirmation is active
	if m.cleanupConfirmActive {
		return handleCleanupConfirmKey(m, msg)
	}

	// If delete confirmation is active
	if m.deleteConfirmActive {
		return handleDeleteConfirmKey(m, msg)
	}

	// If modal is active, delegate to modal
	if m.modal.Active {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(msg)
		return m, cmd
	}

	// If rename input is active, delegate to rename
	if m.renameActive {
		return handleRenameKey(m, msg)
	}

	// Explorer navigation intercepts movement keys; everything else falls
	// through to the shared bindings below
	if m.explorerActive() {
		if handled, m2, cmd := handleExplorerKey(m, msg); handled {
			return m2, cmd
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		m.clickUsage = m.clickUsage.Next()
		return m, nil

	case "shift+tab":
		m.toggleLayout()
		return m, clearStatusCmd()

	case "r":
		if !m.anyBusy() && len(m.repos) > 0 {
			lock := m.allRepoNames()
			logFn, startCmd := m.beginOp("Refreshing all repos...", lock...)
			return m, tea.Batch(startCmd, refreshAllCmd(logFn, &m.config, lock))
		}

	case "c":
		if !m.anyBusy() && len(m.repos) > 0 {
			m.cleanupConfirmActive = true
			m.cleanupRemoteBranch = false
			m.statusMsg = "Cleanup merged worktrees? [Y]es / [N]o"
			return m, nil
		}

	case "n":
		if len(m.repos) > 0 {
			m.modal = ui.NewModal(m.repos, m.config.WorkDir, m.config.GetRepoPackageManager, m.config.Global.InstallOnCreate)
			return m, textinput.Blink
		}

	case "s":
		var repoNames []string
		for _, r := range m.repos {
			if !r.IsMonorepo {
				repoNames = append(repoNames, r.Name)
			}
		}
		m.settings = ui.NewSettings(&m.config, repoNames)
		m.settings.ViewHeight = m.height
		m.settings.ViewWidth = m.width
		return m, nil

	case "l":
		if m.logPanel.Visible {
			m.logPanel.Clear()
		}
		return m, nil

	case "up", "k":
		if m.scrollY > 0 {
			m.scrollY -= 2
			if m.scrollY < 0 {
				m.scrollY = 0
			}
		}
		return m, nil

	case "down", "j":
		m.scrollY += 2
		m.clampScroll()
		return m, nil

	case "esc":
		m.focusedCard = -1
		m.focusedWT = -1
		return m, nil
	}

	return m, nil
}

func handleContextMenuKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.contextMenu.Active = false
		return m, nil
	case "up", "k":
		if m.contextMenu.CursorIdx > 0 {
			m.contextMenu.CursorIdx--
		}
		return m, nil
	case "down", "j":
		if m.contextMenu.CursorIdx < len(m.contextMenu.Items)-1 {
			m.contextMenu.CursorIdx++
		}
		return m, nil
	case "enter":
		m.contextMenu.Active = false
		if m.contextMenu.CursorIdx >= 0 && m.contextMenu.CursorIdx < len(m.contextMenu.Items) {
			item := m.contextMenu.Items[m.contextMenu.CursorIdx]
			return executeContextAction(m, item.Action)
		}
	}
	return m, nil
}

// executeContextAction acts on the repo/worktree snapshotted when the context
// menu opened (m.menuRepo/m.menuWT) — never on indices into m.repos, which a
// background reload may have shifted since.
func executeContextAction(m Model, action ui.HoverButton) (Model, tea.Cmd) {
	repo := m.menuRepo
	wt := m.menuWT
	hasWT := m.menuHasWT

	switch action {
	case ui.BtnRefresh:
		if repo.Path == "" {
			m.statusMsg = "Refresh individual repos instead"
			return m, clearStatusCmd()
		}
		logFn, startCmd := m.beginOp("Refreshing "+repo.Name+"...", repo.Name)
		return m, tea.Batch(startCmd, singleRefreshCmd(logFn, repo.Path, m.config.GetRepoBasisBranch(repo.Name), repo.Name))

	case ui.BtnBasis:
		// Open rename-style input for basis branch
		m.renameActive = true
		m.renameRepo = repo
		m.renameIsBasis = true
		m.renameInput.SetValue(m.config.GetRepoBasisBranch(repo.Name))
		m.renameInput.Focus()
		m.statusMsg = "Enter new basis branch for " + repo.Name
		return m, textinput.Blink

	case ui.BtnRebase:
		if hasWT {
			// Monorepo worktree paths are branch subdirs, not git worktrees —
			// the menu hides Rebase there, but guard against stray dispatch
			if repo.IsMonorepo {
				m.statusMsg = "Rebase isn't supported for monorepo worktrees yet"
				return m, clearStatusCmd()
			}
			lock := lockSet(repo)
			logFn, startCmd := m.beginOp("Rebasing "+wt.Branch+"...", lock...)
			return m, tea.Batch(startCmd, rebaseCmd(logFn, wt.Path, repo.MainBranch, m.config.GetRepoPackageManager(repo.Name), wt.Branch, lock))
		}

	case ui.BtnRename:
		if hasWT {
			m.renameActive = true
			m.renameRepo = repo
			m.renameWT = wt
			m.renameIsBasis = false
			m.renameRemoteBranch = false
			m.renameInput.SetValue("")
			m.renameInput.Focus()
			return m, textinput.Blink
		}

	case ui.BtnDelete:
		if hasWT {
			// Protected branches: the worktree can be removed, the branch never is
			protected := git.IsProtectedBranch(wt.Branch)
			_, dangerous := deleteWarning(wt.Status)
			m.deleteConfirmActive = true
			m.deleteRepo = repo
			m.deleteWT = wt
			m.deleteDangerous = dangerous
			m.deleteProtected = protected
			m.deleteRemoteBranch = false
			m.deleteLocalBranch = !protected
			if dangerous {
				m.deleteTypedInput.SetValue("")
				m.deleteTypedInput.Focus()
				m.statusMsg = "Delete " + wt.Branch + "? Type DELETE to confirm"
				return m, textinput.Blink
			}
			m.statusMsg = "Delete " + wt.Branch + "? [Y]es / [N]o"
			return m, nil
		}
	}

	return m, nil
}

func confirmDelete(m Model) (Model, tea.Cmd) {
	m.deleteConfirmActive = false
	m.deleteDangerous = false
	m.deleteProtected = false
	repo, wt := m.deleteRepo, m.deleteWT
	if wt.Path == "" {
		m.statusMsg = ""
		return m, nil
	}
	lock := lockSet(repo)
	logFn, startCmd := m.beginOp("Deleting "+wt.Branch+"...", lock...)
	if repo.IsMonorepo {
		return m, tea.Batch(startCmd, deleteMonorepoCmd(logFn, m.config.WorkDir, wt.Path, wt.Branch, repo.RepoNames, m.deleteLocalBranch, m.deleteRemoteBranch, lock))
	}
	return m, tea.Batch(startCmd, deleteCmd(logFn, repo.Path, wt.Path, wt.Branch, m.deleteLocalBranch, m.deleteRemoteBranch, lock))
}

func cancelDelete(m Model) (Model, tea.Cmd) {
	m.deleteConfirmActive = false
	m.deleteDangerous = false
	m.deleteRemoteBranch = false
	m.deleteLocalBranch = false
	m.deleteProtected = false
	m.statusMsg = ""
	return m, nil
}

func handleCleanupConfirmKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n", "N":
		m.cleanupConfirmActive = false
		m.cleanupRemoteBranch = false
		m.statusMsg = ""
		return m, nil
	case "d", "D":
		m.cleanupRemoteBranch = !m.cleanupRemoteBranch
		return m, nil
	case "y", "Y":
		m.cleanupConfirmActive = false
		deleteRemote := m.cleanupRemoteBranch
		m.cleanupRemoteBranch = false
		lock := m.allRepoNames()
		logFn, startCmd := m.beginOp("Cleaning up merged...", lock...)
		return m, tea.Batch(startCmd, cleanupCmd(logFn, &m.config, deleteRemote, lock))
	}
	return m, nil
}

func handleDeleteConfirmKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	// Esc always cancels
	if msg.String() == "esc" {
		return cancelDelete(m)
	}

	// Ctrl+d toggles remote branch deletion when remote exists
	if msg.String() == "ctrl+d" && !m.deleteProtected {
		if deleteHasRemote(m.deleteWT.Status) && m.deleteLocalBranch {
			m.deleteRemoteBranch = !m.deleteRemoteBranch
			return m, nil
		}
	}

	// Ctrl+b toggles local branch deletion (both modes)
	if msg.String() == "ctrl+b" && !m.deleteProtected {
		m.deleteLocalBranch = !m.deleteLocalBranch
		if !m.deleteLocalBranch {
			m.deleteRemoteBranch = false
		}
		return m, nil
	}

	if m.deleteDangerous {
		// "Type DELETE" mode
		switch msg.String() {
		case "enter":
			if strings.ToUpper(strings.TrimSpace(m.deleteTypedInput.Value())) == "DELETE" {
				return confirmDelete(m)
			}
			return m, nil
		default:
			var cmd tea.Cmd
			m.deleteTypedInput, cmd = m.deleteTypedInput.Update(msg)
			return m, cmd
		}
	}

	// Simple Y/N mode — d toggles remote, b toggles local branch deletion
	switch msg.String() {
	case "b", "B":
		if m.deleteProtected {
			return m, nil
		}
		m.deleteLocalBranch = !m.deleteLocalBranch
		if !m.deleteLocalBranch {
			m.deleteRemoteBranch = false
		}
		return m, nil
	case "d", "D":
		if m.deleteProtected {
			return m, nil
		}
		if deleteHasRemote(m.deleteWT.Status) && m.deleteLocalBranch {
			m.deleteRemoteBranch = !m.deleteRemoteBranch
			return m, nil
		}
	case "y", "Y":
		return confirmDelete(m)
	case "n", "N":
		return cancelDelete(m)
	}
	return m, nil
}

func handleOpenPromptKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "left", "h", "shift+tab":
		m.openPromptSelection = (m.openPromptSelection + 2) % 3
	case "right", "l", "tab":
		m.openPromptSelection = (m.openPromptSelection + 1) % 3
	case "y", "Y", "enter":
		return openCreatedWorkspaces(m, m.openPromptSelection)
	case "n", "N", "esc":
		m.openPromptActive = false
		return m, clearStatusCmd()
	}
	return m, nil
}

// openCreatedWorkspaces opens the just-created workspace(s) with the chosen
// mode and dismisses the prompt. IDE mode opens the workspace file itself;
// AI CLI/terminal modes need a directory to cd into.
func openCreatedWorkspaces(m Model, usage opener.ClickUsage) (Model, tea.Cmd) {
	m.openPromptActive = false
	// Monorepo creates share one workspace file across all results.
	shared := make(map[string]int)
	for _, r := range m.openPromptResults {
		if r != nil && r.WorkspaceFile != "" {
			shared[r.WorkspaceFile]++
		}
	}
	opened := make(map[string]bool)
	var openErr error
	for _, r := range m.openPromptResults {
		if r == nil || r.WorkspaceFile == "" {
			continue
		}
		target := r.WorkspaceFile
		if usage != opener.ClickIDE {
			if shared[r.WorkspaceFile] > 1 {
				// branch subdir containing all of the monorepo's worktrees
				target = filepath.Dir(r.WorkspaceFile)
			} else {
				target = r.WorktreePath
			}
		}
		if opened[target] {
			continue
		}
		opened[target] = true
		if err := opener.OpenWorktree(target, usage, m.openerOpts()); err != nil {
			openErr = err
		} else {
			m.markSessionLive(target, usage)
		}
	}
	if openErr != nil {
		m.statusMsg = "Failed to open: " + openErr.Error()
	} else {
		m.statusMsg = "Opened workspace(s)"
	}
	return m, clearStatusCmd()
}

func renameHasRemote(m Model) bool {
	return !m.renameIsBasis && deleteHasRemote(m.renameWT.Status)
}

func handleRenameKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	// Ctrl+d toggles remote rename (only for branch renames with remote)
	if msg.String() == "ctrl+d" && renameHasRemote(m) {
		m.renameRemoteBranch = !m.renameRemoteBranch
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.renameActive = false
		m.renameRemoteBranch = false
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.renameInput.Value())

		// Basis branch change
		if m.renameIsBasis {
			if value == "" {
				m.statusMsg = "Basis branch cannot be empty"
				return m, clearStatusCmd()
			}
			repo := m.renameRepo
			// Validate branch exists in the repo (local or remote)
			if repo.Path != "" {
				_, localErr := git.RunGit(repo.Path, "show-ref", "--verify", "--quiet", "refs/heads/"+value)
				_, remoteErr := git.RunGit(repo.Path, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+value)
				if localErr != nil && remoteErr != nil {
					m.statusMsg = "Branch '" + value + "' not found in " + repo.Name
					return m, clearStatusCmd()
				}
			}
			m.renameActive = false
			m.config.SetRepoBasisBranch(repo.Name, value)
			m.statusMsg = "Basis branch for " + repo.Name + " set to " + value
			return m, tea.Batch(loadReposCmd(&m.config), clearStatusCmd())
		}

		// Branch rename — validate before closing dialog
		if err := git.ValidateBranchName(value); err != nil {
			m.statusMsg = "Invalid: " + err.Error()
			return m, clearStatusCmd()
		}
		repo, wt := m.renameRepo, m.renameWT
		if wt.Path == "" {
			m.renameActive = false
			return m, nil
		}
		m.renameActive = false
		renameRemote := m.renameRemoteBranch
		m.renameRemoteBranch = false
		lock := lockSet(repo)
		logFn, startCmd := m.beginOp("Renaming "+wt.Branch+"...", lock...)
		if repo.IsMonorepo {
			// wt.Path is the branch subdirectory for monorepo worktrees
			return m, tea.Batch(startCmd, renameMonorepoCmd(logFn, wt.Path, repo.RepoNames, wt.Branch, value, renameRemote, &m.config, lock))
		}
		return m, tea.Batch(startCmd, renameCmd(logFn, repo.Path, wt.Path, wt.Branch, value, renameRemote, &m.config, lock))
	default:
		var cmd tea.Cmd
		m.renameInput, cmd = m.renameInput.Update(msg)
		return m, cmd
	}
}

// handleExplorerKey processes Explorer-layout navigation and row actions.
// Returns handled=false for keys that should fall through to the shared
// main-view bindings.
func handleExplorerKey(m Model, msg tea.KeyMsg) (bool, Model, tea.Cmd) {
	st := &m.explorer
	repo := m.repos[st.SelectedRepo]

	switch msg.String() {
	case "up", "k":
		if st.FocusSheet {
			if st.SelectedWT > 0 {
				st.SelectedWT--
				m.ensureExplorerRowVisible()
			}
		} else if st.SelectedRepo > 0 {
			st.SelectedRepo--
			st.SelectedWT = 0
			st.SheetScroll = 0
			m.ensureExplorerRepoVisible()
		}
		return true, m, nil

	case "down", "j":
		if st.FocusSheet {
			if st.SelectedWT < len(repo.Worktrees)-1 {
				st.SelectedWT++
				m.ensureExplorerRowVisible()
			}
		} else if st.SelectedRepo < len(m.repos)-1 {
			st.SelectedRepo++
			st.SelectedWT = 0
			st.SheetScroll = 0
			m.ensureExplorerRepoVisible()
		}
		return true, m, nil

	case "left", "h":
		st.FocusSheet = false
		return true, m, nil

	case "right", "l":
		if len(repo.Worktrees) > 0 {
			st.FocusSheet = true
			if st.SelectedWT < 0 {
				st.SelectedWT = 0
			}
		}
		return true, m, nil

	case "esc":
		if st.FocusSheet {
			st.FocusSheet = false
			return true, m, nil
		}
		return false, m, nil

	case "enter":
		if !st.FocusSheet {
			if len(repo.Worktrees) > 0 {
				st.FocusSheet = true
				if st.SelectedWT < 0 {
					st.SelectedWT = 0
				}
			}
			return true, m, nil
		}
		m2, cmd := explorerAction(m, ui.BtnOpen)
		return true, m2, cmd

	case "b":
		if st.FocusSheet {
			m2, cmd := explorerAction(m, ui.BtnRebase)
			return true, m2, cmd
		}
	case "m":
		if st.FocusSheet {
			m2, cmd := explorerAction(m, ui.BtnRename)
			return true, m2, cmd
		}
	case "d":
		if st.FocusSheet {
			m2, cmd := explorerAction(m, ui.BtnDelete)
			return true, m2, cmd
		}
	}
	return false, m, nil
}

// explorerAction runs an action-strip action on the selected worktree,
// snapshotting the target (same contract as the context menu).
func explorerAction(m Model, action ui.HoverButton) (Model, tea.Cmd) {
	if m.explorer.SelectedRepo >= len(m.repos) {
		return m, nil
	}
	repo := m.repos[m.explorer.SelectedRepo]
	if m.explorer.SelectedWT < 0 || m.explorer.SelectedWT >= len(repo.Worktrees) {
		return m, nil
	}
	wt := repo.Worktrees[m.explorer.SelectedWT]

	if action == ui.BtnOpen {
		err := opener.OpenWorktree(wt.Path, m.clickUsage, m.openerOpts())
		if err != nil {
			m.statusMsg = "Failed to open: " + err.Error()
		} else {
			m.statusMsg = "Opened " + wt.Branch + " in " + m.clickUsage.String() + m.tmuxFallbackNote(m.clickUsage)
			m.markSessionLive(wt.Path, m.clickUsage)
		}
		return m, clearStatusCmd()
	}

	// Mutations need the repo free
	if m.repoBusy(repo) {
		m.statusMsg = repo.Name + " is busy — wait for the running operation"
		return m, clearStatusCmd()
	}
	m.menuRepo = repo
	m.menuWT = wt
	m.menuHasWT = true
	return executeContextAction(m, action)
}
