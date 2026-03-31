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

// HeaderOpts configures header rendering.
type HeaderOpts struct {
	Loading        bool
	Frame          int
	StatusMsg      string
	VersionHovered bool
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
	usageStr := renderClickUsage(activeUsage, aiCliLabel)

	// Render status line below usage
	statusLine := renderStatusLine(o.StatusMsg, o.Loading, o.Frame)
	rightBlock := usageStr + "\n" + statusLine

	// Position: banner center-left, usage+status top-right
	bannerWidth := lipgloss.Width(banner)
	rightWidth := lipgloss.Width(rightBlock)

	availableWidth := width - (MarginH * 2)
	gap := availableWidth - bannerWidth - rightWidth
	if gap < 2 {
		gap = 2
	}

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

func renderClickUsage(active opener.ClickUsage, aiCliLabel string) string {
	label := ClickUsageLabelStyle.Render("Click Usage:")

	if aiCliLabel == "" {
		aiCliLabel = "AI CLI"
	}

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
		} else {
			rendered = ClickUsageInactiveStyle.Render(m.name)
		}
		parts = append(parts, rendered)
		if i < len(modes)-1 {
			parts = append(parts, BranchDimStyle.Render("│"))
		}
	}

	tabHint := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack).Render("  ← Tab")
	parts = append(parts, tabHint)

	return strings.Join(parts, "")
}
