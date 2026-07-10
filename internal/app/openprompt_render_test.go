package app

import (
	"strings"
	"testing"

	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"

	"github.com/charmbracelet/lipgloss"
)

func TestOpenPromptOptionsRowMatchesHitZoneMath(t *testing.T) {
	cfg := config.Config{Global: config.DefaultGlobal()}
	m := NewModel(cfg)
	m.width, m.height = 100, 40
	m.openPromptActive = true
	m.openPromptSelection = opener.ClickIDE
	m.openPromptResults = []*git.CreateResult{
		{RepoName: "goforms", Branch: "test/test", WorkspaceFile: "/x/goforms-lts/test-test.code-workspace", WorktreePath: "/x/goforms-lts/test-test"},
	}

	modal := m.renderOpenPromptDialog()
	t.Log("\n" + modal)

	_, modalTop, modalH := modalMetrics(modal, m.height)
	optionsY := modalTop + modalH - 5
	lines := strings.Split(modal, "\n")
	rowInModal := optionsY - modalTop
	if rowInModal < 0 || rowInModal >= len(lines) {
		t.Fatalf("options row %d out of range (%d lines)", rowInModal, len(lines))
	}
	row := lines[rowInModal]
	for _, want := range []string{"IDE", "Claude", "Terminal"} {
		if !strings.Contains(row, want) {
			t.Errorf("options row (modal line %d) missing %q: %q", rowInModal, want, row)
		}
	}

	// Option X offsets must match where the labels actually render.
	// Compare in runes: box-drawing chars are multi-byte, screen columns are runes.
	plainRow := []rune(stripANSI(row))
	for _, opt := range m.openPromptOptions() {
		// opt.x is the padded cell start; label begins one char in (Padding(0,1)).
		idx := runeIndex(plainRow, opt.label)
		if idx != opt.x+1 {
			t.Errorf("option %q: zone starts at x=%d (label expected at %d), actual label at %d", opt.label, opt.x, opt.x+1, idx)
		}
	}
}

func runeIndex(haystack []rune, needle string) int {
	n := []rune(needle)
	for i := 0; i+len(n) <= len(haystack); i++ {
		if string(haystack[i:i+len(n)]) == needle {
			return i
		}
	}
	return -1
}

func stripANSI(s string) string {
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

func TestOpenPromptModalHeightAssumption(t *testing.T) {
	// The mouse handler assumes: modalH = results + 12 (8 chrome/content lines
	// + border*2 + padding*2). Guard it for 1 and 3 results.
	for _, n := range []int{1, 3} {
		cfg := config.Config{Global: config.DefaultGlobal()}
		m := NewModel(cfg)
		m.width, m.height = 100, 40
		var results []*git.CreateResult
		for i := 0; i < n; i++ {
			results = append(results, &git.CreateResult{RepoName: "r", Branch: "b"})
		}
		m.openPromptResults = results
		if got, want := lipgloss.Height(m.renderOpenPromptDialog()), n+12; got != want {
			t.Errorf("n=%d: modal height %d, want %d", n, got, want)
		}
	}
}
