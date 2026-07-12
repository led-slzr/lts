package ui

import (
	"fmt"
	"strings"

	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"

	"github.com/charmbracelet/lipgloss"
)

// Theme Studio: a full-screen, Explorer-shaped browser — themes in the
// left pane, a synthetic repo in the right pane with one worktree per
// status so every palette slot is exercised. There is no separate preview
// renderer: browsing hot-swaps the live theme, so the studio itself (and
// everything in it) IS the preview. Enter commits, esc restores.

type StudioModel struct {
	Active   bool
	Cursor   int
	Hovered  int    // mouse-hovered list row (-1 = none)
	SavedKey string // theme to restore on esc
}

// NewThemeStudio opens the studio with the cursor on the active theme.
func NewThemeStudio(currentKey string) StudioModel {
	cursor := 0
	for i, t := range Themes {
		if t.Key == currentKey {
			cursor = i
			break
		}
	}
	return StudioModel{Active: true, Cursor: cursor, Hovered: -1, SavedKey: currentKey}
}

// Studio geometry — the mouse handler depends on these matching the render.
const (
	StudioListStartY = 4  // first theme row (title, blank, pane header above)
	StudioListWidth  = 26 // left pane incl. separator column
)

// sampleWT is one preview row: every WTStatus appears once.
type sampleWT struct {
	branch string
	status git.WTStatus
	text   string
	age    string
	tmux   bool
}

var studioSamples = []sampleWT{
	{"feat/checkout-flow", git.StatusClean, "✓ clean", "2h", true},
	{"fix/login-redirect", git.StatusChanged, "3 changed", "5h", true},
	{"feat/pricing-page", git.StatusToPush, "2 to push", "1d", false},
	{"chore/deps-bump", git.StatusToPull, "1 to pull", "2d", false},
	{"fix/race-condition", git.StatusDiverged, "⚠ diverged", "3d", false},
	{"feat/dark-mode", git.StatusMergedCleanable, "merged, cleanable", "4d", true},
	{"feat/onboarding", git.StatusMergedDirty, "merged, 2 changed", "6d", false},
	{"feat/api-v2", git.StatusNew, "new branch", "1w", false},
	{"fix/typo-hotfix", git.StatusNewDirty, "new, 1 changed", "2w", false},
	{"spike/websockets", git.StatusNoRemote, "never pushed", "3w", false},
	{"feat/old-experiment", git.StatusMissing, "⚠ missing", "5w", false},
}

// RenderThemeStudio draws the full screen in the currently applied theme.
func RenderThemeStudio(st StudioModel, width, height int) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Background(ColorBlack)
	dim := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	white := lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Background(ColorBlack)
	sel := lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Background(ColorDarkGreen)
	sep := lipgloss.NewStyle().Foreground(ColorGray).Background(ColorBlack)
	crit := lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Background(ColorBlack)

	listW := StudioListWidth - 2 // minus separator column and its space

	// --- Left pane: the theme list ---
	var left []string
	left = append(left, dim.Render(padCell("THEMES", listW)))
	for i, t := range Themes {
		label := " " + t.Name
		if t.Key == st.SavedKey {
			label += " •"
		}
		switch {
		case i == st.Cursor:
			left = append(left, sel.Render(padCell("▸"+label, listW)))
		case i == st.Hovered:
			left = append(left, white.Render(padCell(" "+label, listW)))
		default:
			left = append(left, dim.Render(padCell(" "+label, listW)))
		}
	}

	// --- Right pane: the synthetic repo sheet ---
	rightW := width - StudioListWidth - 3
	var right []string
	right = append(right, white.Render("sample-repo")+dim.Render("  — every status, live in "+CurrentTheme.Name))
	right = append(right,
		dim.Render(fmt.Sprintf("%-24s%-22s%-6s%s", "BRANCH", "STATUS", "AGE", "TMUX")))
	for i, wt := range studioSamples {
		branch := padCell(wt.branch, 24)
		var line string
		if i == 1 { // one row rendered as the Explorer selection
			line = sel.Render(padCell("▸ "+wt.branch, 24)) + statusStyle(wt.status).Render(padCell(wt.text, 22))
		} else {
			line = WTBranchStyle.Render(branch) + statusStyle(wt.status).Render(padCell(wt.text, 22))
		}
		line += dim.Render(padCell(wt.age, 6))
		if wt.tmux {
			line += lipgloss.NewStyle().Foreground(ColorTeal).Background(ColorBlack).Render("●")
		}
		right = append(right, line)
	}
	right = append(right, "")
	right = append(right,
		InlineBtnHoverStyle.Render("[enter open]")+dim.Render(" ")+
			InlineBtnStyle.Render("[b rebase] [g pr] [m rename] [p modules] [d delete] [x kill tmux]"))

	// --- Mini Board card (the real renderer) when height allows ---
	card := studioSampleCard(minStudio(rightW, 46))
	cardLines := strings.Split(card, "\n")
	strip := []string{
		"",
		StatusBarStyle.Render("✓ Created feat/preview — deps installed, opened in AI CLI"),
		FooterStyle.Render("[r] Refresh   [n] New Worktree   [C] Cleanup   [s] Settings") +
			"   " + crit.Render("⚠ work will be LOST"),
	}
	needed := StudioListStartY + len(right) + 1 + len(cardLines) + len(strip) + 2
	if height >= needed {
		right = append(right, "")
		right = append(right, cardLines...)
	}
	right = append(right, strip...)

	// --- Compose: title, then left │ right, then the hint line ---
	lines := []string{
		"",
		" " + accent.Render("◆ Theme Studio") + dim.Render("  — the preview is live: this whole screen wears the selected theme"),
		"",
	}
	rows := len(right)
	if len(left) > rows {
		rows = len(left)
	}
	for i := 0; i < rows; i++ {
		l, r := padCell("", listW), ""
		if i < len(left) {
			l = left[i]
		} else {
			l = dim.Render(padCell("", listW))
		}
		if i < len(right) {
			r = right[i]
		}
		lines = append(lines, " "+l+sep.Render("│ ")+truncate(r, rightW))
	}
	for len(lines) < height-2 {
		lines = append(lines, "")
	}
	saved := ThemeByKey(st.SavedKey).Name
	lines = append(lines, " "+dim.Render("↑/↓ or hover to try on · enter apply · esc keep "+saved))
	return strings.Join(lines, "\n")
}

// studioSampleCard renders a genuine Board card via the real renderer, so
// borders, tree characters and the title style preview faithfully too.
func studioSampleCard(cardWidth int) string {
	wts := []git.Worktree{
		{Name: "feat/checkout-flow", Branch: "feat/checkout-flow", Path: "/preview/sample-repo-lts/sample-repo-feat-checkout-flow",
			Status: git.StatusClean, StatusText: "✓ clean"},
		{Name: "fix/login-redirect", Branch: "fix/login-redirect", Path: "/preview/sample-repo-lts/sample-repo-fix-login-redirect",
			Status: git.StatusChanged, StatusText: "3 changed"},
		{Name: "feat/pricing-page", Branch: "feat/pricing-page", Path: "/preview/sample-repo-lts/sample-repo-feat-pricing-page",
			Status: git.StatusToPush, StatusText: "2 to push"},
	}
	repo := git.Repo{Name: "sample-repo", Path: "/preview/sample-repo", MainBranch: "main", LTSDir: "sample-repo-lts", Worktrees: wts}
	tmuxLive := map[string]bool{opener.SessionName(wts[0].Path): true}
	return RenderCard(repo, cardWidth, false, -1, BtnNone, false, tmuxLive)
}

func minStudio(a, b int) int {
	if a < b {
		return a
	}
	return b
}
