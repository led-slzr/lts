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
// The target repo/worktree is snapshotted by the app when the menu opens.
type ContextMenuModel struct {
	Active    bool
	Items     []ContextMenuItem
	CursorIdx int
	X, Y      int // screen position to render at
}

// RepoContextItems returns context menu items for a repo header.
// canHibernate adds the hibernate entry (GitHub-remote repo with gh ready —
// the premise is that GitHub holds everything the local copy does).
// canHide offers hiding (worktree-less repos only); a hidden repo is
// display-only — its single action is coming back.
func RepoContextItems(isMonorepo, canHibernate, canHide, hidden bool) []ContextMenuItem {
	if hidden {
		return []ContextMenuItem{
			{Label: "Unhide Repo", Action: BtnUnhideRepo},
		}
	}
	if isMonorepo {
		return []ContextMenuItem{
			{Label: "Refresh", Action: BtnRefresh},
		}
	}
	items := []ContextMenuItem{
		{Label: "Refresh", Action: BtnRefresh},
		{Label: "Change Basis Branch", Action: BtnBasis},
	}
	if canHibernate {
		items = append(items, ContextMenuItem{Label: "Hibernate Repo", Action: BtnHibernate})
	}
	if canHide {
		items = append(items, ContextMenuItem{Label: "Hide Repo", Action: BtnHideRepo})
	}
	return items
}

// WorktreeContextItems returns context menu items for a worktree.
// Monorepo worktree paths are branch subdirectories (containers of per-repo
// worktrees, not git worktrees themselves), so Rebase can't run there —
// per-repo monorepo rebase is a future feature. hasSession adds the tmux
// session kill entry.
func WorktreeContextItems(isMonorepo, hasSession, canPR bool) []ContextMenuItem {
	items := []ContextMenuItem{}
	if !isMonorepo {
		items = append(items, ContextMenuItem{Label: "Rebase", Action: BtnRebase})
	}
	if canPR {
		items = append(items, ContextMenuItem{Label: "Create PR", Action: BtnCreatePR})
	}
	items = append(items,
		ContextMenuItem{Label: "Rename Branch", Action: BtnRename},
		ContextMenuItem{Label: "Clean Modules", Action: BtnCleanModules},
		ContextMenuItem{Label: "Delete", Action: BtnDelete},
	)
	if hasSession {
		items = append(items, ContextMenuItem{Label: "Kill Tmux Session", Action: BtnKillSession})
	}
	return items
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
		Background(ColorSelBg).
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
		} else if item.Action == BtnDelete || item.Action == BtnHibernate {
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
