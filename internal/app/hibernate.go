package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Hibernate: delete a GitHub-synced repo locally, safely. The audit proves
// GitHub has everything; untracked .env files are backed up first and
// offered back on re-clone. The repo stays locked from audit start until
// the dialog closes so nothing mutates it under the checklist.

// canHibernate gates the context-menu entry: single GitHub-remote repos
// with gh authenticated (the whole premise is "GitHub has it").
func (m *Model) canHibernate(repo git.Repo) bool {
	return m.ghState == ui.CloneReady && !repo.IsMonorepo && repo.Path != "" && m.githubRemotes[repo.Name]
}

// envBackupRoot is where hibernate parks untracked env files, one
// timestamped dir per backup: ~/.config/lts/env-backup/<repo>/<stamp>/.
func envBackupRoot() string {
	return filepath.Join(config.GlobalConfigDir(), "env-backup")
}

// startHibernate opens the dialog and dispatches the audit, holding the
// repo's lock for the whole dialog lifetime.
func startHibernate(m Model, repo git.Repo) (Model, tea.Cmd) {
	m.hibernateActive = true
	m.hibernateRepo = repo
	m.hibernateAudit = nil
	m.hibernateAuditPending = true
	lock := lockSet(repo)
	logFn, startCmd := m.beginOp("Auditing "+repo.Name+" for hibernation...", lock...)
	return m, tea.Batch(startCmd, hibernateAuditCmd(logFn, m.config.WorkDir, repo, m.config.GetRepoBasisBranch(repo.Name), lock))
}

func hibernateAuditCmd(logFn git.LogFunc, scriptDir string, repo git.Repo, basis string, locked []string) tea.Cmd {
	return func() tea.Msg {
		audit := git.AuditHibernate(scriptDir, repo, basis, logFn)
		return HibernateAuditMsg{RepoName: repo.Name, Audit: audit, Locked: locked}
	}
}

func hibernateCmd(logFn git.LogFunc, scriptDir string, repo git.Repo, audit git.HibernateAudit, backupRoot string, locked []string) tea.Cmd {
	return func() tea.Msg {
		// Backup before anything is deleted — a failed backup aborts the whole op.
		dest, n, err := git.BackupEnvs(audit.EnvFiles, backupRoot, repo.Name)
		if err != nil {
			return HibernateDoneMsg{RepoName: repo.Name, Locked: locked,
				Err: fmt.Errorf("env backup failed, nothing was deleted: %w", err)}
		}
		if n > 0 {
			logFn("hibernate:"+repo.Name, fmt.Sprintf("Backed up %d .env file(s) to %s", n, dest), false)
		}
		for _, wt := range repo.Worktrees {
			opener.KillSession(wt.Path)
		}
		opener.KillSession(repo.Path)
		if err := git.HibernateRepo(scriptDir, repo, logFn); err != nil {
			return HibernateDoneMsg{RepoName: repo.Name, EnvBacked: n, Locked: locked, Err: err}
		}
		return HibernateDoneMsg{RepoName: repo.Name, Freed: audit.FreedBytes, EnvBacked: n, Locked: locked}
	}
}

func handleHibernateKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.hibernateActive = false
		m.hibernateInput.Blur()
		if m.hibernateAuditPending {
			// The audit goroutine still holds the repo lock — it's released
			// when its result arrives and finds the dialog gone
			m.statusMsg = "Hibernate canceled"
			return m, clearStatusCmd()
		}
		m.hibernateAudit = nil
		m.clearBusy(lockSet(m.hibernateRepo)...)
		m.statusMsg = ""
		return m, nil

	case "enter":
		if m.hibernateAudit != nil && m.hibernateAudit.Blockers() == 0 &&
			strings.ToUpper(strings.TrimSpace(m.hibernateInput.Value())) == "DELETE" {
			return confirmHibernate(m)
		}
		return m, nil
	}

	// With blockers shown the typed input isn't active, so 'r' re-audits
	// (after pushing / dropping a stash in another terminal)
	if msg.String() == "r" && m.hibernateAudit != nil && m.hibernateAudit.Blockers() > 0 {
		return reauditHibernate(m)
	}
	if m.hibernateAudit != nil && m.hibernateAudit.Blockers() == 0 {
		var cmd tea.Cmd
		m.hibernateInput, cmd = m.hibernateInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func confirmHibernate(m Model) (Model, tea.Cmd) {
	repo := m.hibernateRepo
	audit := *m.hibernateAudit
	m.hibernateActive = false
	m.hibernateAudit = nil
	m.hibernateInput.Blur()
	lock := lockSet(repo) // already held since the audit — beginOp refreshes the label
	logFn, startCmd := m.beginOp("Hibernating "+repo.Name+"...", lock...)
	return m, tea.Batch(startCmd, hibernateCmd(logFn, m.config.WorkDir, repo, audit, envBackupRoot(), lock))
}

func reauditHibernate(m Model) (Model, tea.Cmd) {
	repo := m.hibernateRepo
	m.hibernateAudit = nil
	m.hibernateAuditPending = true
	lock := lockSet(repo)
	logFn, startCmd := m.beginOp("Auditing "+repo.Name+" for hibernation...", lock...)
	return m, tea.Batch(startCmd, hibernateAuditCmd(logFn, m.config.WorkDir, repo, m.config.GetRepoBasisBranch(repo.Name), lock))
}

func handleEnvRestoreKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.envRestoreActive = false
		repoPath := filepath.Join(m.config.WorkDir, m.envRestoreRepo)
		n, err := git.RestoreEnvBackup(m.envRestoreDir, repoPath)
		if err != nil {
			m.statusMsg = "Env restore failed: " + err.Error()
		} else {
			m.statusMsg = fmt.Sprintf("Restored %d .env file(s) into %s", n, m.envRestoreRepo)
			if m.envRestoreWT > 0 {
				m.statusMsg += fmt.Sprintf(" · %d worktree env(s) kept in the backup", m.envRestoreWT)
			}
		}
		return m, clearStatusCmd()
	case "n", "N", "esc":
		m.envRestoreActive = false
		m.statusMsg = "Backup kept at " + m.envRestoreDir
		return m, clearStatusCmd()
	}
	return m, nil
}

// renderHibernateDialog shows the audit checklist. Confirmation is only
// possible with zero blockers — hibernate has no force path.
func (m Model) renderHibernateDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorRed).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	okStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorGreen).Background(ui.ColorBlack)
	warnStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorYellow).Background(ui.ColorBlack)
	critStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorRed).Background(ui.ColorBlack)

	repo := m.hibernateRepo
	content := titleStyle.Render("Hibernate " + repo.Name)
	if m.hibernateAudit != nil {
		content += dimStyle.Render("  ·  frees ~" + git.HumanBytes(m.hibernateAudit.FreedBytes))
	}
	content += "\n\n"
	content += dimStyle.Render("Deletes the local repo and its worktrees — GitHub keeps the rest.") + "\n\n"

	if m.hibernateAudit == nil {
		content += dimStyle.Render("  Auditing — fetching origin, checking branches, stashes,") + "\n"
		content += dimStyle.Render("  worktrees and .env files...") + "\n\n"
		content += dimStyle.Render("  esc cancel")
		return ui.ModalStyle.Width(64).Render(content)
	}

	audit := *m.hibernateAudit
	const maxDetails = 4
	for _, c := range audit.Checks {
		switch c.State {
		case git.CheckFail:
			content += critStyle.Render("  ✗ "+c.Label) + "\n"
		case git.CheckWarn:
			content += warnStyle.Render("  ⚠ "+c.Label) + "\n"
		default:
			content += okStyle.Render("  ✓ ") + dimStyle.Render(c.Label) + "\n"
		}
		for i, d := range c.Details {
			if i == maxDetails {
				content += dimStyle.Render(fmt.Sprintf("      … and %d more", len(c.Details)-i)) + "\n"
				break
			}
			content += dimStyle.Render("      · "+d) + "\n"
		}
	}
	if n := len(audit.EnvFiles); n > 0 {
		content += warnStyle.Render(fmt.Sprintf("  ⚠ %d .env file(s) → backup: ~/.config/lts/env-backup/%s/", n, repo.Name)) + "\n"
		for i, f := range audit.EnvFiles {
			if i == maxDetails {
				content += dimStyle.Render(fmt.Sprintf("      … and %d more", n-i)) + "\n"
				break
			}
			content += dimStyle.Render("      · "+f.Rel) + "\n"
		}
	} else {
		content += okStyle.Render("  ✓ ") + dimStyle.Render("no untracked .env files") + "\n"
	}

	content += "\n"
	if audit.Blockers() > 0 {
		content += critStyle.Render("  Resolve the ✗ items above, then press r to re-audit") + "\n\n"
		content += dimStyle.Render("  r re-audit • esc close")
		return ui.ModalStyle.Width(64).Render(content)
	}
	content += dimStyle.Render("  Re-clone it anytime with (c)") + "\n\n"
	content += warnStyle.Render("  Type DELETE to confirm:") + "\n\n"
	content += "  " + m.hibernateInput.View() + "\n\n"
	content += dimStyle.Render("  enter confirm • esc cancel")
	return ui.ModalStyle.Width(64).Render(content)
}

// renderEnvRestoreDialog is the post-clone prompt offering the hibernate
// env backup back, each file to where it lived.
func (m Model) renderEnvRestoreDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorGreen).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)

	when := ""
	if ts, err := time.Parse("20060102-150405", filepath.Base(m.envRestoreDir)); err == nil {
		when = " from " + ts.Format("Jan 2, 2006")
	}
	content := titleStyle.Render("Restore .env backup?") + "\n\n"
	content += dimStyle.Render("A hibernate backup"+when+" exists for ") +
		whiteStyle.Render(m.envRestoreRepo) + dimStyle.Render(":") + "\n\n"
	content += whiteStyle.Render(fmt.Sprintf("  %d .env file(s)", m.envRestoreMain)) +
		dimStyle.Render(" can go back to where they lived") + "\n"
	if m.envRestoreWT > 0 {
		content += dimStyle.Render(fmt.Sprintf("  (%d worktree env(s) stay in the backup for manual retrieval)", m.envRestoreWT)) + "\n"
	}
	content += "\n"
	content += whiteStyle.Render("[Y]") + dimStyle.Render("es  ") + whiteStyle.Render("[N]") + dimStyle.Render("o")
	return ui.ModalStyle.Width(64).Render(content)
}
