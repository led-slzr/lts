package app

import (
	"strings"
	"testing"

	"lts-revamp/internal/git"
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// ConfirmRowsOffset must point at the first "→" plan row for both single and
// multi-repo plans (mouse hit-testing depends on it).
func TestConfirmRowsOffsetMatchesRender(t *testing.T) {
	cases := []struct {
		name  string
		repos []string
	}{
		{"single", []string{"core"}},
		{"multi", []string{"core", "goforms"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repos := make([]git.Repo, len(tc.repos))
			for i, n := range tc.repos {
				repos[i] = git.Repo{Name: n, Path: "/x/" + n}
			}
			mm := ui.NewModal(repos, "/x", func(string) string { return "pnpm" }, true)
			for i := range tc.repos {
				mm.Selected[i] = true
			}
			mm.Input.SetValue("feat/test")
			// enter on branch step computes the plan and moves to confirm
			mm.Step = ui.ModalEnterBranch
			mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if mm.Step != ui.ModalConfirm {
				t.Fatalf("expected confirm step, got %v (error %q)", mm.Step, mm.Error)
			}

			view := mm.View(100, 40)
			t.Log("\n" + view)
			lines := strings.Split(view, "\n")
			// content starts after border(1)+padding(1)
			rowLine := 2 + mm.ConfirmRowsOffset()
			if rowLine >= len(lines) {
				t.Fatalf("row line %d out of range (%d lines)", rowLine, len(lines))
			}
			row := stripANSI(lines[rowLine])
			if !strings.Contains(row, "→") {
				t.Errorf("expected plan row at modal line %d, got %q", rowLine, row)
			}
			if !strings.Contains(row, "pnpm install") {
				t.Errorf("expected install toggle on row, got %q", row)
			}
			// default comes from settings (true here)
			if !strings.Contains(row, "[✓]") {
				t.Errorf("expected default-enabled checkbox, got %q", row)
			}

			// toggle via space flips the focused (first) row
			mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			view = mm.View(100, 40)
			lines = strings.Split(view, "\n")
			if !strings.Contains(stripANSI(lines[rowLine]), "[ ]") {
				t.Errorf("expected checkbox off after space, got %q", stripANSI(lines[rowLine]))
			}
			// InstallDeps map carries per-repo values keyed by repo name
			if len(mm.InstallDeps) != len(tc.repos) {
				t.Errorf("InstallDeps has %d entries, want %d", len(mm.InstallDeps), len(tc.repos))
			}
		})
	}
}

// Without a configured package manager the confirm step shows no toggles.
func TestConfirmNoTogglesWithoutPackageManager(t *testing.T) {
	mm := ui.NewModal([]git.Repo{{Name: "core", Path: "/x/core"}}, "/x", nil, true)
	mm.Selected[0] = true
	mm.Input.SetValue("feat/test")
	mm.Step = ui.ModalEnterBranch
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := stripANSI(mm.View(100, 40))
	if strings.Contains(view, "install") {
		t.Errorf("expected no install toggles, got:\n%s", view)
	}
}
