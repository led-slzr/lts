package app

import (
	"strings"
	"testing"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/lock"
	"lts-revamp/internal/ui"
)

// When another lts process (an agent's CLI run) holds a repo's lock, the
// TUI must refuse to start an operation on it — not warn and proceed.
func TestTUIRefusesOpsOnExternallyLockedRepo(t *testing.T) {
	ws := t.TempDir()
	m := NewModel(config.Config{Global: config.DefaultGlobal(), Local: map[string]config.RepoLocalConfig{}, WorkDir: ws})
	m.width, m.height = 100, 40
	m.initialLoad = false
	repo := git.Repo{Name: "core", Path: ws + "/core", MainBranch: "main"}
	wt := git.Worktree{Branch: "feat/x", Path: ws + "/core-lts/feat-x", Status: git.StatusClean}
	repo.Worktrees = []git.Worktree{wt}
	m.repos = []git.Repo{repo}

	h, err := lock.TryAcquire(ws, []string{"core"}, "lts delete feat/x")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()

	m.menuRepo, m.menuWT, m.menuHasWT = repo, wt, true
	for _, action := range []ui.HoverButton{ui.BtnRebase, ui.BtnCleanModules, ui.BtnRefresh} {
		m2, _ := executeContextAction(m, action)
		if len(m2.busy) != 0 {
			t.Fatalf("action %v started despite the external lock (busy=%v)", action, m2.busy)
		}
		if !strings.Contains(m2.statusMsg, "busy") || !strings.Contains(m2.statusMsg, "lts delete") {
			t.Fatalf("action %v: status should name the holder, got %q", action, m2.statusMsg)
		}
	}

	// Once released, the same action takes the lock itself
	h.Release()
	m2, _ := executeContextAction(m, ui.BtnRebase)
	if len(m2.busy) != 1 {
		t.Fatalf("rebase should start after release, busy=%v", m2.busy)
	}
	if locked, owner := lock.Holder(ws, "core"); !locked || !strings.Contains(owner, "TUI") {
		t.Fatalf("TUI must hold the lock while operating (locked=%v owner=%q)", locked, owner)
	}
	m2.clearBusy("core")
	if locked, _ := lock.Holder(ws, "core"); locked {
		t.Fatal("lock must be released when the op finishes")
	}
}
