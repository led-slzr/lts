package app

import (
	"testing"
	"time"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
)

func TestAgeDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"OFF": 0, "": 0, "off": 0,
		"8H":  8 * time.Hour,
		"1D":  24 * time.Hour,
		"7D":  7 * 24 * time.Hour,
		"30D": 30 * 24 * time.Hour,
		"2W":  0, // unknown suffix — disabled, never a surprise sweep
		"XD":  0, // garbage — disabled
		"-1D": 0, // negative — disabled
	}
	for in, want := range cases {
		if got := ageDuration(in); got != want {
			t.Errorf("ageDuration(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMaintenanceCandidates(t *testing.T) {
	now := time.Now().Unix()
	old := now - 3*86400 // 3 days idle
	m := NewModel(config.Config{Global: config.DefaultGlobal(), WorkDir: "/x"})
	m.repos = []git.Repo{
		{Name: "idle", Path: "/x/idle", Worktrees: []git.Worktree{
			{Branch: "feat/old", Path: "/x/idle-lts/feat-old", LastActivity: old},
			{Branch: "feat/hot", Path: "/x/idle-lts/feat-hot", LastActivity: now - 60},
		}},
		{Name: "busyrepo", Path: "/x/busyrepo", Worktrees: []git.Worktree{
			{Branch: "feat/b", Path: "/x/busyrepo-lts/feat-b", LastActivity: old},
		}},
		{Name: "migrating", Path: "/x/migrating", NeedsMigration: true, Worktrees: []git.Worktree{
			{Branch: "feat/m", Path: "/x/migrating-lts/feat-m", LastActivity: old},
		}},
		{Name: "unknown", Path: "/x/unknown", Worktrees: []git.Worktree{
			{Branch: "feat/u", Path: "/x/unknown-lts/feat-u", LastActivity: 0}, // no data — never swept
		}},
		{Name: "idle-group", IsMonorepo: true, RepoNames: []string{"idle", "goforms"}, Worktrees: []git.Worktree{
			{Branch: "feat/g", Path: "/x/idle-goforms-lts/feat-g", LastActivity: old},
		}},
	}
	m.beginOp("Rebasing...", "busyrepo")

	paths, locks := m.maintenanceCandidates(24 * time.Hour)

	wantPaths := map[string]bool{
		"/x/idle-lts/feat-old":       true,
		"/x/idle-goforms-lts/feat-g": true,
	}
	if len(paths) != len(wantPaths) {
		t.Fatalf("paths = %v, want %v", paths, wantPaths)
	}
	for _, p := range paths {
		if !wantPaths[p] {
			t.Errorf("unexpected candidate %s", p)
		}
	}

	// Locks: idle (once, deduped across the single repo and the group) + goforms
	lockSet := map[string]bool{}
	for _, l := range locks {
		if lockSet[l] {
			t.Errorf("duplicate lock %s", l)
		}
		lockSet[l] = true
	}
	if !lockSet["idle"] || !lockSet["goforms"] {
		t.Errorf("locks = %v, want idle+goforms", locks)
	}
	if lockSet["busyrepo"] || lockSet["migrating"] || lockSet["unknown"] {
		t.Errorf("locks include skipped repos: %v", locks)
	}
}

func TestStartAutoMaintenanceDisabled(t *testing.T) {
	m := NewModel(config.Config{Global: config.DefaultGlobal(), WorkDir: "/x"})
	m.repos = []git.Repo{{Name: "idle", Path: "/x/idle", Worktrees: []git.Worktree{
		{Branch: "feat/old", Path: "/x/idle-lts/feat-old", LastActivity: time.Now().Unix() - 30*86400},
	}}}
	// Both settings default OFF — nothing must run, nothing must lock
	if cmd := m.startAutoMaintenance(); cmd != nil {
		t.Error("maintenance must be a no-op when both settings are OFF")
	}
	if m.anyBusy() {
		t.Errorf("no locks should be taken: %v", m.busy)
	}
}
