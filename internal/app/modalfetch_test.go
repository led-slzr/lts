package app

import (
	"testing"

	"lts-revamp/internal/git"
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBranchListRefreshesFromFetch(t *testing.T) {
	mm := ui.NewModal([]git.Repo{{Name: "core", Path: "/x/core"}}, "/x", func(string) string { return "pnpm" }, true)
	mm.Selected[0] = true
	mm.Step = ui.ModalEnterBranch
	mm.FetchingBranches = true

	// Fetched result for the current selection replaces the list
	mm, _ = mm.Update(ui.ModalBranchesFetchedMsg{
		Key:      "/x/core",
		Branches: []git.BranchInfo{{Name: "feature/from-remote", IsLocal: false}},
	})
	if mm.FetchingBranches {
		t.Error("expected FetchingBranches to clear")
	}
	if len(mm.AllBranches) != 1 || mm.AllBranches[0].Name != "feature/from-remote" {
		t.Errorf("expected refreshed branch list, got %+v", mm.AllBranches)
	}

	// A stale result keyed to a different selection is ignored
	mm, _ = mm.Update(ui.ModalBranchesFetchedMsg{
		Key:      "/x/other",
		Branches: []git.BranchInfo{{Name: "wrong/list"}},
	})
	if len(mm.AllBranches) != 1 || mm.AllBranches[0].Name != "feature/from-remote" {
		t.Errorf("stale fetch result overwrote list: %+v", mm.AllBranches)
	}
}

func TestEnteringBranchStepStartsFetch(t *testing.T) {
	mm := ui.NewModal([]git.Repo{{Name: "core", Path: "/x/core"}}, "/x", func(string) string { return "pnpm" }, true)
	mm.Selected[0] = true
	var cmd tea.Cmd
	mm, cmd = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if mm.Step != ui.ModalEnterBranch {
		t.Fatalf("expected branch step, got %v", mm.Step)
	}
	if !mm.FetchingBranches {
		t.Error("expected FetchingBranches to be set")
	}
	if cmd == nil {
		t.Error("expected a fetch command to be returned")
	}
}
