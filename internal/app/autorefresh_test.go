package app

import (
	"testing"
	"time"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/lock"
)

func TestAutoRefreshDueAndLocking(t *testing.T) {
	ws := t.TempDir()
	cfg := config.Config{Global: config.DefaultGlobal(), Local: map[string]config.RepoLocalConfig{}, WorkDir: ws}
	cfg.Global.AutoRefresh = "30M"
	m := NewModel(cfg)
	m.initialLoad = false
	now := time.Now()
	m.config.Local["FRESH"] = config.RepoLocalConfig{BasisBranch: "main", LastRefresh: now.Add(-5 * time.Minute).Unix()}
	m.config.Local["STALE"] = config.RepoLocalConfig{BasisBranch: "main", LastRefresh: now.Add(-2 * time.Hour).Unix()}
	m.config.Local["HIDDEN"] = config.RepoLocalConfig{BasisBranch: "main", Hidden: true}
	m.repos = []git.Repo{
		{Name: "fresh", Path: ws + "/fresh"},
		{Name: "stale", Path: ws + "/stale"},
		{Name: "hidden", Path: ws + "/hidden"},
		{Name: "never", Path: ws + "/never"},
		{Name: "stale-never", IsMonorepo: true, RepoNames: []string{"stale", "never"}},
	}
	names := func(rs []git.Repo) map[string]bool {
		out := map[string]bool{}
		for _, r := range rs {
			out[r.Name] = true
		}
		return out
	}
	due := names(m.autoRefreshDue(now))
	if !due["stale"] || !due["never"] || due["fresh"] || due["hidden"] || due["stale-never"] {
		t.Fatalf("due = %v", due)
	}

	// A repo locked by another process is left for the next check
	h, _ := lock.TryAcquire(ws, []string{"never"}, "lts rebase")
	defer h.Release()
	mm, _ := m.autoRefresh()
	got := mm.(Model)
	if _, busy := got.busy["stale"]; !busy {
		t.Fatalf("stale should be refreshing, busy=%v", got.busy)
	}
	if _, busy := got.busy["never"]; busy {
		t.Fatal("never is locked elsewhere and must not be touched")
	}

	m.config.Global.AutoRefresh = "OFF"
	if len(m.autoRefreshDue(now)) != 0 {
		t.Fatal("OFF must never refresh")
	}
	if ageDuration("15M") != 15*time.Minute {
		t.Fatal("minutes not parsed")
	}
}
