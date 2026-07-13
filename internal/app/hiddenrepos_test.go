package app

import (
	"strings"
	"testing"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/ui"
)

// Hidden repos vanish from the loaded list (making the empty-state folder
// suggestions reachable in cluttered directories) unless Show Hidden Repos
// is on — then they stay, flagged for grayed-out rendering.
func TestHiddenReposFilteredOnLoad(t *testing.T) {
	m := testModel()
	m.config.Local[".NVM"] = config.RepoLocalConfig{Hidden: true}
	loaded := []git.Repo{{Name: ".nvm", Path: "/x/.nvm"}, {Name: "core", Path: "/x/core"}}

	updated, _ := m.Update(ReposLoadedMsg{Repos: loaded})
	m2 := updated.(Model)
	if len(m2.repos) != 1 || m2.repos[0].Name != "core" {
		t.Fatalf("hidden repo should be dropped, got %+v", names(m2.repos))
	}

	m2.config.Global.ShowHiddenRepos = true
	updated, _ = m2.Update(ReposLoadedMsg{Repos: []git.Repo{{Name: ".nvm", Path: "/x/.nvm"}, {Name: "core", Path: "/x/core"}}})
	m3 := updated.(Model)
	if len(m3.repos) != 2 {
		t.Fatalf("show-hidden should keep both, got %v", names(m3.repos))
	}
	var nvm git.Repo
	for _, r := range m3.repos {
		if r.Name == ".nvm" {
			nvm = r
		}
	}
	if !nvm.Hidden {
		t.Fatal("shown hidden repo must carry the Hidden flag for rendering")
	}
}

func names(repos []git.Repo) []string {
	out := make([]string, len(repos))
	for i, r := range repos {
		out[i] = r.Name
	}
	return out
}

// Hiding is only offered on worktree-less plain repos; hidden repos get a
// single-action menu (Unhide).
func TestHideMenuGating(t *testing.T) {
	m := testModel()
	if !m.canHideRepo(git.Repo{Name: "x", Path: "/x/x"}) {
		t.Error("bare repo should be hideable")
	}
	if m.canHideRepo(git.Repo{Name: "x", Path: "/x/x", Worktrees: []git.Worktree{{Name: "w"}}}) {
		t.Error("repo with worktrees must not be hideable")
	}
	if m.canHideRepo(git.Repo{Name: "m", IsMonorepo: true}) {
		t.Error("mono cards are not hideable")
	}
	// testModel has a core-goforms mono card — its constituents stay visible.
	if m.canHideRepo(git.Repo{Name: "core", Path: "/x/core"}) {
		t.Error("live mono-group constituents must not be hideable")
	}
	items := ui.RepoContextItems(false, false, false, true)
	if len(items) != 1 || items[0].Label != "Unhide Repo" {
		t.Fatalf("hidden repo menu should be Unhide only, got %+v", items)
	}
}

// The hide action persists and reloads; unhide reverses it.
func TestHideUnhideActionRoundTrip(t *testing.T) {
	m := testModel()
	m.config.WorkDir = t.TempDir()
	m.menuRepo = git.Repo{Name: "clutter", Path: m.config.WorkDir + "/clutter"}
	m.menuHasWT = false

	m2, cmd := executeContextAction(m, ui.BtnHideRepo)
	if cmd == nil || !m2.config.IsRepoHidden("clutter") {
		t.Fatal("hide should persist and trigger a reload")
	}
	if !strings.Contains(m2.statusMsg, "Hid clutter") {
		t.Errorf("status: %q", m2.statusMsg)
	}
	// Survives a config reload from disk.
	reloaded := config.Load(m2.config.WorkDir)
	if !reloaded.IsRepoHidden("clutter") {
		t.Fatal("hidden flag should persist in .lts.conf")
	}

	m3, _ := executeContextAction(m2, ui.BtnUnhideRepo)
	if m3.config.IsRepoHidden("clutter") {
		t.Fatal("unhide should clear the flag")
	}
}
