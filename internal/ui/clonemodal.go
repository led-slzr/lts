package ui

import (
	"fmt"
	"strings"

	"lts-revamp/internal/gh"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CloneModel is the "clone a repo from GitHub" browser: a filterable list of
// every repo the authenticated user can see, newest activity first.
type CloneModel struct {
	Active   bool
	Input    textinput.Model
	All      []gh.Repo
	Filtered []gh.Repo
	Cursor   int
	Scroll   int
	Hovered  int // -1 = none
	Loading  bool
	Err      string
}

// CloneSelectedMsg asks the app to clone the chosen repo.
type CloneSelectedMsg struct {
	NameWithOwner string
}

// CloneCancelMsg closes the browser.
type CloneCancelMsg struct{}

// CloneListMaxVisible is the repo-list window size (shared with mouse hit-testing).
const CloneListMaxVisible = 12

func NewCloneModal(cached []gh.Repo) CloneModel {
	ti := textinput.New()
	ti.Placeholder = "filter repos (owner/name)"
	ti.CharLimit = 100
	ti.Width = 50
	ti.Focus()
	m := CloneModel{
		Active:  true,
		Input:   ti,
		All:     cached,
		Hovered: -1,
		Loading: len(cached) == 0,
	}
	m.filter()
	return m
}

// SetRepos replaces the list (initial fetch or background refresh).
func (m *CloneModel) SetRepos(repos []gh.Repo, err error) {
	m.Loading = false
	if err != nil {
		m.Err = err.Error()
		return
	}
	m.Err = ""
	m.All = repos
	m.filter()
}

func (m *CloneModel) filter() {
	query := strings.ToLower(strings.TrimSpace(m.Input.Value()))
	if query == "" {
		m.Filtered = m.All
	} else {
		m.Filtered = nil
		for _, r := range m.All {
			if strings.Contains(strings.ToLower(r.NameWithOwner), query) ||
				strings.Contains(strings.ToLower(r.Description), query) {
				m.Filtered = append(m.Filtered, r)
			}
		}
	}
	if m.Cursor >= len(m.Filtered) {
		m.Cursor = len(m.Filtered) - 1
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	if m.Scroll > len(m.Filtered) {
		m.Scroll = 0
	}
}

func (m CloneModel) Update(msg tea.Msg) (CloneModel, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc":
		m.Active = false
		return m, func() tea.Msg { return CloneCancelMsg{} }
	case "enter":
		if m.Cursor >= 0 && m.Cursor < len(m.Filtered) {
			target := m.Filtered[m.Cursor].NameWithOwner
			m.Active = false
			return m, func() tea.Msg { return CloneSelectedMsg{NameWithOwner: target} }
		}
		return m, nil
	case "up":
		if m.Cursor > 0 {
			m.Cursor--
			m.ensureVisible()
		}
		return m, nil
	case "down":
		if m.Cursor < len(m.Filtered)-1 {
			m.Cursor++
			m.ensureVisible()
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.Input, cmd = m.Input.Update(msg)
	m.filter()
	return m, cmd
}

func (m *CloneModel) ensureVisible() {
	if m.Cursor < m.Scroll {
		m.Scroll = m.Cursor
	}
	if m.Cursor >= m.Scroll+CloneListMaxVisible {
		m.Scroll = m.Cursor - CloneListMaxVisible + 1
	}
}

// CloneListContentOffset returns the content lines before the repo list —
// mouse hit-testing depends on matching the render below.
func (m *CloneModel) CloneListContentOffset() int {
	offset := 2 // title + blank
	offset += 2 // input + blank
	if m.Err != "" {
		offset += 2
	}
	if m.Scroll > 0 {
		offset++ // "↑ more"
	}
	return offset
}

// View renders the browser (unplaced).
func (m CloneModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Background(ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Background(ColorBlack)
	errStyle := lipgloss.NewStyle().Foreground(ColorRed).Background(ColorBlack)
	hoverStyle := lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorDarkGreen).Bold(true)
	privStyle := lipgloss.NewStyle().Foreground(ColorYellow).Background(ColorBlack)
	dateStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack).Italic(true)

	content := titleStyle.Render("Clone a Repository") + "\n\n"
	content += m.Input.View() + "\n"
	if m.Err != "" {
		content += "\n" + errStyle.Render(truncatePlain(m.Err, 60)) + "\n"
	}
	content += "\n"

	switch {
	case m.Loading && len(m.All) == 0:
		content += dimStyle.Render("  Loading repositories from GitHub...")
	case len(m.Filtered) == 0:
		content += dimStyle.Render("  No matching repositories")
	default:
		if m.Scroll > 0 {
			content += dimStyle.Render(fmt.Sprintf("  ↑ %d more", m.Scroll)) + "\n"
		}
		end := m.Scroll + CloneListMaxVisible
		if end > len(m.Filtered) {
			end = len(m.Filtered)
		}
		for i := m.Scroll; i < end; i++ {
			r := m.Filtered[i]
			owner, name := r.NameWithOwner, ""
			if idx := strings.IndexByte(r.NameWithOwner, '/'); idx >= 0 {
				owner, name = r.NameWithOwner[:idx+1], r.NameWithOwner[idx+1:]
			}
			nameCell := padCell(owner+name, 40)
			var row string
			if i == m.Cursor || i == m.Hovered {
				row = hoverStyle.Render("▸ " + nameCell)
			} else {
				row = "  " + dimStyle.Render(owner) + whiteStyle.Render(name) +
					strings.Repeat(" ", maxInt(0, 40-lipgloss.Width(owner+name)))
			}
			tag := "      "
			if r.IsPrivate {
				tag = privStyle.Render("priv") + "  "
			}
			row += tag + dateStyle.Render(formatAge(r.UpdatedAt.Unix()))
			content += truncate(row, 62) + "\n"
		}
		if end < len(m.Filtered) {
			content += dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.Filtered)-end)) + "\n"
		}
		if m.Loading {
			content += dimStyle.Render("  (refreshing...)") + "\n"
		}
	}

	content += "\n" + dimStyle.Render("↑/↓ navigate • enter clone • esc cancel")
	return ModalStyle.Width(68).Render(content)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
