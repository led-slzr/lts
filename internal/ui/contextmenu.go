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

// RenderContextMenu renders the context menu as a centered dialog.
func RenderContextMenu(menu ContextMenuModel, screenWidth, screenHeight int) string {
	if !menu.Active {
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorGreen).
		Background(ColorBlack)

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

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack)

	var lines []string
	lines = append(lines, titleStyle.Render("Actions"))
	lines = append(lines, "")

	for i, item := range menu.Items {
		if i == menu.CursorIdx {
			lines = append(lines, cursorStyle.Render("▸ "+item.Label))
		} else if item.Action == BtnDelete {
			lines = append(lines, deleteStyle.Render("  "+item.Label))
		} else {
			lines = append(lines, itemStyle.Render("  "+item.Label))
		}
	}

	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("↑/↓ navigate • enter select • esc close"))

	content := strings.Join(lines, "\n")
	return ModalStyle.Width(40).Render(content)
}

// RenderContextMenuPlaced renders the context menu centered on screen.
func RenderContextMenuPlaced(menu ContextMenuModel, screenWidth, screenHeight int) string {
	modal := RenderContextMenu(menu, screenWidth, screenHeight)
	return lipgloss.Place(screenWidth, screenHeight, lipgloss.Center, lipgloss.Center, modal)
}
