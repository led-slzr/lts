package ui

import (
	"fmt"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/version"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// VersionHitZone returns the screen coordinates of the version label in the header.
// The banner has 6 lines, version is next, with Margin(1, MarginH, 0, MarginH).
func VersionHitZone() (x, y, w int) {
	versionText := "v" + version.Display()
	return MarginH, 1 + len(ltsBanner), len(versionText)
}

// ReleaseURL returns the GitHub releases URL for the current version.
func ReleaseURL() string {
	return fmt.Sprintf("https://github.com/led-slzr/lts/releases/tag/v%s", version.Version)
}

// UpdateBadgeHitZone returns the screen coordinates of the "(Update Available)" badge.
// It sits right after the version text on the same line.
func UpdateBadgeHitZone() (x, y, w int) {
	versionW := len("v" + version.Display()) // plain ASCII, len = visual width
	badgeW := len(" (Update Available)")
	return MarginH + versionW, 1 + len(ltsBanner), badgeW
}

// Big block-letter LTS title
var ltsBanner = []string{
	"██╗     ████████╗███████╗",
	"██║     ╚══██╔══╝██╔════╝",
	"██║        ██║   ███████╗",
	"██║        ██║   ╚════██║",
	"███████╗   ██║   ███████║",
	"╚══════╝   ╚═╝   ╚══════╝",
}

// Inline tree-themed spinner frames — all padded to equal visual width (13 chars).
// A mini worktree branch growing cycle using the same characters as the loader.
var spinnerFrames = []string{
	"·            ",
	"· ─          ",
	"· ── ○       ",
	"· ── ○ ─     ",
	"· ── ○ ── ○  ",
	"· ── ○ ── ○  ",
	"· ── ○       ",
	"·            ",
}

// RenderSpinner returns a styled inline spinner frame.
func RenderSpinner(frame int) string {
	idx := frame % len(spinnerFrames)
	f := spinnerFrames[idx]

	bg := lipgloss.NewStyle().Background(ColorBlack)
	nodeStyle := bg.Foreground(ColorGreen).Bold(true)
	branchStyle := bg.Foreground(ColorDarkGreen)
	seedStyle := bg.Foreground(ColorYellow)

	var result strings.Builder
	for _, ch := range f {
		switch ch {
		case '○':
			result.WriteString(nodeStyle.Render("○"))
		case '·':
			result.WriteString(seedStyle.Render("●"))
		case '─':
			result.WriteString(branchStyle.Render("─"))
		default:
			result.WriteRune(ch)
		}
	}
	return result.String()
}

// ClickUsageZone represents a clickable region for a click usage mode.
type ClickUsageZone struct {
	X     int
	W     int
	Usage opener.ClickUsage
}

// headerLayout holds the computed positions shared between rendering and hit testing.
type headerLayout struct {
	BannerWidth  int
	RightBlockX  int // screen X where the right block starts
	Gap          int
}

// UsageLabels holds the display names of the three click-usage targets,
// derived from the configured IDE / AI CLI / terminal commands.
type UsageLabels struct {
	IDE      string
	AICli    string
	Terminal string
}

// normalized fills empty labels with generic fallbacks.
func (l UsageLabels) normalized() UsageLabels {
	if l.IDE == "" {
		l.IDE = "IDE"
	}
	if l.AICli == "" {
		l.AICli = "AI CLI"
	}
	if l.Terminal == "" {
		l.Terminal = "Terminal"
	}
	return l
}

// modes returns the usage/label pairs in display order.
func (l UsageLabels) modes() []struct {
	usage opener.ClickUsage
	name  string
} {
	l = l.normalized()
	return []struct {
		usage opener.ClickUsage
		name  string
	}{
		{opener.ClickIDE, l.IDE},
		{opener.ClickAICli, l.AICli},
		{opener.ClickTerminal, l.Terminal},
	}
}

// computeHeaderLayout is the single source of truth for header positioning.
func computeHeaderLayout(termWidth int, labels UsageLabels, updateAvailable ...string) headerLayout {
	bannerStyle := lipgloss.NewStyle().
		Foreground(ColorDarkGreen).
		Background(ColorBlack).
		Bold(true)

	var bannerLines []string
	for _, line := range ltsBanner {
		bannerLines = append(bannerLines, bannerStyle.Render(line))
	}
	versionLine := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack).Render("v" + version.Display())
	if len(updateAvailable) > 0 && updateAvailable[0] != "" {
		versionLine += lipgloss.NewStyle().Foreground(ColorDarkGreen).Background(ColorBlack).Bold(true).Render(" (Update Available)")
	}
	bannerLines = append(bannerLines, versionLine)
	bannerWidth := lipgloss.Width(strings.Join(bannerLines, "\n"))

	usageStr := renderClickUsage(opener.ClickIDE, labels, -1)
	viewStr := renderViewToggle("board", -1)
	statusLine := renderStatusLine("", false, 0)
	rightBlock := usageStr + "\n" + viewStr + "\n" + statusLine
	rightWidth := lipgloss.Width(rightBlock)

	availableWidth := termWidth - (MarginH * 2)
	gap := availableWidth - bannerWidth - rightWidth
	if gap < 2 {
		gap = 2
	}

	return headerLayout{
		BannerWidth: bannerWidth,
		RightBlockX: MarginH + bannerWidth + gap,
		Gap:         gap,
	}
}

// ClickUsageHitZones returns the screen coordinates of each click usage tab.
func ClickUsageHitZones(termWidth int, labels UsageLabels, updateAvailable ...string) (y int, zones []ClickUsageZone) {
	ua := ""
	if len(updateAvailable) > 0 {
		ua = updateAvailable[0]
	}
	layout := computeHeaderLayout(termWidth, labels, ua)

	labelW := lipgloss.Width(ClickUsageLabelStyle.Render("Click Usage:")) + 1 // +1 for space after label

	modes := labels.modes()

	y = 2 // 1 (header top margin) + 1 (rightBlock marginTop)
	curX := layout.RightBlockX + labelW
	var result []ClickUsageZone
	for i, m := range modes {
		w := lipgloss.Width(ClickUsageActiveStyle.Render(m.name))
		result = append(result, ClickUsageZone{X: curX, W: w, Usage: m.usage})
		curX += w
		if i < len(modes)-1 {
			curX += lipgloss.Width(BranchDimStyle.Render("│"))
		}
	}

	return y, result
}

// HeaderOpts configures header rendering.
type HeaderOpts struct {
	Loading            bool
	Frame              int
	StatusMsg          string
	VersionHovered     bool
	HoveredUsage       opener.ClickUsage // -1 = none hovered
	UpdateAvailable    string            // non-empty = version available (e.g. "2.6.1")
	UpdateBadgeHovered bool
	Layout             string // "board" or "explorer"
	HoveredView        int    // -1 = none, 0 = Board, 1 = Explorer
}

func RenderHeader(width int, activeUsage opener.ClickUsage, labels UsageLabels, opts ...HeaderOpts) string {
	var o HeaderOpts
	if len(opts) > 0 {
		o = opts[0]
	}

	// Render LTS banner
	bannerStyle := lipgloss.NewStyle().
		Foreground(ColorDarkGreen).
		Background(ColorBlack).
		Bold(true)

	var bannerLines []string
	for _, line := range ltsBanner {
		bannerLines = append(bannerLines, bannerStyle.Render(line))
	}
	// Version tag below banner
	var versionRendered string
	if o.VersionHovered {
		versionRendered = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Background(ColorBlack).
			Bold(true).
			Underline(true).
			Render("v" + version.Display())
	} else {
		versionRendered = lipgloss.NewStyle().
			Foreground(ColorDim).
			Background(ColorBlack).
			Render("v" + version.Display())
	}
	// Append "(Update Available)" badge if applicable
	if o.UpdateAvailable != "" {
		var badge string
		if o.UpdateBadgeHovered {
			badge = lipgloss.NewStyle().
				Foreground(ColorWhite).
				Background(ColorDarkGreen).
				Bold(true).
				Render(" (Update Available)")
		} else {
			badge = lipgloss.NewStyle().
				Foreground(ColorDarkGreen).
				Background(ColorBlack).
				Bold(true).
				Render(" (Update Available)")
		}
		versionRendered += badge
	}
	bannerLines = append(bannerLines, versionRendered)
	banner := strings.Join(bannerLines, "\n")

	// Render click usage toggle, view toggle, and status line
	usageStr := renderClickUsage(activeUsage, labels, o.HoveredUsage)
	viewStr := renderViewToggle(o.Layout, o.HoveredView)
	statusLine := renderStatusLine(o.StatusMsg, o.Loading, o.Frame)
	rightBlock := usageStr + "\n" + viewStr + "\n" + statusLine

	// Position: banner center-left, usage+status top-right
	layout := computeHeaderLayout(width, labels, o.UpdateAvailable)
	gap := layout.Gap

	// Place right block aligned to top of banner
	rightPadded := lipgloss.NewStyle().
		MarginTop(1).
		Render(rightBlock)

	headerRow := lipgloss.JoinHorizontal(
		lipgloss.Top,
		banner,
		strings.Repeat(" ", gap),
		rightPadded,
	)

	return lipgloss.NewStyle().
		Margin(1, MarginH, 0, MarginH).
		Render(headerRow)
}

func renderStatusLine(status string, loading bool, frame int) string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	valueStyle := lipgloss.NewStyle().Foreground(ColorClean).Background(ColorBlack)

	if status == "" {
		status = "Ready to manage"
	}

	line := labelStyle.Render("Status: ") + valueStyle.Render(status)
	if loading {
		line += " " + RenderSpinner(frame)
	}
	return line
}

func renderClickUsage(active opener.ClickUsage, labels UsageLabels, hoveredUsage opener.ClickUsage) string {
	label := ClickUsageLabelStyle.Render("Click Usage:")

	hoveredStyle := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorBlack).
		Bold(true).
		Underline(true).
		Padding(0, 1)

	modes := labels.modes()

	var parts []string
	parts = append(parts, label)
	parts = append(parts, " ")

	for i, m := range modes {
		var rendered string
		if m.usage == active {
			rendered = ClickUsageActiveStyle.Render(m.name)
		} else if m.usage == hoveredUsage {
			rendered = hoveredStyle.Render(m.name)
		} else {
			rendered = ClickUsageInactiveStyle.Render(m.name)
		}
		parts = append(parts, rendered)
		if i < len(modes)-1 {
			parts = append(parts, BranchDimStyle.Render("│"))
		}
	}

	tabKey := lipgloss.NewStyle().Foreground(ColorDarkGreen).Background(ColorBlack).Render("(tab)")
	parts = append(parts, " ", tabKey)

	return strings.Join(parts, "")
}

// viewToggleNames are the layout names in display order.
var viewToggleNames = [2]string{"Board", "Explorer"}

// viewToggleLabel is padded to align with "Click Usage:" above it.
const viewToggleLabel = "Layout:     "

// renderViewToggle renders the layout switcher line, aligned under the
// Click Usage line (the label is padded to the same width).
func renderViewToggle(activeLayout string, hoveredView int) string {
	label := ClickUsageLabelStyle.Render(viewToggleLabel)

	hoveredStyle := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorBlack).
		Bold(true).
		Underline(true).
		Padding(0, 1)

	activeIdx := 0
	if activeLayout == "explorer" {
		activeIdx = 1
	}

	parts := []string{label, " "}
	for i, name := range viewToggleNames {
		var rendered string
		switch {
		case i == activeIdx:
			rendered = ClickUsageActiveStyle.Render(name)
		case i == hoveredView:
			rendered = hoveredStyle.Render(name)
		default:
			rendered = ClickUsageInactiveStyle.Render(name)
		}
		parts = append(parts, rendered)
		if i < len(viewToggleNames)-1 {
			parts = append(parts, BranchDimStyle.Render("│"))
		}
	}
	key := lipgloss.NewStyle().Foreground(ColorDarkGreen).Background(ColorBlack).Render("(shift+tab)")
	parts = append(parts, " ", key)
	return strings.Join(parts, "")
}

// ViewToggleHitZones returns the screen coordinates of the Board/Explorer
// cells on the header's View line (one row below Click Usage).
func ViewToggleHitZones(termWidth int, labels UsageLabels, updateAvailable ...string) (y int, zones []ClickUsageZone) {
	ua := ""
	if len(updateAvailable) > 0 {
		ua = updateAvailable[0]
	}
	layout := computeHeaderLayout(termWidth, labels, ua)

	labelW := lipgloss.Width(ClickUsageLabelStyle.Render(viewToggleLabel)) + 1

	y = 3 // click usage is at y=2; the view line is directly below
	curX := layout.RightBlockX + labelW
	for i, name := range viewToggleNames {
		w := lipgloss.Width(ClickUsageActiveStyle.Render(name))
		zones = append(zones, ClickUsageZone{X: curX, W: w, Usage: opener.ClickUsage(i)})
		curX += w
		if i < len(viewToggleNames)-1 {
			curX += lipgloss.Width(BranchDimStyle.Render("│"))
		}
	}
	return y, zones
}
