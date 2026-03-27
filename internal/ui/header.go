package ui

import (
	"lts-revamp/internal/opener"
	"lts-revamp/internal/version"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Big block-letter LTS title
var ltsBanner = []string{
	"██╗     ████████╗███████╗",
	"██║     ╚══██╔══╝██╔════╝",
	"██║        ██║   ███████╗",
	"██║        ██║   ╚════██║",
	"███████╗   ██║   ███████║",
	"╚══════╝   ╚═╝   ╚══════╝",
}

func RenderHeader(width int, activeUsage opener.ClickUsage, aiCliLabel string) string {
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
	versionStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack)
	bannerLines = append(bannerLines, versionStyle.Render("v"+version.Version))
	banner := strings.Join(bannerLines, "\n")

	// Render click usage toggle
	usageStr := renderClickUsage(activeUsage, aiCliLabel)

	// Position: banner center-left, usage top-right
	bannerWidth := lipgloss.Width(banner)
	usageWidth := lipgloss.Width(usageStr)

	availableWidth := width - (MarginH * 2)
	gap := availableWidth - bannerWidth - usageWidth
	if gap < 2 {
		gap = 2
	}

	// Place usage aligned to top of banner
	usagePadded := lipgloss.NewStyle().
		MarginTop(1).
		Render(usageStr)

	headerRow := lipgloss.JoinHorizontal(
		lipgloss.Top,
		banner,
		strings.Repeat(" ", gap),
		usagePadded,
	)

	return lipgloss.NewStyle().
		Margin(1, MarginH, 0, MarginH).
		Render(headerRow)
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

	return strings.Join(parts, "")
}
