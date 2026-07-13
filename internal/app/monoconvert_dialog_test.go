package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lts-revamp/internal/git"
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConvertDialogDetectsAdoptablePartners(t *testing.T) {
	m := testModel()
	m.repos = []git.Repo{
		{Name: "core", Path: "/x/core", Worktrees: []git.Worktree{{Branch: "feat/x", Path: "/x/core-lts/core-feat-x"}}},
		{Name: "erp", Path: "/x/erp", Worktrees: []git.Worktree{{Branch: "feat/x", Path: "/x/erp-lts/erp-feat-x"}}},
		{Name: "goforms", Path: "/x/goforms"},
		{Name: "hidden", Path: "/x/hidden", Hidden: true},
	}
	m2, _ := startConvert(m, m.repos[0], m.repos[0].Worktrees[0])
	if !m2.convertActive || len(m2.convertChoices) != 2 {
		t.Fatalf("expected 2 choices (hidden excluded), got %+v", m2.convertChoices)
	}
	byName := map[string]convertChoice{}
	for _, c := range m2.convertChoices {
		byName[c.Name] = c
	}
	if byName["erp"].AdoptPath == "" {
		t.Error("erp has the same branch — must be an adoption")
	}
	if byName["goforms"].AdoptPath != "" {
		t.Error("goforms has no such branch — must be a fresh create")
	}
}

func TestConvertConfirmLocksAllParticipants(t *testing.T) {
	m := testModel()
	m.convertActive = true
	m.convertRepo = m.repos[0] // core
	m.convertWT = git.Worktree{Branch: "feat/x", Path: "/x/core-lts/core-feat-x"}
	m.convertChoices = []convertChoice{{Name: "goforms", Selected: true}}

	m2, cmd := confirmConvert(m)
	if cmd == nil || m2.convertActive {
		t.Fatal("confirm should close the dialog and dispatch")
	}
	for _, name := range []string{"core", "goforms"} {
		if _, locked := m2.busy[name]; !locked {
			t.Errorf("%s must be locked during conversion", name)
		}
	}
	// Enter with nothing selected keeps the dialog open, locks nothing.
	m3 := testModel()
	m3.convertActive = true
	m3.convertRepo = m3.repos[0]
	m3.convertChoices = []convertChoice{{Name: "goforms"}}
	m4, cmd := confirmConvert(m3)
	if cmd != nil || !m4.convertActive || m4.anyBusy() {
		t.Fatal("empty selection must be a no-op")
	}
}

func TestReduceRowsMatchLongestRepoName(t *testing.T) {
	// Fabricated branch subdir: "core" and "core-ui" both in the group —
	// core-ui-feat-x must match core-ui, never core.
	dir := t.TempDir()
	for _, wt := range []string{"core-feat-x", "core-ui-feat-x"} {
		os.MkdirAll(filepath.Join(dir, wt), 0755)
		os.WriteFile(filepath.Join(dir, wt, ".git"), []byte("gitdir: /nowhere\n"), 0644)
	}
	repo := git.Repo{Name: "core-core-ui", IsMonorepo: true, RepoNames: []string{"core", "core-ui"}}
	rows := constituentRows(repo, git.Worktree{Branch: "feat/x", Path: dir}, func(string) string { return "main" })
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %+v", rows)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.Name] = filepath.Base(r.WtPath)
	}
	if got["core-ui"] != "core-ui-feat-x" || got["core"] != "core-feat-x" {
		t.Fatalf("prefix matching wrong: %v", got)
	}
}

func TestReduceKeyGatingAndConfirm(t *testing.T) {
	m := testModel()
	m.reduceActive = true
	m.reduceRepo = m.repos[2] // core-goforms mono card
	m.reduceWT = git.Worktree{Branch: "feat/x", Path: "/x/core-goforms-lts/feat-x"}
	m.reduceRows = []reduceRow{
		{Name: "core", CanDelete: false, StatusText: "3 changed"},
		{Name: "goforms", CanDelete: true, StatusText: "✓ clean"},
	}

	// Space on a dangerous row is inert; on a safe row it toggles.
	m2, _ := handleReduceKey(m, tea.KeyMsg{Type: tea.KeySpace})
	if m2.reduceRows[0].Delete {
		t.Fatal("dangerous constituent must be locked to keep")
	}
	m2.reduceCursor = 1
	m3, _ := handleReduceKey(m2, tea.KeyMsg{Type: tea.KeySpace})
	if !m3.reduceRows[1].Delete {
		t.Fatal("safe constituent should toggle to delete")
	}

	m4, cmd := handleReduceKey(m3, key("enter"))
	if cmd == nil || m4.reduceActive {
		t.Fatal("enter should dispatch the split")
	}
	for _, name := range []string{"core", "goforms"} {
		if _, locked := m4.busy[name]; !locked {
			t.Errorf("%s must be locked during the split", name)
		}
	}
	if !strings.Contains(m4.busy["core"], "Splitting") {
		t.Errorf("lock label: %q", m4.busy["core"])
	}
}

func TestMonoMenuEntries(t *testing.T) {
	items := ui.WorktreeContextItems(false, false, false, true)
	found := false
	for _, it := range items {
		if it.Action == ui.BtnConvertMono {
			found = true
		}
		if it.Action == ui.BtnReduceMono {
			t.Error("plain worktree must not offer Split")
		}
	}
	if !found {
		t.Error("plain worktree with partners should offer Add to Mono Group")
	}
	items = ui.WorktreeContextItems(true, false, false, false)
	found = false
	for _, it := range items {
		if it.Action == ui.BtnReduceMono {
			found = true
		}
		if it.Action == ui.BtnConvertMono {
			t.Error("mono worktree must not offer Add to Mono Group")
		}
	}
	if !found {
		t.Error("mono worktree should offer Split Mono Group")
	}
}
