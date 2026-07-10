package ui

import (
	"strings"
	"testing"

	"lts-revamp/internal/config"
)

func stripANSITest(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func testSettings(repoCount int, w, h int) SettingsModel {
	cfg := &config.Config{Global: config.DefaultGlobal(), Local: map[string]config.RepoLocalConfig{}, WorkDir: "/x"}
	var repos []string
	for i := 0; i < repoCount; i++ {
		repos = append(repos, "repo-"+string(rune('a'+i)))
	}
	s := NewSettings(cfg, repos)
	s.ViewWidth, s.ViewHeight = w, h
	return s
}

// The tab bar must fit on one line even at the minimum supported terminal
// width (60), and tab hit-testing must land where the names render.
func TestSettingsTabBarSingleLineAndHitTest(t *testing.T) {
	for _, w := range []int{60, 100} {
		s := testSettings(2, w, 30)
		lines := strings.Split(s.View(w, 30), "\n")

		tabRow := -1
		for y, line := range lines {
			plain := stripANSITest(line)
			if strings.Contains(plain, "Preferences") {
				// All four names on the same line = no wrap
				for _, name := range s.TabNames {
					if !strings.Contains(plain, name) {
						t.Fatalf("w=%d: tab bar wrapped — %q missing from row %d: %q", w, name, y, plain)
					}
				}
				tabRow = y
				break
			}
		}
		if tabRow < 0 {
			t.Fatalf("w=%d: tab bar not found", w)
		}

		// Clicking each tab name's rendered position selects that tab
		plain := []rune(stripANSITest(lines[tabRow]))
		for i, name := range s.TabNames {
			x := runeIndexOf(plain, name)
			if x < 0 {
				t.Fatalf("w=%d: %q not found in tab row", w, name)
			}
			if got := s.hitTestTab(x+1, tabRow); got != i {
				t.Errorf("w=%d: hitTestTab at %q (x=%d,y=%d) = %d, want %d", w, name, x+1, tabRow, got, i)
			}
		}
	}
}

// Every row the mouse can activate must be the row where that item renders —
// including on the scrolled Worktrees tab with many repos.
func TestSettingsItemHitTestMatchesRender(t *testing.T) {
	cases := []struct {
		name   string
		repos  int
		tab    int
		height int
		scroll int
	}{
		{"preferences", 2, TabPreferences, 40, 0},
		{"worktrees-many-repos", 8, TabWorktrees, 40, 0},
		{"worktrees-scrolled", 8, TabWorktrees, 24, 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testSettings(tc.repos, 100, tc.height)
			s.ActiveTab = tc.tab
			s.buildItems(s.RepoNames)
			s.Scroll = tc.scroll

			lines := strings.Split(s.View(100, tc.height), "\n")
			_, _, contentLeft, _ := s.modalMetrics()

			hits := 0
			for y, line := range lines {
				idx := s.hitTestItem(contentLeft+2, y)
				if idx < 0 {
					continue
				}
				hits++
				plain := stripANSITest(line)
				if !strings.Contains(plain, s.Items[idx].Label) {
					t.Errorf("row %d: hitTestItem says item %d (%q) but row renders %q",
						y, idx, s.Items[idx].Label, strings.TrimSpace(plain))
				}
			}
			if hits == 0 {
				t.Error("no rows hit — hit zones misaligned with render")
			}
		})
	}
}

func runeIndexOf(haystack []rune, needle string) int {
	n := []rune(needle)
	for i := 0; i+len(n) <= len(haystack); i++ {
		if string(haystack[i:i+len(n)]) == needle {
			return i
		}
	}
	return -1
}
