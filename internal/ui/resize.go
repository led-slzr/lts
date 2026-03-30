package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// RenderResizePrompt renders a centered message asking the user to resize their terminal.
func RenderResizePrompt(width, height, minWidth, minHeight int) string {
	titleStyle := lipgloss.NewStyle().
		Foreground(ColorYellow).
		Background(ColorBlack).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack)

	sizeStyle := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorBlack)

	treeStyle := lipgloss.NewStyle().
		Foreground(ColorDarkGreen).
		Background(ColorBlack)

	tree := treeStyle.Render("  ○\n  │\n  ·")

	content := tree + "\n\n"
	content += titleStyle.Render("Terminal too small") + "\n\n"
	content += dimStyle.Render("Please resize your terminal to at least:") + "\n"
	content += sizeStyle.Render(fmt.Sprintf("  %d x %d", minWidth, minHeight)) + "\n\n"
	content += dimStyle.Render(fmt.Sprintf("Current: %d x %d", width, height))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}
