package app

import (
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/ui"
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

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		m.clickUsage = m.clickUsage.Next()
		return m, nil

	case "r":
		if !m.loading {
			logFn, startCmd := m.beginLoading("Refreshing all repos...")
			return m, tea.Batch(startCmd, refreshAllCmd(logFn, &m.config))
		}

	case "c":
		if !m.loading {
			m.cleanupConfirmActive = true
			m.cleanupRemoteBranch = false
			m.statusMsg = "Cleanup merged worktrees? [Y]es / [N]o"
			return m, nil
		}

	case "n":
		if !m.loading {
			m.modal = ui.NewModal(m.repos)
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
			return executeContextAction(m, item.Action, m.contextMenu.RepoIdx, m.contextMenu.WTIdx)
		}
	}
	return m, nil
}

func executeContextAction(m Model, action ui.HoverButton, repoIdx, wtIdx int) (Model, tea.Cmd) {
	if repoIdx < 0 || repoIdx >= len(m.repos) {
		return m, nil
	}
	repo := m.repos[repoIdx]

	switch action {
	case ui.BtnRefresh:
		if repo.Path == "" {
			m.statusMsg = "Refresh individual repos instead"
			return m, clearStatusCmd()
		}
		logFn, startCmd := m.beginLoading("Refreshing " + repo.Name + "...")
		return m, tea.Batch(startCmd, singleRefreshCmd(logFn, repo.Path, m.config.GetRepoBasisBranch(repo.Name), repoIdx))

	case ui.BtnBasis:
		// Open rename-style input for basis branch
		m.renameActive = true
		m.renameRepoIdx = repoIdx
		m.renameWTIdx = -2 // flag: this is a basis branch change, not a rename
		m.renameInput.SetValue(m.config.GetRepoBasisBranch(repo.Name))
		m.renameInput.Focus()
		m.statusMsg = "Enter new basis branch for " + repo.Name
		return m, textinput.Blink

	case ui.BtnRebase:
		if wtIdx >= 0 && wtIdx < len(repo.Worktrees) {
			wt := repo.Worktrees[wtIdx]
			logFn, startCmd := m.beginLoading("Rebasing " + wt.Branch + "...")
			return m, tea.Batch(startCmd, rebaseCmd(logFn, wt.Path, repo.MainBranch, repoIdx, wtIdx))
		}

	case ui.BtnRename:
		if wtIdx >= 0 && wtIdx < len(repo.Worktrees) {
			m.renameActive = true
			m.renameRepoIdx = repoIdx
			m.renameWTIdx = wtIdx
			m.renameRemoteBranch = false
			m.renameInput.SetValue("")
			m.renameInput.Focus()
			return m, textinput.Blink
		}

	case ui.BtnDelete:
		if wtIdx >= 0 && wtIdx < len(repo.Worktrees) {
			wt := repo.Worktrees[wtIdx]
			// Block protected branches
			if git.IsProtectedBranch(wt.Branch) {
				m.statusMsg = "Cannot delete protected branch: " + wt.Branch
				return m, clearStatusCmd()
			}
			_, dangerous := deleteWarning(wt.Status)
			m.deleteConfirmActive = true
			m.deleteRepoIdx = repoIdx
			m.deleteWTIdx = wtIdx
			m.deleteDangerous = dangerous
			m.deleteRemoteBranch = false
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
	if m.deleteRepoIdx >= 0 && m.deleteRepoIdx < len(m.repos) {
		repo := m.repos[m.deleteRepoIdx]
		if m.deleteWTIdx >= 0 && m.deleteWTIdx < len(repo.Worktrees) {
			wt := repo.Worktrees[m.deleteWTIdx]
			logFn, startCmd := m.beginLoading("Deleting " + wt.Branch + "...")
			ri, wi := m.deleteRepoIdx, m.deleteWTIdx
			return m, tea.Batch(startCmd, deleteCmd(logFn, repo.Path, wt.Path, wt.Branch, m.deleteRemoteBranch, ri, wi))
		}
	}
	m.statusMsg = ""
	return m, nil
}

func cancelDelete(m Model) (Model, tea.Cmd) {
	m.deleteConfirmActive = false
	m.deleteDangerous = false
	m.deleteRemoteBranch = false
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
		logFn, startCmd := m.beginLoading("Cleaning up merged...")
		return m, tea.Batch(startCmd, cleanupCmd(logFn, &m.config, deleteRemote))
	}
	return m, nil
}

func handleDeleteConfirmKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	// Esc always cancels
	if msg.String() == "esc" {
		return cancelDelete(m)
	}

	// Ctrl+d toggles remote branch deletion when remote exists
	if msg.String() == "ctrl+d" {
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

	// Simple Y/N mode — d toggles remote deletion
	switch msg.String() {
	case "d", "D":
		if m.deleteRepoIdx >= 0 && m.deleteRepoIdx < len(m.repos) {
			repo := m.repos[m.deleteRepoIdx]
			if m.deleteWTIdx >= 0 && m.deleteWTIdx < len(repo.Worktrees) {
				if deleteHasRemote(repo.Worktrees[m.deleteWTIdx].Status) {
					m.deleteRemoteBranch = !m.deleteRemoteBranch
					return m, nil
				}
			}
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
	case "y", "Y", "enter":
		m.openPromptActive = false
		var openErr error
		for _, r := range m.openPromptResults {
			if r.WorkspaceFile != "" {
				if err := opener.OpenWorktree(r.WorkspaceFile, m.clickUsage, m.config.Global.IDECommand, m.config.Global.AICliCommand, m.config.Global.Terminal); err != nil {
					openErr = err
				}
			}
		}
		if openErr != nil {
			m.statusMsg = "Failed to open: " + openErr.Error()
		} else {
			m.statusMsg = "Opened workspace(s)"
		}
		return m, clearStatusCmd()
	case "n", "N", "esc":
		m.openPromptActive = false
		return m, clearStatusCmd()
	}
	return m, nil
}

func renameHasRemote(m Model) bool {
	if m.renameRepoIdx < 0 || m.renameRepoIdx >= len(m.repos) || m.renameWTIdx < 0 {
		return false
	}
	repo := m.repos[m.renameRepoIdx]
	if m.renameWTIdx >= len(repo.Worktrees) {
		return false
	}
	return deleteHasRemote(repo.Worktrees[m.renameWTIdx].Status)
}

func handleRenameKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	// Ctrl+d toggles remote rename (only for branch renames with remote)
	if msg.String() == "ctrl+d" && m.renameWTIdx >= 0 && renameHasRemote(m) {
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

		// Basis branch change (wtIdx == -2)
		if m.renameWTIdx == -2 && m.renameRepoIdx >= 0 && m.renameRepoIdx < len(m.repos) {
			if value == "" {
				m.statusMsg = "Basis branch cannot be empty"
				return m, clearStatusCmd()
			}
			repo := m.repos[m.renameRepoIdx]
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
		if m.renameRepoIdx >= 0 && m.renameRepoIdx < len(m.repos) &&
			m.renameWTIdx >= 0 && m.renameWTIdx < len(m.repos[m.renameRepoIdx].Worktrees) {
			m.renameActive = false
			repo := m.repos[m.renameRepoIdx]
			wt := repo.Worktrees[m.renameWTIdx]
			repoIdx := m.renameRepoIdx
			wtIdx := m.renameWTIdx
			renameRemote := m.renameRemoteBranch
			m.renameRemoteBranch = false
			logFn, startCmd := m.beginLoading("Renaming branch...")
			if repo.IsMonorepo {
				// wt.Path is the branch subdirectory for monorepo worktrees
				return m, tea.Batch(startCmd, renameMonorepoCmd(logFn, wt.Path, repo.RepoNames, wt.Branch, value, renameRemote, &m.config, repoIdx, wtIdx))
			}
			return m, tea.Batch(startCmd, renameCmd(logFn, repo.Path, wt.Path, wt.Branch, value, renameRemote, &m.config, repoIdx, wtIdx))
		}
		m.renameActive = false
		return m, nil
	default:
		var cmd tea.Cmd
		m.renameInput, cmd = m.renameInput.Update(msg)
		return m, cmd
	}
}
