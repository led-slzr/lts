package ui

import (
	"strings"
	"testing"
	"time"

	"lts-revamp/internal/git"
)

func explorerTestRepos() []git.Repo {
	now := time.Now().Unix()
	return []git.Repo{
		{Name: "core", Path: "/x/core", MainBranch: "main", Worktrees: []git.Worktree{
			{Name: "feat/one", Branch: "feat/one", Path: "/x/core-lts/feat-one", Status: git.StatusClean, StatusText: "synced", LastActivity: now - 3600},
			{Name: "fix/two", Branch: "fix/two", Path: "/x/core-lts/fix-two", Status: git.StatusChanged, StatusText: "3 changed | synced", LastActivity: now - 86400*2},
		}},
		{Name: "goforms", Path: "/x/goforms", MainBranch: "main"},
		{Name: "core-goforms", IsMonorepo: true, RepoNames: []string{"core", "goforms"}, Worktrees: []git.Worktree{
			{Name: "feat/x", Branch: "feat/x", Path: "/x/core-goforms-lts/feat-x", Status: git.StatusNew, StatusText: "new"},
		}},
	}
}

// Every Explorer hit zone must land on the row that renders its target.
func TestExplorerHitZonesMatchRender(t *testing.T) {
	repos := explorerTestRepos()
	st := ExplorerState{SelectedRepo: 0, SelectedWT: 1, FocusSheet: true}
	yOffset := 8
	res := LayoutExplorer(repos, 100, yOffset, 20, st, BtnNone, nil, nil, CloneReady, nil, true)

	lines := strings.Split(res.View, "\n")
	rowText := func(y int) string {
		idx := y - yOffset
		if idx < 0 || idx >= len(lines) {
			t.Fatalf("zone y=%d outside rendered block (%d lines)", y, len(lines))
		}
		return stripANSITest(lines[idx])
	}

	seenRepos, seenRows, seenActions, seenNew := 0, 0, 0, 0
	for _, z := range res.HitZones {
		switch z.Type {
		case ZoneExplorerRepo:
			seenRepos++
			if !strings.Contains(rowText(z.Y), repos[z.RepoIdx].Name) {
				t.Errorf("repo zone %d (y=%d) row %q missing %q", z.RepoIdx, z.Y, rowText(z.Y), repos[z.RepoIdx].Name)
			}
		case ZoneExplorerRow:
			seenRows++
			branch := repos[st.SelectedRepo].Worktrees[z.WTIdx].Branch
			if !strings.Contains(rowText(z.Y), branch) {
				t.Errorf("row zone wt=%d (y=%d) row %q missing %q", z.WTIdx, z.Y, rowText(z.Y), branch)
			}
		case ZoneExplorerAction:
			seenActions++
			row := []rune(rowText(z.Y))
			if z.X+z.W > len(row) {
				t.Errorf("action zone [%d,%d) beyond row width %d", z.X, z.X+z.W, len(row))
				continue
			}
			seg := string(row[z.X : z.X+z.W])
			if !strings.HasPrefix(seg, "[") || !strings.HasSuffix(seg, "]") {
				t.Errorf("action zone at x=%d y=%d not on a button: %q (row %q)", z.X, z.Y, seg, string(row))
			}
		case ZoneExplorerNew:
			seenNew++
			row := []rune(rowText(z.Y))
			if z.X+z.W > len(row) {
				t.Errorf("[+ new] zone beyond row")
				continue
			}
			if got := string(row[z.X : z.X+z.W]); got != "n New Worktree" {
				t.Errorf("new-worktree zone at x=%d got %q (row %q)", z.X, got, string(row))
			}
		}
	}
	if seenRepos != len(repos) {
		t.Errorf("expected %d repo zones, got %d", len(repos), seenRepos)
	}
	if seenRows != 2 {
		t.Errorf("expected 2 row zones, got %d", seenRows)
	}
	// selected row's strip: rebase, pr, rename, modules, delete
	// (open isn't listed — clicking the row is the primary action)
	if seenActions != 5 {
		t.Errorf("expected 5 action zones, got %d", seenActions)
	}
	hasPR := false
	for _, z := range res.HitZones {
		if z.Type == ZoneExplorerAction && z.Button == BtnCreatePR {
			hasPR = true
		}
	}
	if !hasPR {
		t.Error("expected a PR action when canPR is set")
	}
	if seenNew != 1 {
		t.Errorf("expected 1 [+ new] zone, got %d", seenNew)
	}

	// Status text is fully visible in the sheet (the Board can't fit it)
	full := stripANSITest(res.View)
	if !strings.Contains(full, "3 changed | synced") {
		t.Error("sheet should show the full status text")
	}
	// AGE column present at this width
	if !strings.Contains(full, "AGE") || !strings.Contains(full, "2d") {
		t.Error("expected AGE column with relative age")
	}
}

// Monorepo selection: no rebase in the action strip.
func TestExplorerMonorepoActions(t *testing.T) {
	repos := explorerTestRepos()
	st := ExplorerState{SelectedRepo: 2, SelectedWT: 0, FocusSheet: true}
	res := LayoutExplorer(repos, 100, 8, 20, st, BtnNone, nil, nil, CloneReady, nil, false)
	actions := 0
	for _, z := range res.HitZones {
		if z.Type == ZoneExplorerAction {
			actions++
			if z.Button == BtnRebase {
				t.Error("monorepo action strip must not offer rebase")
			}
		}
	}
	if actions != 3 {
		t.Errorf("expected 3 actions for monorepo (rename/modules/delete), got %d", actions)
	}
}

// The header View toggle hit zones must match where Board/Explorer render.
func TestViewToggleHitZonesMatchRender(t *testing.T) {
	labels := UsageLabels{IDE: "Windsurf", AICli: "Claude", Terminal: "Ghostty"}
	// Greeting fills the banner gap — it must never shift the right block
	header := RenderHeader(120, 0, labels, HeaderOpts{Layout: "board", HoveredView: -1, Greeting: "Heyya Mark! Let's get worktree-ing", GhUser: "led-slzr"})
	lines := strings.Split(header, "\n")

	y, zones := ViewToggleHitZones(120, labels)
	if y >= len(lines) {
		t.Fatalf("view toggle y=%d beyond header height %d", y, len(lines))
	}
	row := []rune(stripANSITest(lines[y]))
	names := []string{"Board", "Explorer"}
	for i, z := range zones {
		if z.X+z.W > len(row) {
			t.Fatalf("zone %d beyond row", i)
		}
		seg := strings.TrimSpace(string(row[z.X : z.X+z.W]))
		if seg != names[i] {
			t.Errorf("view zone %d = %q, want %q (row %q)", i, seg, names[i], string(row))
		}
	}
}

// The GitHub login zone must land where the name renders.
func TestGhUserHitZoneMatchesRender(t *testing.T) {
	labels := UsageLabels{IDE: "Windsurf", AICli: "Claude", Terminal: "Ghostty"}
	header := RenderHeader(120, 0, labels, HeaderOpts{Layout: "board", HoveredView: -1, GhUser: "led-slzr"})
	lines := strings.Split(header, "\n")
	x, y, w := GhUserHitZone(120, labels, "led-slzr")
	if y >= len(lines) {
		t.Fatalf("zone y=%d beyond header %d", y, len(lines))
	}
	row := []rune(stripANSITest(lines[y]))
	if x+w > len(row) {
		t.Fatalf("zone beyond row")
	}
	if got := string(row[x : x+w]); got != "led-slzr" {
		t.Errorf("zone at x=%d got %q (row %q)", x, got, string(row))
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Now().Unix()
	cases := map[int64]string{
		0:             "—",
		now - 10:      "now",
		now - 300:     "5m",
		now - 7200:    "2h",
		now - 86400*3: "3d",
	}
	for ts, want := range cases {
		if got := formatAge(ts); got != want {
			t.Errorf("formatAge(%d) = %q, want %q", ts, got, want)
		}
	}
}

func TestFormatWTSize(t *testing.T) {
	cases := []struct {
		sz   WTSize
		want string
	}{
		{WTSize{}, "…"}, // unscanned
		{WTSize{Total: 500, Known: true}, "500B"},
		{WTSize{Total: 300 << 20, Known: true}, "300M"},
		{WTSize{Total: 1400 << 20, Modules: 1100 << 20, Known: true}, "300M+1.1G"},
		{WTSize{Total: 5 << 30, Modules: 4 << 30, Known: true}, "1.0G+4.0G"},
	}
	for _, tc := range cases {
		if got := formatWTSize(tc.sz); got != tc.want {
			t.Errorf("formatWTSize(%+v) = %q, want %q", tc.sz, got, tc.want)
		}
	}
}
