package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ContextMenuItem represents a single action in the context menu.
type ContextMenuItem struct {
	Label  string
	Action HoverButton
}

// ContextMenuModel holds the state of an active context menu.
type ContextMenuModel struct {
	Active    bool
	Items     []ContextMenuItem
	CursorIdx int
	RepoIdx   int
	WTIdx     int // -2 = repo header, 0+ = worktree
	X, Y      int // screen position to render at
}

// RepoContextItems returns context menu items for a repo header.
func RepoContextItems(isMonorepo bool) []ContextMenuItem {
	if isMonorepo {
		return []ContextMenuItem{
			{Label: "Refresh", Action: BtnRefresh},
		}
	}
	return []ContextMenuItem{
		{Label: "Refresh", Action: BtnRefresh},
		{Label: "Change Basis Branch", Action: BtnBasis},
	}
}

// WorktreeContextItems returns context menu items for a worktree.
func WorktreeContextItems() []ContextMenuItem {
	return []ContextMenuItem{
		{Label: "Rebase", Action: BtnRebase},
		{Label: "Rename Branch", Action: BtnRename},
		{Label: "Delete", Action: BtnDelete},
	}
}

// RenderContextMenu renders the floating context menu.
func RenderContextMenu(menu ContextMenuModel, screenWidth, screenHeight int) string {
	if !menu.Active {
		return ""
	}

	menuStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorGray).
		BorderBackground(ColorBlack).
		Background(ColorBlack).
		Padding(0, 1)

	cursorStyle := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorDarkGreen).
		Bold(true).
		Padding(0, 1)

	itemStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack).
		Padding(0, 1)

	deleteStyle := lipgloss.NewStyle().
		Foreground(ColorRed).
		Background(ColorBlack).
		Padding(0, 1)

	var lines []string
	for i, item := range menu.Items {
		if i == menu.CursorIdx {
			lines = append(lines, cursorStyle.Render("▸ "+item.Label))
		} else if item.Action == BtnDelete {
			lines = append(lines, deleteStyle.Render("  "+item.Label))
		} else {
			lines = append(lines, itemStyle.Render("  "+item.Label))
		}
	}

	content := strings.Join(lines, "\n")
	rendered := menuStyle.Render(content)

	// Position the menu near the click point
	menuW := lipgloss.Width(rendered)
	menuH := lipgloss.Height(rendered)

	posX := menu.X
	posY := menu.Y + 1 // below the clicked line

	// Keep within screen bounds
	if posX+menuW > screenWidth {
		posX = screenWidth - menuW - 1
	}
	if posX < 0 {
		posX = 0
	}
	if posY+menuH > screenHeight {
		posY = menu.Y - menuH // above instead
	}

	return lipgloss.Place(
		screenWidth, screenHeight,
		lipgloss.Left, lipgloss.Top,
		rendered,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
	)
}
