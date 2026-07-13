package ui

import (
	"fmt"
	"strings"

	"lts-revamp/internal/git"

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
	Info      []string // pre-styled detail lines shown above the actions
	CursorIdx int
	X, Y      int // screen position to render at
}

// ItemsStartOffset is the content-region row of the first action item —
// mouse hit-testing must match the render below.
func (menu ContextMenuModel) ItemsStartOffset() int {
	if len(menu.Info) > 0 {
		return 2 + len(menu.Info) + 1 // title, blank, info..., blank
	}
	return 2 // title, blank
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
// canConvert offers promoting a single-repo worktree into a mono group
// (other plain repos must exist); mono worktrees offer the reverse split.
func WorktreeContextItems(isMonorepo, hasSession, canPR, canConvert bool) []ContextMenuItem {
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
	)
	if !isMonorepo && canConvert {
		items = append(items, ContextMenuItem{Label: "Add to Mono Group", Action: BtnConvertMono})
	}
	if isMonorepo {
		items = append(items, ContextMenuItem{Label: "Split Mono Group", Action: BtnReduceMono})
	}
	items = append(items, ContextMenuItem{Label: "Delete", Action: BtnDelete})
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
	if len(menu.Info) > 0 {
		// Info lines must stay single-row — a wrap would shift the item
		// rows out from under the mouse hit-testing
		for _, info := range menu.Info {
			lines = append(lines, truncate(info, 42))
		}
		lines = append(lines, "")
	}

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
	return ModalStyle.Width(46).Render(content)
}

// RenderContextMenuPlaced renders the context menu centered on screen.
func RenderContextMenuPlaced(menu ContextMenuModel, screenWidth, screenHeight int) string {
	modal := RenderContextMenu(menu, screenWidth, screenHeight)
	return lipgloss.Place(screenWidth, screenHeight, lipgloss.Center, lipgloss.Center, modal)
}

// WorktreeMenuInfo builds the detail block for a worktree context menu —
// the same data the Explorer sheet shows, so both layouts tell one story.
func WorktreeMenuInfo(wt git.Worktree, size WTSize, hasSession bool) []string {
	dim := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	cyan := lipgloss.NewStyle().Foreground(ColorCyan).Background(ColorBlack)
	teal := lipgloss.NewStyle().Foreground(ColorTeal).Background(ColorBlack)

	age := "active " + formatAge(wt.LastActivity)
	if wt.CreatedAt > 0 {
		age += " · created " + formatAge(wt.CreatedAt)
	}
	lines := []string{
		dim.Render("branch  ") + cyan.Render(truncatePlain(wt.Branch, 30)),
		dim.Render("status  ") + statusStyle(wt.Status).Render(wt.StatusText),
		dim.Render("size    ") + dim.Render(formatWTSize(size)),
		dim.Render("age     ") + dim.Render(age),
	}
	if hasSession {
		lines = append(lines, dim.Render("tmux    ")+teal.Render("● session live"))
	} else {
		lines = append(lines, dim.Render("tmux    ")+dim.Render("no session"))
	}
	return lines
}

// RepoMenuInfo builds the detail block for a repo-header context menu.
func RepoMenuInfo(repo git.Repo, basis string) []string {
	dim := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	white := lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Background(ColorBlack)

	name := repo.Name
	if repo.Hidden {
		name += " (hidden)"
	}
	kind := "repo"
	if repo.IsMonorepo {
		kind = "mono group · " + strings.Join(repo.RepoNames, ", ")
	}
	return []string{
		dim.Render("repo    ") + white.Render(truncatePlain(name, 26)),
		dim.Render("kind    ") + dim.Render(truncatePlain(kind, 26)),
		dim.Render("basis   ") + dim.Render(basis),
		dim.Render("trees   ") + dim.Render(fmt.Sprintf("%d worktree(s)", len(repo.Worktrees))),
	}
}
