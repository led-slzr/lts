package ui

import "github.com/charmbracelet/lipgloss"

// RenderScrollIndicator renders a subtle hint showing scroll direction availability.
func RenderScrollIndicator(width int, canUp, canDown bool) string {
	style := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)

	hint := ""
	if canUp && canDown {
		hint = "↑↓ scroll"
	} else if canUp {
		hint = "↑ scroll"
	} else if canDown {
		hint = "↓ scroll"
	}

	return lipgloss.NewStyle().
		Width(width - (MarginH * 2)).
		Align(lipgloss.Center).
		Margin(0, MarginH).
		Render(style.Render(hint))
}
