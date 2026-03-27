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
			return m, refreshAllCmd(&m.config)
		}

	case "esc":
		// Clear any selection
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
		item := m.contextMenu.Items[m.contextMenu.CursorIdx]
		return executeContextAction(m, item.Action, m.contextMenu.RepoIdx, m.contextMenu.WTIdx)
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
		m.loading = true
		m.statusMsg = "Refreshing " + repo.Name + "..."
		return m, singleRefreshCmd(repo.Path, m.config.GetRepoBasisBranch(repo.Name), repoIdx)

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
			m.loading = true
			m.statusMsg = "Rebasing " + wt.Branch + "..."
			return m, rebaseCmd(wt.Path, repo.MainBranch, repoIdx, wtIdx)
		}

	case ui.BtnRename:
		if wtIdx >= 0 && wtIdx < len(repo.Worktrees) {
			m.renameActive = true
			m.renameRepoIdx = repoIdx
			m.renameWTIdx = wtIdx
			m.renameInput.SetValue("")
			m.renameInput.Focus()
			return m, textinput.Blink
		}

	case ui.BtnDelete:
		if wtIdx >= 0 && wtIdx < len(repo.Worktrees) {
			wt := repo.Worktrees[wtIdx]
			m.deleteConfirmActive = true
			m.deleteRepoIdx = repoIdx
			m.deleteWTIdx = wtIdx
			m.statusMsg = "Delete " + wt.Branch + "? [Y]es / [N]o"
			return m, nil
		}
	}

	return m, nil
}

func handleDeleteConfirmKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.deleteConfirmActive = false
		if m.deleteRepoIdx >= 0 && m.deleteRepoIdx < len(m.repos) {
			repo := m.repos[m.deleteRepoIdx]
			if m.deleteWTIdx >= 0 && m.deleteWTIdx < len(repo.Worktrees) {
				wt := repo.Worktrees[m.deleteWTIdx]
				m.loading = true
				m.statusMsg = "Deleting " + wt.Branch + "..."
				ri, wi := m.deleteRepoIdx, m.deleteWTIdx
				return m, deleteCmd(repo.Path, wt.Path, wt.Branch, ri, wi)
			}
		}
		m.statusMsg = ""
		return m, nil
	case "n", "N", "esc":
		m.deleteConfirmActive = false
		m.statusMsg = ""
		return m, nil
	}
	return m, nil
}

func handleOpenPromptKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.openPromptActive = false
		// Open all created workspace files
		for _, r := range m.openPromptResults {
			if r.WorkspaceFile != "" {
				opener.OpenWorktree(r.WorkspaceFile, m.clickUsage, m.config.Global.IDECommand, m.config.Global.AICliCommand, m.config.Global.Terminal)
			}
		}
		m.statusMsg = "Opened workspace(s)"
		return m, clearStatusCmd()
	case "n", "N", "esc":
		m.openPromptActive = false
		return m, clearStatusCmd()
	}
	return m, nil
}

func handleRenameKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.renameActive = false
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.renameInput.Value())
		m.renameActive = false

		// Basis branch change (wtIdx == -2)
		if m.renameWTIdx == -2 && m.renameRepoIdx >= 0 && m.renameRepoIdx < len(m.repos) {
			if value == "" {
				m.statusMsg = "Basis branch cannot be empty"
				return m, clearStatusCmd()
			}
			repo := m.repos[m.renameRepoIdx]
			m.config.SetRepoBasisBranch(repo.Name, value)
			m.statusMsg = "Basis branch for " + repo.Name + " set to " + value
			return m, tea.Batch(loadReposCmd(&m.config), clearStatusCmd())
		}

		// Branch rename
		if err := git.ValidateBranchName(value); err != nil {
			m.statusMsg = "Invalid branch: " + err.Error()
			return m, clearStatusCmd()
		}
		if m.renameRepoIdx >= 0 && m.renameWTIdx >= 0 {
			repo := m.repos[m.renameRepoIdx]
			wt := repo.Worktrees[m.renameWTIdx]
			repoIdx := m.renameRepoIdx
			wtIdx := m.renameWTIdx
			return m, renameCmd(wt.Path, value, repoIdx, wtIdx)
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.renameInput, cmd = m.renameInput.Update(msg)
		return m, cmd
	}
}
