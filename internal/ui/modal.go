package ui

import (
	"fmt"
	"lts-revamp/internal/git"
	"path/filepath"
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

	// Branch suggestions (populated when entering ModalEnterBranch)
	AllBranches      []git.BranchInfo // all branches from selected repos
	FilteredBranches []git.BranchInfo // filtered by current input text
	BranchScroll     int              // scroll offset in branch list
	BranchHovered    int              // hovered branch index (-1 = none)
	ScrollbarHovered bool             // true when mouse is over the scrollbar thumb
	ScriptDir        string           // for loading branches
}

// Messages
type ModalCreateMsg struct {
	RepoNames []string // selected repo names
	Branch    string
}

type ModalCancelMsg struct{}

func NewModal(repos []git.Repo, scriptDir string) ModalModel {
	ti := textinput.New()
	ti.Placeholder = "type or pick a branch below"
	ti.CharLimit = 100
	ti.Width = 50

	return ModalModel{
		Active:        true,
		Step:          ModalSelectRepos,
		Repos:         repos,
		Selected:      make(map[int]bool),
		CursorIdx:     0,
		Input:         ti,
		BranchHovered: -1,
		ScriptDir:     scriptDir,
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
		// Only forward real key messages to the text input.
		// Mouse events can leak as raw ANSI/SGR sequences through tea.KeyMsg.
		if keyMsg, isKey := msg.(tea.KeyMsg); isKey {
			// Allow known control keys
			switch keyMsg.Type {
			case tea.KeyBackspace, tea.KeyDelete, tea.KeyLeft, tea.KeyRight,
				tea.KeyHome, tea.KeyEnd, tea.KeyCtrlA, tea.KeyCtrlE,
				tea.KeyCtrlW, tea.KeyCtrlU, tea.KeyCtrlK:
				var cmd tea.Cmd
				m.Input, cmd = m.Input.Update(msg)
				m.FilterBranches()
				return m, cmd
			case tea.KeyRunes:
				// Only allow characters valid in git branch names.
				// This blocks mouse escape sequences that leak as runes
				// (containing [, <, >, ;, digits mixed with M, etc.)
				for _, r := range keyMsg.Runes {
					if !isValidBranchChar(r) {
						return m, nil
					}
				}
				var cmd tea.Cmd
				m.Input, cmd = m.Input.Update(msg)
				m.FilterBranches()
				return m, cmd
			}
			// Block everything else (escape sequences, mouse leaks, etc.)
			return m, nil
		}
		return m, nil
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
		m.loadBranches()
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

// isValidBranchChar returns true if the rune is valid in a git branch name.
// This excludes characters used in mouse escape sequences ([, <, >, ;).
func isValidBranchChar(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '/', '-', '_', '.', '+', '@', '{', '}', '#', '%', '!', '~':
		return true
	}
	return false
}

func (m *ModalModel) loadBranches() {
	var repoPaths []string
	for idx := range m.Selected {
		repo := m.Repos[idx]
		if repo.Path != "" {
			repoPaths = append(repoPaths, repo.Path)
		} else if repo.IsMonorepo {
			// For monorepo, use constituent repo paths
			for _, rn := range repo.RepoNames {
				p := filepath.Join(m.ScriptDir, rn)
				repoPaths = append(repoPaths, p)
			}
		}
	}
	m.AllBranches = git.GetBranchesWithDates(repoPaths)
	m.FilterBranches()
}

func (m *ModalModel) FilterBranches() {
	query := strings.ToLower(strings.TrimSpace(m.Input.Value()))
	if query == "" {
		m.FilteredBranches = m.AllBranches
	} else {
		m.FilteredBranches = nil
		for _, b := range m.AllBranches {
			if strings.Contains(strings.ToLower(b.Name), query) {
				m.FilteredBranches = append(m.FilteredBranches, b)
			}
		}
	}
	// Reset scroll if filter changed
	if m.BranchScroll > len(m.FilteredBranches) {
		m.BranchScroll = 0
	}
}

// BranchListMaxVisible is the max number of branch suggestions shown at once.
const BranchListMaxVisible = 8

// BranchListContentOffset returns the number of content lines before the branch list starts
// inside the ModalEnterBranch view. This must match the rendering order.
func (m *ModalModel) BranchListContentOffset() int {
	offset := 0
	offset += 2 // title + empty
	selectedCount := len(m.Selected)
	offset += 1 // "Repository: xxx" or "Repositories (N): ..."
	offset += 2 // empty + "Branch name:"
	offset += 2 // empty + input field
	if m.Error != "" {
		offset += 2 // empty + error
	}
	offset += 1 // empty line before branch list
	// Scroll-up indicator
	if m.BranchScroll > 0 {
		offset += 1
	}
	_ = selectedCount
	return offset
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

// View returns the rendered modal box (not placed on screen).
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
		}

		content += "\n" + dimStyle.Render("Branch name:") + "\n\n"
		content += m.Input.View() + "\n"
		if m.Error != "" {
			content += "\n" + errorStyle.Render(m.Error)
		}

		// Branch suggestions list
		if len(m.FilteredBranches) > 0 {
			content += "\n"

			localTag := lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack)
			remoteTag := lipgloss.NewStyle().Foreground(ColorYellow).Background(ColorBlack)
			dateStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack).Italic(true)
			hoverStyle := lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorDarkGreen).Bold(true)
			normalStyle := lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorBlack)
			normalDimStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)

			visible := BranchListMaxVisible
			total := len(m.FilteredBranches)
			if total < visible {
				visible = total
			}
			endIdx := m.BranchScroll + visible
			if endIdx > total {
				endIdx = total
			}

			// Scroll indicator top
			if m.BranchScroll > 0 {
				content += dimStyle.Render(fmt.Sprintf("  ↑ %d more", m.BranchScroll)) + "\n"
			}

			// Layout: branch list column + scrollbar column (if scrollable)
			// Full content width: modal(60) - border(2) - padding(4) = 54
			hasScrollbar := total > visible
			fullW := 54
			listW := fullW
			if hasScrollbar {
				listW = fullW - 3 // 1 space gap + 1 scrollbar + 1 padding
			}

			// Build branch list rows
			var listLines []string
			for i := m.BranchScroll; i < endIdx; i++ {
				b := m.FilteredBranches[i]
				isHovered := i == m.BranchHovered

				var tag string
				if b.IsLocal {
					tag = localTag.Render("local")
				} else {
					tag = remoteTag.Render("remote")
				}
				date := dateStyle.Render("(" + b.Date + ")")
				suffix := " " + tag + " " + date
				suffixW := lipgloss.Width(suffix)

				prefix := "  "
				if isHovered {
					prefix = "▸ "
				}
				maxNameW := listW - len(prefix) - suffixW
				name := b.Name
				if len(name) > maxNameW && maxNameW > 3 {
					name = name[:maxNameW-1] + "…"
				}

				var row string
				if isHovered {
					row = hoverStyle.Render(prefix+name) + suffix
				} else {
					ns := normalStyle
					if !b.IsLocal {
						ns = normalDimStyle
					}
					row = ns.Render(prefix+name) + suffix
				}

				// Pad to fixed list width
				rowW := lipgloss.Width(row)
				if rowW < listW {
					row += strings.Repeat(" ", listW-rowW)
				}
				listLines = append(listLines, row)
			}

			if hasScrollbar {
				// Build scrollbar column
				thumbLen := visible * visible / total
				if thumbLen < 1 {
					thumbLen = 1
				}
				if thumbLen > visible {
					thumbLen = visible
				}
				maxScroll := total - visible
				thumbStart := 0
				trackSpace := visible - thumbLen
				if maxScroll > 0 && trackSpace > 0 {
					thumbStart = m.BranchScroll * trackSpace / maxScroll
				}

				trackChar := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack).Render("│")
				thumbChar := lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack).Bold(true).Render("┃")
				thumbHoverChar := lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorDarkGreen).Bold(true).Render("█")

				var scrollLines []string
				for idx := 0; idx < visible; idx++ {
					if idx >= thumbStart && idx < thumbStart+thumbLen {
						if m.ScrollbarHovered {
							scrollLines = append(scrollLines, thumbHoverChar)
						} else {
							scrollLines = append(scrollLines, thumbChar)
						}
					} else {
						scrollLines = append(scrollLines, trackChar)
					}
				}

				listCol := strings.Join(listLines, "\n")
				scrollCol := strings.Join(scrollLines, "\n")
				content += lipgloss.JoinHorizontal(lipgloss.Top, listCol, " ", scrollCol) + "\n"
			} else {
				content += strings.Join(listLines, "\n") + "\n"
			}

			// Branch count
			if total > 0 {
				countInfo := fmt.Sprintf("%d/%d", min(endIdx, total), total)
				content += dimStyle.Render("  "+countInfo+" branches") + "\n"
			}
		} else if len(m.AllBranches) > 0 {
			content += "\n" + dimStyle.Render("  No matching branches") + "\n"
		}

		content += "\n" + dimStyle.Render("enter confirm • click branch to fill • esc cancel")

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

	style := ModalStyle
	if m.Step == ModalEnterBranch {
		style = style.Width(60) // wider for branch list with dates
	}
	return style.Render(content)
}

// ViewPlaced renders the modal centered on screen.
func (m ModalModel) ViewPlaced(width, height int) string {
	modal := m.View(width, height)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
