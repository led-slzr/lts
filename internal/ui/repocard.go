package ui

import (
	"lts-revamp/internal/git"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// extractChangedCount pulls the number from status text like "7 changed | new".
// Returns "" if no changed count found.
func extractChangedCount(statusText string) string {
	if idx := strings.Index(statusText, " changed"); idx > 0 {
		return "(" + statusText[:idx] + ")"
	}
	return ""
}

// HoverButton identifies which inline button is hovered.
type HoverButton int

const (
	BtnNone HoverButton = iota
	// Inline context trigger
	BtnContextMenu
	// Context menu actions (repo)
	BtnBasis
	BtnRefresh
	// Context menu actions (worktree)
	BtnRebase
	BtnRename
	BtnDelete
	// Footer buttons
	BtnRefreshAll
	BtnCleanupMerged
	BtnExit
	// Create button
	BtnCreateWT
	// Settings button
	BtnSettings
	// Migrate button
	BtnMigrate
)

// innerWidth returns the usable content width inside a card.
// Card border = 1 char each side, padding = 1 char each side = 4 total.
func innerWidth(cardWidth int) int {
	return cardWidth - 4
}

// truncate ensures a rendered string doesn't exceed maxWidth visible characters.
func truncate(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(maxWidth).Render(s)
}

// statusStyle returns the lipgloss style for a worktree based on its status.
// Matches lts.sh color mapping exactly.
func statusStyle(status git.WTStatus) lipgloss.Style {
	base := lipgloss.NewStyle().Background(ColorBlack)
	switch status {
	case git.StatusMissing:
		return base.Foreground(ColorRed)
	case git.StatusDiverged:
		return base.Foreground(ColorRed)
	case git.StatusChanged:
		return base.Foreground(ColorCyan)
	case git.StatusMergedCleanable:
		return base.Foreground(ColorGreen)
	case git.StatusMergedDirty:
		return base.Foreground(ColorCyan)
	case git.StatusToPush:
		return base.Foreground(ColorYellow)
	case git.StatusToPull:
		return base.Foreground(ColorYellow)
	case git.StatusNoRemote:
		return base.Foreground(ColorDim)
	case git.StatusNew:
		return base.Foreground(ColorBlue)
	case git.StatusNewDirty:
		return base.Foreground(ColorCyan)
	case git.StatusClean:
		return base.Foreground(ColorClean)
	}
	return base.Foreground(ColorClean)
}

// RenderCard renders a single repository card.
// focusedWT: index of hovered worktree (-1 = none, -2 = repo header hovered)
func RenderCard(repo git.Repo, cardWidth int, focused bool, focusedWT int, hoveredBtn HoverButton) string {
	iw := innerWidth(cardWidth)

	// Migration card: yellow border with centered message and migrate button
	if repo.NeedsMigration {
		return renderMigrationCard(repo, cardWidth, iw, focused, focusedWT, hoveredBtn)
	}

	var lines []string

	// Header line
	isHeaderHovered := focused && focusedWT == -2
	var header string
	if repo.IsMonorepo {
		// Monorepo card: show name with repo count
		monoLabel := lipgloss.NewStyle().Foreground(ColorMagenta).Background(ColorBlack).Render("mono")
		header = RepoNameStyle.Render(repo.Name) + " " + BranchDimStyle.Render("(") + monoLabel + BranchDimStyle.Render(")")
	} else if isHeaderHovered && repo.Path != "" {
		// Highlighted header (hovered, clickable repo)
		header = WTHighlightStyle.Underline(true).Render(repo.Name+" ("+repo.MainBranch+")")
	} else {
		header = RepoNameStyle.Render(repo.Name) + " " + BranchDimStyle.Render("("+repo.MainBranch+")")
	}

	if isHeaderHovered {
		triggerBtn := renderInlineBtn("[⋮]", hoveredBtn == BtnContextMenu)
		header = rightAlignButtons(header, triggerBtn, iw)
	}

	lines = append(lines, truncate(header, iw))

	// Worktree lines — branch name colored by status, no inline status text
	for i, wt := range repo.Worktrees {
		isLast := i == len(repo.Worktrees)-1
		treeChar := "├"
		if isLast {
			treeChar = "└"
		}

		isHovered := focused && focusedWT == i

		branchDisplay := wt.Branch
		if branchDisplay == "" {
			branchDisplay = wt.Name
		}

		// Color the branch name based on status, append changed count if any
		changedBadge := extractChangedCount(wt.StatusText)
		branchStyled := statusStyle(wt.Status).Render(branchDisplay)
		if changedBadge != "" {
			branchStyled += " " + statusStyle(wt.Status).Render(changedBadge)
		}

		var line string
		if isHovered {
			hoverDisplay := branchDisplay
			if changedBadge != "" {
				hoverDisplay += " " + changedBadge
			}
			branchStyled = WTHighlightStyle.Render(hoverDisplay)
			triggerBtn := renderInlineBtn("[⋮]", hoveredBtn == BtnContextMenu)
			textPart := TreeCharStyle.Render(treeChar) + branchStyled
			line = rightAlignButtons(textPart, triggerBtn, iw)
		} else {
			line = TreeCharStyle.Render(treeChar) + branchStyled
		}

		lines = append(lines, truncate(line, iw))
	}

	content := strings.Join(lines, "\n")

	// Apply card border style
	var style lipgloss.Style
	if focused {
		style = CardBorderFocused.Width(cardWidth - 2)
	} else {
		style = CardBorderNormal.Width(cardWidth - 2)
	}

	return style.Render(content)
}

// renderMigrationCard renders a card for a repo that needs migration to LTS.
// Shows yellow border, centered message, branch name, and migrate button.
// If the repo already has LTS worktrees, they are shown below the migration notice.
func renderMigrationCard(repo git.Repo, cardWidth, iw int, focused bool, focusedWT int, hoveredBtn HoverButton) string {
	yellowText := lipgloss.NewStyle().Foreground(ColorYellow).Background(ColorBlack)
	dimText := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	branchStyle := lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorBlack).Bold(true)
	centerStyle := lipgloss.NewStyle().Width(iw).Align(lipgloss.Center).Background(ColorBlack)

	var lines []string

	// Header with repo name
	header := RepoNameStyle.Render(repo.Name) + " " + BranchDimStyle.Render("("+repo.MainBranch+")")
	lines = append(lines, truncate(header, iw))

	// Empty line
	lines = append(lines, "")

	// Branch name (truncated for narrow cards)
	lines = append(lines, centerStyle.Render(truncate(branchStyle.Render(repo.MigrationBranch), iw)))

	// Warning message
	lines = append(lines, centerStyle.Render(yellowText.Render("Existing work detected")))

	// Reason (truncated for narrow cards)
	lines = append(lines, centerStyle.Render(truncate(dimText.Render(repo.MigrationReason), iw)))

	// Empty line
	lines = append(lines, "")

	// Migrate button
	var btn string
	if focused && hoveredBtn == BtnMigrate {
		btn = MigrateBtnHoverStyle.Render(" Migrate to LTS ")
	} else {
		btn = MigrateBtnStyle.Render(" Migrate to LTS ")
	}
	lines = append(lines, centerStyle.Render(btn))

	// Show existing worktrees if any (below the migration notice)
	if len(repo.Worktrees) > 0 {
		lines = append(lines, "") // separator
		for i, wt := range repo.Worktrees {
			isLast := i == len(repo.Worktrees)-1
			treeChar := "├"
			if isLast {
				treeChar = "└"
			}

			isHovered := focused && focusedWT == i

			branchDisplay := wt.Branch
			if branchDisplay == "" {
				branchDisplay = wt.Name
			}
			changedBadge := extractChangedCount(wt.StatusText)

			var line string
			if isHovered {
				hoverDisplay := branchDisplay
				if changedBadge != "" {
					hoverDisplay += " " + changedBadge
				}
				line = TreeCharStyle.Render(treeChar) + WTHighlightStyle.Render(hoverDisplay)
			} else {
				branchStyled := statusStyle(wt.Status).Render(branchDisplay)
				if changedBadge != "" {
					branchStyled += " " + statusStyle(wt.Status).Render(changedBadge)
				}
				line = TreeCharStyle.Render(treeChar) + branchStyled
			}
			lines = append(lines, truncate(line, iw))
		}
	} else {
		// Empty line for padding when no worktrees
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")

	style := CardBorderMigration.Width(cardWidth - 2)
	return style.Render(content)
}

// RenderStatusLegend renders a compact color legend for worktree statuses.
func RenderStatusLegend(width int) string {
	items := []struct {
		color lipgloss.Color
		label string
	}{
		{ColorClean, "clean"},
		{ColorCyan, "changed"},
		{ColorYellow, "to push/pull"},
		{ColorGreen, "cleanable"},
		{ColorBlue, "new"},
		{ColorRed, "diverged"},
		{ColorDim, "no remote"},
	}

	var parts []string
	for _, item := range items {
		dot := lipgloss.NewStyle().Foreground(item.color).Render("●")
		label := lipgloss.NewStyle().Foreground(ColorDim).Render(item.label)
		parts = append(parts, dot+" "+label)
	}

	legend := strings.Join(parts, "  ")
	return lipgloss.NewStyle().
		Width(width - (MarginH * 2)).
		Align(lipgloss.Center).
		Margin(0, MarginH).
		Render(legend)
}

// rightAlignButtons places buttons at the right edge of the content area.
// Text is truncated if needed to make room, and spaces fill the gap.
func rightAlignButtons(text, buttons string, contentWidth int) string {
	buttonsW := lipgloss.Width(buttons)
	textW := lipgloss.Width(text)

	maxTextW := contentWidth - buttonsW - 1 // at least 1 space gap
	if textW > maxTextW {
		text = truncate(text, maxTextW)
		textW = lipgloss.Width(text)
	}

	gap := contentWidth - textW - buttonsW
	if gap < 1 {
		gap = 1
	}

	spacer := lipgloss.NewStyle().Background(ColorBlack).Render(strings.Repeat(" ", gap))
	return text + spacer + buttons
}

func renderInlineBtn(label string, hovered bool) string {
	if hovered {
		return InlineBtnHoverStyle.Render(label)
	}
	return InlineBtnStyle.Render(label)
}
