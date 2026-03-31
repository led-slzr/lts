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
	versionText := "v" + version.Version
	return MarginH, 1 + len(ltsBanner), len(versionText)
}

// ReleaseURL returns the GitHub releases URL for the current version.
func ReleaseURL() string {
	return fmt.Sprintf("https://github.com/led-slzr/lts/releases/tag/v%s", version.Version)
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

// computeHeaderLayout is the single source of truth for header positioning.
func computeHeaderLayout(termWidth int, aiCliLabel string) headerLayout {
	bannerStyle := lipgloss.NewStyle().
		Foreground(ColorDarkGreen).
		Background(ColorBlack).
		Bold(true)

	var bannerLines []string
	for _, line := range ltsBanner {
		bannerLines = append(bannerLines, bannerStyle.Render(line))
	}
	bannerLines = append(bannerLines, lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack).Render("v"+version.Version))
	bannerWidth := lipgloss.Width(strings.Join(bannerLines, "\n"))

	usageStr := renderClickUsage(opener.ClickIDE, aiCliLabel, -1)
	statusLine := renderStatusLine("", false, 0)
	rightBlock := usageStr + "\n" + statusLine
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
func ClickUsageHitZones(termWidth int, aiCliLabel string) (y int, zones []ClickUsageZone) {
	if aiCliLabel == "" {
		aiCliLabel = "AI CLI"
	}

	layout := computeHeaderLayout(termWidth, aiCliLabel)

	labelW := lipgloss.Width(ClickUsageLabelStyle.Render("Click Usage:")) + 1 // +1 for space after label

	modes := []struct {
		usage opener.ClickUsage
		name  string
	}{
		{opener.ClickIDE, "IDE"},
		{opener.ClickAICli, aiCliLabel},
		{opener.ClickTerminal, "Terminal"},
	}

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
	Loading        bool
	Frame          int
	StatusMsg      string
	VersionHovered bool
	HoveredUsage   opener.ClickUsage // -1 = none hovered
}

func RenderHeader(width int, activeUsage opener.ClickUsage, aiCliLabel string, opts ...HeaderOpts) string {
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
			Render("v" + version.Version)
	} else {
		versionRendered = lipgloss.NewStyle().
			Foreground(ColorDim).
			Background(ColorBlack).
			Render("v" + version.Version)
	}
	bannerLines = append(bannerLines, versionRendered)
	banner := strings.Join(bannerLines, "\n")

	// Render click usage toggle
	usageStr := renderClickUsage(activeUsage, aiCliLabel, o.HoveredUsage)

	// Render status line below usage
	statusLine := renderStatusLine(o.StatusMsg, o.Loading, o.Frame)
	rightBlock := usageStr + "\n" + statusLine

	// Position: banner center-left, usage+status top-right
	layout := computeHeaderLayout(width, aiCliLabel)
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

func renderClickUsage(active opener.ClickUsage, aiCliLabel string, hoveredUsage opener.ClickUsage) string {
	label := ClickUsageLabelStyle.Render("Click Usage:")

	if aiCliLabel == "" {
		aiCliLabel = "AI CLI"
	}

	hoveredStyle := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorBlack).
		Bold(true).
		Underline(true).
		Padding(0, 1)

	modes := []struct {
		usage opener.ClickUsage
		name  string
	}{
		{opener.ClickIDE, "IDE"},
		{opener.ClickAICli, aiCliLabel},
		{opener.ClickTerminal, "Terminal"},
	}

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
