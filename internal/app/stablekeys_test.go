package app

import (
	"strings"
	"testing"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"

	tea "github.com/charmbracelet/bubbletea"
)

// Dialogs act on the target snapshotted at open time — a repo-list reload
// (which can reorder or shrink m.repos) while a dialog is open must not
// change what the dialog acts on.
func TestDeleteDialogSurvivesRepoReload(t *testing.T) {
	m := NewModel(config.Config{Global: config.DefaultGlobal(), WorkDir: "/x"})
	m.deleteConfirmActive = true
	m.deleteRepo = git.Repo{Name: "core", Path: "/x/core"}
	m.deleteWT = git.Worktree{Branch: "feat/x", Path: "/x/core-lts/feat-x", Status: git.StatusClean}
	m.deleteLocalBranch = true

	// Simulate a background reload that emptied the repo list entirely
	m.repos = nil

	// The dialog still renders the snapshotted target
	dialog := stripANSI(m.renderDeleteConfirmDialog())
	if !contains(dialog, "feat/x") {
		t.Errorf("dialog lost its target after reload:\n%s", dialog)
	}

	// Confirming still produces a delete command (not a silent no-op)
	m2, cmd := confirmDelete(m)
	if cmd == nil {
		t.Error("expected a delete command from the snapshotted target")
	}
	if m2.deleteConfirmActive {
		t.Error("expected dialog to close")
	}
}

func TestRenameDialogSurvivesRepoReload(t *testing.T) {
	m := NewModel(config.Config{Global: config.DefaultGlobal(), WorkDir: "/x"})
	m.renameActive = true
	m.renameRepo = git.Repo{Name: "core", Path: "/x/core", IsMonorepo: false}
	m.renameWT = git.Worktree{Branch: "feat/old", Path: "/x/core-lts/feat-old", Status: git.StatusClean}
	m.renameIsBasis = false
	m.repos = nil

	dialog := stripANSI(m.renderRenameDialog())
	if !contains(dialog, "feat/old") {
		t.Errorf("rename dialog lost its target after reload:\n%s", dialog)
	}

	m.renameInput.SetValue("feat/new")
	m2, cmd := handleRenameKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("expected a rename command from the snapshotted target")
	}
	if m2.renameActive {
		t.Error("expected rename dialog to close")
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
