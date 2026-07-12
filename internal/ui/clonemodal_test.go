package ui

import (
	"strings"
	"testing"
	"time"

	"lts-revamp/internal/gh"

	"github.com/charmbracelet/lipgloss"
)

func cloneTestRepos(n int) []gh.Repo {
	var repos []gh.Repo
	for i := 0; i < n; i++ {
		repos = append(repos, gh.Repo{
			NameWithOwner: "org/repo-" + string(rune('a'+i)),
			UpdatedAt:     time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}
	return repos
}

// The mouse handler derives row positions from CloneListContentOffset —
// every visible row must land where its repo renders, including scrolled.
func TestCloneListOffsetMatchesRender(t *testing.T) {
	cases := []struct {
		name   string
		repos  int
		scroll int
		err    string
	}{
		{"plain", 5, 0, ""},
		{"scrolled", 20, 4, ""},
		{"with-error", 5, 0, "gh repo list failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewCloneModal(cloneTestRepos(tc.repos))
			m.Scroll = tc.scroll
			m.Err = tc.err

			// Place like the app does (centered dialog)
			w, h := 100, 40
			placed := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, m.View())
			lines := strings.Split(placed, "\n")
			modalTop := (h - lipgloss.Height(m.View())) / 2
			listStartY := modalTop + 2 + m.CloneListContentOffset()

			end := tc.scroll + CloneListMaxVisible
			if end > len(m.Filtered) {
				end = len(m.Filtered)
			}
			for i := tc.scroll; i < end; i++ {
				y := listStartY + (i - tc.scroll)
				if y >= len(lines) {
					t.Fatalf("row %d y=%d beyond screen", i, y)
				}
				row := stripANSITest(lines[y])
				if !strings.Contains(row, m.Filtered[i].NameWithOwner) {
					t.Errorf("row %d (y=%d) = %q, want %q", i, y, strings.TrimSpace(row), m.Filtered[i].NameWithOwner)
				}
			}
		})
	}
}

// Filtering keeps the cursor valid and matches on name and description.
func TestCloneFilter(t *testing.T) {
	repos := []gh.Repo{
		{NameWithOwner: "org/alpha"},
		{NameWithOwner: "org/beta", Description: "the alpha successor"},
		{NameWithOwner: "me/gamma"},
	}
	m := NewCloneModal(repos)
	m.Cursor = 2
	m.Input.SetValue("alpha")
	m.filter()
	if len(m.Filtered) != 2 {
		t.Fatalf("filtered = %d, want 2 (name + description match)", len(m.Filtered))
	}
	if m.Cursor >= len(m.Filtered) {
		t.Errorf("cursor %d not clamped to %d", m.Cursor, len(m.Filtered))
	}
}
