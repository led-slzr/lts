package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Monorepo conversion dialogs. Convert promotes a single-repo worktree
// into a group (partners adopt an existing same-branch worktree or get a
// fresh one); Split reduces a group worktree to per-constituent outcomes
// (keep as single, or delete when its status is safe). Targets are
// snapshotted at open; locks are taken at confirm after a busy re-check.

type convertChoice struct {
	Name      string
	AdoptPath string // existing same-branch worktree ("" = create fresh)
	Basis     string // shown for fresh creates
	Selected  bool
}

type reduceRow struct {
	Name       string
	WtPath     string
	Branch     string
	StatusText string
	CanDelete  bool // dangerous statuses are locked to keep
	Delete     bool
}

// startConvert builds the partner picker for a single-repo worktree.
func startConvert(m Model, repo git.Repo, wt git.Worktree) (Model, tea.Cmd) {
	var choices []convertChoice
	for _, r := range m.repos {
		if r.IsMonorepo || r.Hidden || r.Path == "" || r.Name == repo.Name {
			continue
		}
		c := convertChoice{Name: r.Name, Basis: m.config.GetRepoBasisBranch(r.Name)}
		for _, pw := range r.Worktrees {
			if pw.Branch == wt.Branch {
				c.AdoptPath = pw.Path
				break
			}
		}
		choices = append(choices, c)
	}
	if len(choices) == 0 {
		m.statusMsg = "No other repos to group with"
		return m, clearStatusCmd()
	}
	m.convertActive = true
	m.convertRepo = repo
	m.convertWT = wt
	m.convertChoices = choices
	m.convertCursor = 0
	return m, nil
}

// startReduce builds the per-constituent fate list for a group worktree.
func startReduce(m Model, repo git.Repo, wt git.Worktree) (Model, tea.Cmd) {
	rows := constituentRows(repo, wt, m.config.GetRepoBasisBranch)
	if len(rows) == 0 {
		m.statusMsg = "No constituent worktrees found in " + filepath.Base(wt.Path)
		return m, clearStatusCmd()
	}
	m.reduceActive = true
	m.reduceRepo = repo
	m.reduceWT = wt
	m.reduceRows = rows
	m.reduceCursor = 0
	return m, nil
}

// constituentRows scans a branch subdir for each constituent's worktree and
// computes a FRESH status — delete is only offered where the delete dialog
// wouldn't demand a typed DELETE (nothing unpushed or uncommitted).
func constituentRows(repo git.Repo, wt git.Worktree, basis func(string) string) []reduceRow {
	names := append([]string{}, repo.RepoNames...)
	// Longest name first so "core-ui-feat-x" matches core-ui, not core.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if len(names[j]) > len(names[i]) {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	entries, err := os.ReadDir(wt.Path)
	if err != nil {
		return nil
	}
	var rows []reduceRow
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(wt.Path, e.Name())
		if !isWorktreeDirApp(p) {
			continue
		}
		for _, name := range names {
			if strings.HasPrefix(e.Name(), name+"-") {
				branch, _ := git.RunGit(p, "branch", "--show-current")
				branch = strings.TrimSpace(branch)
				if branch == "" {
					branch = wt.Branch
				}
				status, text := git.GetWorktreeStatus(p, basis(name))
				_, dangerous := deleteWarning(status)
				rows = append(rows, reduceRow{
					Name: name, WtPath: p, Branch: branch,
					StatusText: text, CanDelete: !dangerous,
				})
				break
			}
		}
	}
	return rows
}

func isWorktreeDirApp(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && !info.IsDir()
}

func handleConvertKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.convertActive = false
		return m, nil
	case "up", "k":
		if m.convertCursor > 0 {
			m.convertCursor--
		}
		return m, nil
	case "down", "j":
		if m.convertCursor < len(m.convertChoices)-1 {
			m.convertCursor++
		}
		return m, nil
	case " ":
		m.convertChoices[m.convertCursor].Selected = !m.convertChoices[m.convertCursor].Selected
		return m, nil
	case "enter":
		return confirmConvert(m)
	}
	return m, nil
}

func confirmConvert(m Model) (Model, tea.Cmd) {
	var partners []git.ConvertPartner
	lock := []string{m.convertRepo.Name}
	for _, c := range m.convertChoices {
		if c.Selected {
			partners = append(partners, git.ConvertPartner{Name: c.Name, AdoptPath: c.AdoptPath})
			lock = append(lock, c.Name)
		}
	}
	if len(partners) == 0 {
		return m, nil // nothing selected — dialog stays
	}
	for _, name := range lock {
		if _, busy := m.busy[name]; busy {
			m.statusMsg = name + " is busy — wait for the running operation"
			m.convertActive = false
			return m, clearStatusCmd()
		}
	}
	m.convertActive = false

	// Session names embed paths — kill sessions on everything that moves.
	opener.KillSession(m.convertWT.Path)
	delete(m.tmuxLive, opener.SessionName(m.convertWT.Path))
	for _, p := range partners {
		if p.AdoptPath != "" {
			opener.KillSession(p.AdoptPath)
			delete(m.tmuxLive, opener.SessionName(p.AdoptPath))
		}
	}

	logFn, startCmd := m.beginOp("Converting "+m.convertWT.Branch+" to a mono group...", lock...)
	return m, tea.Batch(startCmd, convertCmd(logFn, &m.config, m.convertRepo.Name, m.convertWT.Path, m.convertWT.Branch, partners, lock))
}

func convertCmd(logFn git.LogFunc, cfg *config.Config, repoName, wtPath, branch string, partners []git.ConvertPartner, locked []string) tea.Cmd {
	workDir := cfg.WorkDir
	resolve := basisResolver(cfg)
	opts := workspaceOpts(cfg)
	install := cfg.Global.InstallOnCreate
	return func() tea.Msg {
		res, err := git.ConvertToMono(workDir, repoName, wtPath, branch, partners, resolve, opts, install, logFn)
		group := ""
		if res != nil {
			group = res.GroupDir
		}
		return ConvertDoneMsg{Branch: branch, Group: group, Locked: locked, Err: err}
	}
}

func handleReduceKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.reduceActive = false
		return m, nil
	case "up", "k":
		if m.reduceCursor > 0 {
			m.reduceCursor--
		}
		return m, nil
	case "down", "j":
		if m.reduceCursor < len(m.reduceRows)-1 {
			m.reduceCursor++
		}
		return m, nil
	case " ":
		row := &m.reduceRows[m.reduceCursor]
		if row.CanDelete {
			row.Delete = !row.Delete
		}
		return m, nil
	case "enter":
		return confirmReduce(m)
	}
	return m, nil
}

func confirmReduce(m Model) (Model, tea.Cmd) {
	lock := lockSet(m.reduceRepo)
	for _, name := range lock {
		if _, busy := m.busy[name]; busy {
			m.statusMsg = name + " is busy — wait for the running operation"
			m.reduceActive = false
			return m, clearStatusCmd()
		}
	}
	m.reduceActive = false

	var decisions []git.ReduceDecision
	for _, r := range m.reduceRows {
		decisions = append(decisions, git.ReduceDecision{Name: r.Name, WtPath: r.WtPath, Branch: r.Branch, Delete: r.Delete})
		opener.KillSession(r.WtPath)
		delete(m.tmuxLive, opener.SessionName(r.WtPath))
	}
	opener.KillSession(m.reduceWT.Path)
	delete(m.tmuxLive, opener.SessionName(m.reduceWT.Path))

	logFn, startCmd := m.beginOp("Splitting "+m.reduceWT.Branch+"...", lock...)
	return m, tea.Batch(startCmd, reduceCmd(logFn, &m.config, m.reduceWT.Path, m.reduceWT.Branch, decisions, lock))
}

func reduceCmd(logFn git.LogFunc, cfg *config.Config, subdirPath, branch string, decisions []git.ReduceDecision, locked []string) tea.Cmd {
	workDir := cfg.WorkDir
	opts := workspaceOpts(cfg)
	return func() tea.Msg {
		kept, deleted, err := git.ReduceMono(workDir, subdirPath, decisions, opts, logFn)
		return ReduceDoneMsg{Branch: branch, Kept: kept, Deleted: deleted, Locked: locked, Err: err}
	}
}

// --- Rendering ---

// clipLine keeps a dialog row on one line — ModalStyle wraps overflow,
// which breaks the box border.
func clipLine(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

func (m Model) renderConvertDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorGreen).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorSelBg)
	cyanStyle := lipgloss.NewStyle().Foreground(ui.ColorCyan).Background(ui.ColorBlack)

	content := titleStyle.Render("Add to Mono Group") + "\n\n"
	content += dimStyle.Render("Group ") + cyanStyle.Render(m.convertWT.Branch) +
		dimStyle.Render(" across repos — pick partners:") + "\n\n"
	content += whiteStyle.Render("    "+m.convertRepo.Name) + dimStyle.Render("  (this worktree)") + "\n"
	for i, c := range m.convertChoices {
		box := "[ ]"
		if c.Selected {
			box = "[x]"
		}
		note := "new worktree from " + c.Basis
		if c.AdoptPath != "" {
			note = "adopts its existing " + m.convertWT.Branch
		}
		var line string
		if i == m.convertCursor {
			line = selStyle.Render("  ▸ "+box+" "+c.Name) + " " + dimStyle.Render("— "+note)
		} else {
			line = "    " + whiteStyle.Render(box+" "+c.Name) + " " + dimStyle.Render("— "+note)
		}
		content += clipLine(line, 56) + "\n"
	}
	content += "\n" + dimStyle.Render("space toggle • enter convert • esc cancel")
	return ui.ModalStyle.Width(60).Render(content)
}

func (m Model) renderReduceDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorGreen).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorSelBg)
	cyanStyle := lipgloss.NewStyle().Foreground(ui.ColorCyan).Background(ui.ColorBlack)
	redStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorRed).Background(ui.ColorBlack)
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorYellow).Background(ui.ColorBlack)

	content := titleStyle.Render("Split Mono Group") + "\n\n"
	content += dimStyle.Render("Each repo's ") + cyanStyle.Render(m.reduceWT.Branch) +
		dimStyle.Render(" worktree becomes:") + "\n\n"
	for i, r := range m.reduceRows {
		var fate string
		switch {
		case r.Delete:
			fate = redStyle.Render("[delete]") + dimStyle.Render(" worktree + local branch")
		case !r.CanDelete:
			fate = whiteStyle.Render("[keep]") + warnStyle.Render(" — "+r.StatusText+", must keep")
		default:
			fate = whiteStyle.Render("[keep]") + dimStyle.Render(" as single worktree · "+r.StatusText)
		}
		name := fmt.Sprintf("%-16s", r.Name)
		var line string
		if i == m.reduceCursor {
			line = selStyle.Render("  ▸ "+name) + " " + fate
		} else {
			line = "    " + whiteStyle.Render(name) + " " + fate
		}
		content += clipLine(line, 60) + "\n"
	}
	content += "\n" + dimStyle.Render("space keep/delete • enter split • esc cancel")
	return ui.ModalStyle.Width(64).Render(content)
}
