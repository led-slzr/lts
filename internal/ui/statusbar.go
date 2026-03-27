package ui

import "github.com/charmbracelet/lipgloss"

func RenderStatusBar(msg string, width int) string {
	if msg == "" {
		return ""
	}
	return lipgloss.NewStyle().
		Width(width - (MarginH * 2)).
		Align(lipgloss.Center).
		Margin(0, MarginH).
		Render(StatusBarStyle.Render(msg))
}
