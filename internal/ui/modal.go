package ui

import (
	"fmt"
	"lts-revamp/internal/git"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ModalStep int

const (
	ModalSelectRepos ModalStep = iota
	ModalEnterBranch
	ModalConfirm
)

type ModalModel struct {
	Active      bool
	Step        ModalStep
	Repos       []git.Repo
	Selected    map[int]bool // multi-select: repo indices
	CursorIdx   int
	Input       textinput.Model
	Error       string
	Branch      string

	// Pre-computed plan info for confirmation
	PlanSingle  bool   // true if single-repo mode
	PlanLTSDir  string // e.g. "core-lts" or "core-erp-ui-lts"
	PlanWTNames []string // planned worktree folder names
}

// Messages
type ModalCreateMsg struct {
	RepoNames []string // selected repo names
	Branch    string
}

type ModalCancelMsg struct{}

func NewModal(repos []git.Repo) ModalModel {
	ti := textinput.New()
	ti.Placeholder = "e.g. fix/login-bug"
	ti.CharLimit = 100
	ti.Width = 40

	return ModalModel{
		Active:    true,
		Step:      ModalSelectRepos,
		Repos:     repos,
		Selected:  make(map[int]bool),
		CursorIdx: 0,
		Input:     ti,
	}
}

func (m ModalModel) Update(msg tea.Msg) (ModalModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.Active = false
			return m, func() tea.Msg { return ModalCancelMsg{} }

		case "enter":
			return m.handleEnter()

		case "up", "k":
			if m.Step == ModalSelectRepos && m.CursorIdx > 0 {
				m.CursorIdx--
			}
		case "down", "j":
			if m.Step == ModalSelectRepos && m.CursorIdx < len(m.Repos)-1 {
				m.CursorIdx++
			}
		case " ", "tab":
			// Toggle selection in multi-select
			if m.Step == ModalSelectRepos {
				if m.Selected[m.CursorIdx] {
					delete(m.Selected, m.CursorIdx)
				} else {
					m.Selected[m.CursorIdx] = true
				}
			}
		case "backspace":
			if m.Step == ModalEnterBranch {
				m.Error = ""
			}
		}
	}

	if m.Step == ModalEnterBranch {
		var cmd tea.Cmd
		m.Input, cmd = m.Input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m ModalModel) handleEnter() (ModalModel, tea.Cmd) {
	switch m.Step {
	case ModalSelectRepos:
		// If nothing explicitly selected, select the one at cursor
		if len(m.Selected) == 0 {
			m.Selected[m.CursorIdx] = true
		}
		m.Step = ModalEnterBranch
		m.Input.Focus()
		return m, textinput.Blink

	case ModalEnterBranch:
		branch := strings.TrimSpace(m.Input.Value())
		if err := git.ValidateBranchName(branch); err != nil {
			m.Error = err.Error()
			return m, nil
		}
		m.Branch = branch
		m.computePlan()
		m.Step = ModalConfirm
		return m, nil

	case ModalConfirm:
		m.Active = false
		var repoNames []string
		for idx := range m.Selected {
			repoNames = append(repoNames, m.Repos[idx].Name)
		}
		branch := m.Branch
		return m, func() tea.Msg {
			return ModalCreateMsg{RepoNames: repoNames, Branch: branch}
		}
	}
	return m, nil
}

func (m *ModalModel) computePlan() {
	var selectedNames []string
	for idx := range m.Selected {
		selectedNames = append(selectedNames, m.Repos[idx].Name)
	}

	suffix := git.ExtractSuffix(m.Branch)
	safeSuffix := git.SanitizeForFilename(suffix)

	if len(selectedNames) == 1 {
		m.PlanSingle = true
		repo := selectedNames[0]
		m.PlanLTSDir = repo + "-lts"
		m.PlanWTNames = []string{repo + "-" + safeSuffix}
	} else {
		m.PlanSingle = false
		sorted := make([]string, len(selectedNames))
		copy(sorted, selectedNames)
		sort.Strings(sorted)
		ltsPrefix := strings.Join(sorted, "-")
		m.PlanLTSDir = ltsPrefix + "-lts"
		branchSubdir := ltsPrefix + "-" + safeSuffix
		var names []string
		for _, repo := range sorted {
			names = append(names, branchSubdir+"/"+repo+"-"+safeSuffix)
		}
		m.PlanWTNames = names
	}
}

func (m ModalModel) View(width, height int) string {
	if !m.Active {
		return ""
	}

	var content string

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorGreen).
		Background(ColorBlack)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack)

	whiteStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorWhite).
		Background(ColorBlack)

	cyanStyle := lipgloss.NewStyle().
		Foreground(ColorCyan).
		Background(ColorBlack)

	errorStyle := lipgloss.NewStyle().
		Foreground(ColorRed).
		Background(ColorBlack)

	switch m.Step {
	case ModalSelectRepos:
		content = titleStyle.Render("Create Worktree") + "\n\n"
		content += dimStyle.Render("Select repository (space/tab to multi-select):") + "\n\n"
		for i, repo := range m.Repos {
			isSelected := m.Selected[i]
			isCursor := i == m.CursorIdx

			checkbox := "[ ]"
			if isSelected {
				checkbox = "[✓]"
			}

			if isCursor {
				if isSelected {
					content += whiteStyle.Render("▸ "+checkbox+" "+repo.Name) + "\n"
				} else {
					content += whiteStyle.Render("▸ "+checkbox+" "+repo.Name) + "\n"
				}
			} else {
				if isSelected {
					content += cyanStyle.Render("  "+checkbox+" "+repo.Name) + "\n"
				} else {
					content += dimStyle.Render("  "+checkbox+" "+repo.Name) + "\n"
				}
			}
		}
		selectedCount := len(m.Selected)
		hint := "↑/↓ navigate • space select • enter confirm"
		if selectedCount > 1 {
			hint = fmt.Sprintf("%d repos selected • ", selectedCount) + hint
		}
		content += "\n" + dimStyle.Render(hint) + "\n"
		content += dimStyle.Render("esc cancel")

	case ModalEnterBranch:
		content = titleStyle.Render("Create Worktree") + "\n\n"

		// Show selected repos
		var selectedNames []string
		for idx := range m.Selected {
			selectedNames = append(selectedNames, m.Repos[idx].Name)
		}
		if len(selectedNames) == 1 {
			content += dimStyle.Render("Repository: ") + whiteStyle.Render(selectedNames[0]) + "\n"
		} else {
			content += dimStyle.Render(fmt.Sprintf("Repositories (%d): ", len(selectedNames)))
			content += cyanStyle.Render(strings.Join(selectedNames, ", ")) + "\n"
			content += dimStyle.Render("Same branch will be created across all repos") + "\n"
		}

		content += "\n" + dimStyle.Render("Enter branch name:") + "\n\n"
		content += m.Input.View() + "\n"
		if m.Error != "" {
			content += "\n" + errorStyle.Render(m.Error)
		}
		content += "\n" + dimStyle.Render("enter confirm • esc cancel")

	case ModalConfirm:
		content = titleStyle.Render("Create Worktree") + "\n\n"
		content += dimStyle.Render("Confirm creation:") + "\n\n"

		if m.PlanSingle {
			content += dimStyle.Render("Directory: ") + whiteStyle.Render(m.PlanLTSDir+"/") + "\n"
		} else {
			content += dimStyle.Render("Directory: ") + whiteStyle.Render(m.PlanLTSDir+"/") + "\n"
			content += dimStyle.Render("Mode:      ") + cyanStyle.Render("Multi-repo (monorepo-like)") + "\n"
		}
		content += dimStyle.Render("Branch:    ") + cyanStyle.Render(m.Branch) + "\n\n"

		for _, name := range m.PlanWTNames {
			content += dimStyle.Render("  → ") + whiteStyle.Render(name) + "\n"
		}

		content += "\n" + dimStyle.Render("enter create • esc cancel")
	}

	modal := ModalStyle.Render(content)

	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}
