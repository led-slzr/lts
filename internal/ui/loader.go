package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Tree growth animation frames - a binary tree growing from seed to full canopy.
// Each frame is the same height (padded) to prevent layout jumps.
var treeFrames = [][]string{
	// Frame 0: seed
	{
		"",
		"",
		"",
		"",
		"",
		"",
		"         ·         ",
	},
	// Frame 1: sprout
	{
		"",
		"",
		"",
		"",
		"",
		"         │         ",
		"         ·         ",
	},
	// Frame 2: sapling
	{
		"",
		"",
		"",
		"",
		"        �│         ",
		"        ╱ ╲        ",
		"         ·         ",
	},
	// Frame 3: young tree
	{
		"",
		"",
		"",
		"       ╱   ╲       ",
		"      ○     ○      ",
		"        ╲ ╱        ",
		"         ·         ",
	},
	// Frame 4: growing
	{
		"",
		"      ╱     ╲      ",
		"     ○       ○     ",
		"      ╲     ╱      ",
		"       ╲   ╱       ",
		"        ╲ ╱        ",
		"         ·         ",
	},
	// Frame 5: branching
	{
		"    ╱  ╲   ╱  ╲    ",
		"   ○    ○ ○    ○   ",
		"    ╲  ╱   ╲  ╱    ",
		"      ╲     ╱      ",
		"       ╲   ╱       ",
		"        ╲ ╱        ",
		"         ·         ",
	},
	// Frame 6: full canopy
	{
		"  ╱╲  ╱╲   ╱╲  ╱╲  ",
		" ○  ○○  ○ ○  ○○  ○ ",
		"  ╲╱  ╲╱   ╲╱  ╲╱  ",
		"    ╲  ╱     ╲  ╱   ",
		"      ╲       ╱     ",
		"       ╲     ╱      ",
		"         ·          ",
	},
	// Frame 7: leaves appear (full tree with dots)
	{
		" ◆◆◆◆ ◆◆◆◆◆◆◆ ◆◆◆◆",
		" ○  ○○  ○ ○  ○○  ○ ",
		" ◆◆◆◆ ◆◆◆◆◆◆◆ ◆◆◆◆",
		"    ╲  ╱     ╲  ╱   ",
		"      ╲       ╱     ",
		"       ╲     ╱      ",
		"         ·          ",
	},
}

// Simpler, cleaner worktree-style growth animation
var worktreeFrames = [][]string{
	// Frame 0
	{
		"",
		"",
		"",
		"",
		"      ·",
	},
	// Frame 1
	{
		"",
		"",
		"",
		"      │",
		"      ·",
	},
	// Frame 2
	{
		"",
		"",
		"      ○",
		"      │",
		"      ·",
	},
	// Frame 3
	{
		"",
		"      ○",
		"      ├── ○",
		"      │",
		"      ·",
	},
	// Frame 4
	{
		"      ○",
		"      ├── ○",
		"      ├── ○",
		"      │",
		"      ·",
	},
	// Frame 5
	{
		"      ○",
		"      ├── ○",
		"      │   └── ○",
		"      ├── ○",
		"      ·",
	},
	// Frame 6
	{
		"      ○",
		"      ├── ○",
		"      │   ├── ○",
		"      │   └── ○",
		"      ├── ○",
	},
	// Frame 7
	{
		"      ○──────┐",
		"      ├── ○  │",
		"      │   ├── ○",
		"      │   └── ○",
		"      ├── ○",
	},
	// Frame 8: full worktree
	{
		"      ○──────┐",
		"      ├── ○  ├── ○",
		"      │   ├── ○",
		"      │   └── ○",
		"      └── ○",
	},
}

// RenderLoader renders a tree-growing loading animation.
// frame cycles from 0 to len(frames)-1, then optionally loops.
func RenderLoader(width int, frame int, message string) string {
	frames := worktreeFrames
	idx := frame % len(frames)

	// Style the tree lines
	treeLines := frames[idx]
	var styledLines []string

	bg := lipgloss.NewStyle().Background(ColorBlack)
	nodeStyle := bg.Foreground(ColorGreen).Bold(true)
	branchStyle := bg.Foreground(ColorDarkGreen)
	seedStyle := bg.Foreground(ColorYellow).Bold(true)

	for _, line := range treeLines {
		if line == "" {
			styledLines = append(styledLines, "")
			continue
		}
		// Color nodes, branches, and seed differently
		styled := ""
		for _, ch := range line {
			switch ch {
			case '○':
				styled += nodeStyle.Render("○")
			case '·':
				styled += seedStyle.Render("●")
			case '◆':
				styled += bg.Foreground(ColorGreen).Render("◆")
			case '├', '└', '┐', '┤', '│', '─':
				styled += branchStyle.Render(string(ch))
			case '╱', '╲':
				styled += branchStyle.Render(string(ch))
			default:
				styled += string(ch)
			}
		}
		styledLines = append(styledLines, styled)
	}

	tree := strings.Join(styledLines, "\n")

	// Loading message below tree
	msgStyled := bg.
		Foreground(ColorDim).
		Italic(true).
		Render(message)

	// Dots animation based on frame
	dots := strings.Repeat(".", (frame%3)+1)
	dotsStyled := bg.Foreground(ColorGreen).Render(dots)

	content := tree + "\n\n" + msgStyled + dotsStyled

	return lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Margin(2, 0).
		Render(content)
}

// LoaderFrameCount returns the total number of animation frames.
func LoaderFrameCount() int {
	return len(worktreeFrames)
}
