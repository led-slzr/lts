package app

import (
	"strings"
	"testing"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func testModel() Model {
	m := NewModel(config.Config{Global: config.DefaultGlobal(), Local: map[string]config.RepoLocalConfig{}, WorkDir: "/x"})
	m.width, m.height = 100, 40
	m.initialLoad = false
	m.repos = []git.Repo{
		{Name: "core", Path: "/x/core"},
		{Name: "goforms", Path: "/x/goforms"},
		{Name: "core-goforms", IsMonorepo: true, RepoNames: []string{"core", "goforms"}},
	}
	return m
}

func TestLockSetAndRepoBusy(t *testing.T) {
	m := testModel()
	mono := m.repos[2]

	if got := lockSet(m.repos[0]); len(got) != 1 || got[0] != "core" {
		t.Errorf("lockSet(core) = %v", got)
	}
	if got := lockSet(mono); len(got) != 2 {
		t.Errorf("lockSet(mono) = %v", got)
	}

	// Locking a constituent makes the monorepo card busy too
	m.beginOp("Rebasing feat/x...", "core")
	if !m.repoBusy(m.repos[0]) {
		t.Error("core should be busy")
	}
	if m.repoBusy(m.repos[1]) {
		t.Error("goforms should be free")
	}
	if !m.repoBusy(mono) {
		t.Error("monorepo card should be busy while a constituent is locked")
	}

	busyCards := m.busyCardNames()
	if !busyCards["core"] || !busyCards["core-goforms"] || busyCards["goforms"] {
		t.Errorf("busyCardNames = %v", busyCards)
	}
}

func TestConcurrentOpsReleaseOnlyTheirLocks(t *testing.T) {
	m := testModel()
	m.beginOp("Creating feat/a...", "core")
	m.beginOp("Refreshing goforms...", "goforms")
	if len(m.busy) != 2 {
		t.Fatalf("expected 2 locks, got %v", m.busy)
	}

	// goforms finishes — only its lock is released
	updated, _ := m.Update(SingleRefreshDoneMsg{RepoName: "goforms", Locked: []string{"goforms"}})
	m = updated.(Model)
	if _, ok := m.busy["goforms"]; ok {
		t.Error("goforms lock should be released")
	}
	if _, ok := m.busy["core"]; !ok {
		t.Error("core lock must survive the other op finishing")
	}

	updated, _ = m.Update(CreateDoneMsg{Branch: "feat/a", Locked: []string{"core"}, Err: nil})
	m = updated.(Model)
	if m.anyBusy() {
		t.Errorf("all locks should be released, got %v", m.busy)
	}
}

func TestCreateConfirmRejectsBusyRepo(t *testing.T) {
	m := testModel()
	m.beginOp("Rebasing feat/x...", "core")

	updated, _ := m.Update(ui.ModalCreateMsg{RepoNames: []string{"core", "goforms"}, Branch: "feat/new"})
	m = updated.(Model)
	if !strings.Contains(m.statusMsg, "busy") {
		t.Errorf("expected busy rejection, status: %q", m.statusMsg)
	}
	if _, ok := m.busy["goforms"]; ok {
		t.Error("rejected create must not lock goforms")
	}

	// A create on the free repo goes through
	updated, _ = m.Update(ui.ModalCreateMsg{RepoNames: []string{"goforms"}, Branch: "feat/new"})
	m = updated.(Model)
	if _, ok := m.busy["goforms"]; !ok {
		t.Error("create on free repo should lock it")
	}
}

func TestRefreshAllRequiresAllFree(t *testing.T) {
	m := testModel()
	m.beginOp("Creating feat/a...", "core")

	before := len(m.busy)
	m2, _ := handleKeyPress(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if len(m2.busy) != before {
		t.Errorf("refresh-all must be blocked while any repo is busy, locks: %v", m2.busy)
	}
}

func TestSpinnerTickSingleLoop(t *testing.T) {
	m := testModel()
	m.loaderTicking = false

	_, tick1 := m.beginOp("op a", "core")
	if tick1 == nil {
		t.Error("first op should start the spinner")
	}
	_, tick2 := m.beginOp("op b", "goforms")
	if tick2 != nil {
		t.Error("second op must not start another spinner loop")
	}
}

func TestSortRepos(t *testing.T) {
	mk := func() []git.Repo {
		return []git.Repo{
			{Name: "zeta", Worktrees: []git.Worktree{
				{Branch: "b", LastActivity: 100, CreatedAt: 900},
				{Branch: "a", LastActivity: 300, CreatedAt: 100},
			}},
			{Name: "alpha", Worktrees: []git.Worktree{
				{Branch: "x", LastActivity: 200, CreatedAt: 500},
			}},
			{Name: "empty"}, // no worktrees — sinks under timestamp sorts
		}
	}

	repos := mk()
	sortRepos(repos, "activity")
	if repos[0].Name != "zeta" || repos[1].Name != "alpha" || repos[2].Name != "empty" {
		t.Errorf("activity order = %s,%s,%s", repos[0].Name, repos[1].Name, repos[2].Name)
	}
	if repos[0].Worktrees[0].Branch != "a" {
		t.Errorf("worktrees should be newest-activity first, got %s", repos[0].Worktrees[0].Branch)
	}

	repos = mk()
	sortRepos(repos, "created")
	if repos[0].Name != "zeta" || repos[0].Worktrees[0].Branch != "b" {
		t.Errorf("created order wrong: repo %s, first wt %s", repos[0].Name, repos[0].Worktrees[0].Branch)
	}

	repos = mk()
	sortRepos(repos, "name")
	if repos[0].Name != "alpha" || repos[1].Name != "empty" || repos[2].Name != "zeta" {
		t.Errorf("name order = %s,%s,%s", repos[0].Name, repos[1].Name, repos[2].Name)
	}
	if repos[2].Worktrees[0].Branch != "a" {
		t.Errorf("name mode sorts worktrees by branch, got %s", repos[2].Worktrees[0].Branch)
	}
}
