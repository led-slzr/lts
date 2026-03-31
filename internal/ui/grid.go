package ui

import (
	"fmt"
	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	MinCardWidth = 28
	MaxCardWidth = 44
	CardGap      = 2
	GridMarginY  = 1 // top margin of grid container
)

// HitZoneType identifies the type of interactive element.
type HitZoneType int

const (
	ZoneCard HitZoneType = iota
	ZoneRepoHeader
	ZoneWorktree
	ZoneRepoBtn
	ZoneWTBtn
	ZoneCreateBtn
	ZoneFooterBtn
	ZoneMigrateBtn
	ZoneHistoryItem
)

// HitZone represents a clickable/hoverable region on screen.
type HitZone struct {
	X, Y, W, H int
	Type        HitZoneType
	RepoIdx     int
	WTIdx       int // -1 for card-level, -2 for header
	Button      HoverButton
}

// GridResult contains the rendered grid and its hit zones.
type GridResult struct {
	View      string
	HitZones  []HitZone
	Rows      int
	Cols      int
	CardWidth int
}

// LayoutGrid arranges repo cards in a responsive grid.
// gridYOffset is the screen Y coordinate where the grid section starts (before its own margin).
var CurrentWorkDir string // set by the app to filter history suggestions

func LayoutGrid(repos []git.Repo, termWidth int, gridYOffset int, focusedCard int, focusedWT int, hoveredBtn HoverButton, hoveredHistory int) GridResult {
	if len(repos) == 0 {
		return renderEmptyState(termWidth, gridYOffset, hoveredHistory)
	}

	availWidth := termWidth - (MarginH * 2)

	// Calculate columns
	cols := availWidth / (MinCardWidth + CardGap)
	if cols < 1 {
		cols = 1
	}
	if cols > len(repos) {
		cols = len(repos)
	}

	// Calculate actual card width
	cardWidth := (availWidth - (CardGap * (cols - 1))) / cols
	if cardWidth > MaxCardWidth {
		cardWidth = MaxCardWidth
	}
	if cardWidth < MinCardWidth {
		cardWidth = MinCardWidth
	}

	// Arrange into rows
	numRows := (len(repos) + cols - 1) / cols

	var hitZones []HitZone
	var renderedRows []string

	// The grid content will be wrapped with Margin(GridMarginY, MarginH).
	// So the actual screen position of grid content is:
	//   screenY = gridYOffset + GridMarginY + contentY
	//   screenX = MarginH + contentX
	// Hit zones must be in SCREEN coordinates.
	baseY := gridYOffset + GridMarginY
	baseX := MarginH

	contentY := 0 // tracks Y position within grid content

	for row := 0; row < numRows; row++ {
		start := row * cols
		end := start + cols
		if end > len(repos) {
			end = len(repos)
		}

		rowCards := make([]string, 0, end-start)
		rowCardHeights := make([]int, 0, end-start)

		for i := start; i < end; i++ {
			isFocused := focusedCard == i
			wtIdx := -1
			btn := BtnNone
			if isFocused {
				wtIdx = focusedWT
				btn = hoveredBtn
			}
			card := RenderCard(repos[i], cardWidth, isFocused, wtIdx, btn)
			rowCards = append(rowCards, card)
			rowCardHeights = append(rowCardHeights, lipgloss.Height(card))
		}

		// Equalize heights in row
		maxH := 0
		for _, h := range rowCardHeights {
			if h > maxH {
				maxH = h
			}
		}

		equalizedCards := make([]string, len(rowCards))
		for i, card := range rowCards {
			h := rowCardHeights[i]
			if h < maxH {
				card = card + strings.Repeat("\n", maxH-h)
			}
			equalizedCards[i] = card
		}

		// Build hit zones for this row.
		// Each card has a RoundedBorder (1 line top, 1 line bottom, 1 char left, 1 char right)
		// and Padding(0, 1) (0 vertical, 1 horizontal).
		// So card content starts at:
		//   contentStartY = border_top = 1 line
		//   contentStartX = border_left(1) + padding_left(1) = 2 chars
		// Repo header is the first content line.
		// Worktree i is at content line 1 + i.

		contentX := 0 // X position within grid content for this card
		for i := 0; i < len(equalizedCards); i++ {
			repoIdx := start + i

			screenCardX := baseX + contentX
			screenCardY := baseY + contentY

			// Full card zone (entire rendered card area)
			hitZones = append(hitZones, HitZone{
				X: screenCardX, Y: screenCardY, W: cardWidth, H: maxH,
				Type:    ZoneCard,
				RepoIdx: repoIdx,
				WTIdx:   -1,
			})

			// Repo header: inside border, first content line
			// Border top = 1 line, so header is at screenCardY + 1
			headerY := screenCardY + 1
			hitZones = append(hitZones, HitZone{
				X: screenCardX, Y: headerY, W: cardWidth, H: 1,
				Type:    ZoneRepoHeader,
				RepoIdx: repoIdx,
				WTIdx:   -2,
			})

			// Migration card: add migrate button + worktree hit zones
			if repos[repoIdx].NeedsMigration {
				// Migration card layout (inside border):
				// line 0: header
				// line 1: empty
				// line 2: branch name
				// line 3: warning text
				// line 4: reason
				// line 5: empty
				// line 6: migrate button
				// line 7: empty (or separator if worktrees exist)
				// line 8+: worktree lines (if any)
				migrateBtnY := headerY + 6
				hitZones = append(hitZones, HitZone{
					X: screenCardX, Y: migrateBtnY, W: cardWidth, H: 1,
					Type:    ZoneMigrateBtn,
					RepoIdx: repoIdx,
					WTIdx:   -1,
					Button:  BtnMigrate,
				})
				// Worktree hit zones inside migration card (after button + separator)
				for wtI := range repos[repoIdx].Worktrees {
					wtY := headerY + 8 + wtI // 8 = button(6) + separator(1) + worktree start(1)
					hitZones = append(hitZones, HitZone{
						X: screenCardX, Y: wtY, W: cardWidth, H: 1,
						Type:    ZoneWorktree,
						RepoIdx: repoIdx,
						WTIdx:   wtI,
					})
				}
			}

			// Worktree hit zones: each worktree line is after the header (normal cards only)
			if !repos[repoIdx].NeedsMigration {
				for wtI := range repos[repoIdx].Worktrees {
					wtY := headerY + 1 + wtI
					hitZones = append(hitZones, HitZone{
						X: screenCardX, Y: wtY, W: cardWidth, H: 1,
						Type:    ZoneWorktree,
						RepoIdx: repoIdx,
						WTIdx:   wtI,
					})
				}
			}

			contentX += cardWidth + CardGap
		}

		// Join cards with explicit gap spacers so rendered positions match hit zones
		if CardGap > 0 {
			spacer := strings.Repeat(" ", CardGap)
			var gapped []string
			for i, card := range equalizedCards {
				gapped = append(gapped, card)
				if i < len(equalizedCards)-1 {
					// Create a spacer column with matching height
					spacerCol := strings.Repeat(spacer+"\n", maxH-1) + spacer
					gapped = append(gapped, spacerCol)
				}
			}
			rowStr := lipgloss.JoinHorizontal(lipgloss.Top, gapped...)
			renderedRows = append(renderedRows, rowStr)
		} else {
			rowStr := lipgloss.JoinHorizontal(lipgloss.Top, equalizedCards...)
			renderedRows = append(renderedRows, rowStr)
		}
		contentY += maxH
	}

	grid := lipgloss.NewStyle().Margin(GridMarginY, MarginH).Render(
		strings.Join(renderedRows, "\n"),
	)

	return GridResult{
		View:      grid,
		HitZones:  hitZones,
		Rows:      numRows,
		Cols:      cols,
		CardWidth: cardWidth,
	}
}

// DetectInlineButton checks if the mouse X is over the [▸] context menu trigger.
// The trigger is right-aligned inside the card content area.
// Card layout: border(1) + padding(1) + content(cardWidth-4) + padding(1) + border(1)
func DetectInlineButton(mouseX, cardX, cardWidth, wtIdx int) HoverButton {
	if wtIdx != -2 && wtIdx < 0 {
		return BtnNone
	}
	// Last content character is at: cardX + cardWidth - 3
	// (cardWidth - 1 = right border, cardWidth - 2 = right padding, cardWidth - 3 = last content char)
	lastContentX := cardX + cardWidth - 3
	// [▸] occupies 3 chars at right edge of content: lastContentX-2 to lastContentX
	// Use a generous 5-char hit zone for easier clicking
	if mouseX >= lastContentX-4 && mouseX <= lastContentX {
		return BtnContextMenu
	}
	return BtnNone
}

// HitTest finds what element is at the given screen coordinates.
func HitTest(zones []HitZone, x, y int) (repoIdx int, wtIdx int, btn HoverButton) {
	repoIdx = -1
	wtIdx = -1
	btn = BtnNone

	// Check migrate button first (most specific)
	for _, z := range zones {
		if z.Type == ZoneMigrateBtn {
			if x >= z.X && x < z.X+z.W && y >= z.Y && y < z.Y+z.H {
				return z.RepoIdx, z.WTIdx, BtnMigrate
			}
		}
	}

	// Check worktrees (most specific content zones)
	for _, z := range zones {
		if z.Type == ZoneWorktree {
			if x >= z.X && x < z.X+z.W && y >= z.Y && y < z.Y+z.H {
				return z.RepoIdx, z.WTIdx, BtnNone
			}
		}
	}

	// Check repo header
	for _, z := range zones {
		if z.Type == ZoneRepoHeader {
			if x >= z.X && x < z.X+z.W && y >= z.Y && y < z.Y+z.H {
				return z.RepoIdx, -2, BtnNone
			}
		}
	}

	// Check card (catches border area and padding)
	for _, z := range zones {
		if z.Type == ZoneCard {
			if x >= z.X && x < z.X+z.W && y >= z.Y && y < z.Y+z.H {
				return z.RepoIdx, -1, BtnNone
			}
		}
	}

	return -1, -1, BtnNone
}

// HistoryHitTest checks if coordinates hit a history suggestion item.
// Returns the suggestion index or -1.
func HistoryHitTest(zones []HitZone, x, y int) int {
	for _, z := range zones {
		if z.Type == ZoneHistoryItem {
			if x >= z.X && x < z.X+z.W && y >= z.Y && y < z.Y+z.H {
				return z.RepoIdx
			}
		}
	}
	return -1
}

// renderEmptyState renders the "no repos" message with history suggestions.
func renderEmptyState(termWidth, gridYOffset, hoveredHistory int) GridResult {
	suggestions := config.GetHistorySuggestions(CurrentWorkDir)

	dimStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Background(ColorBlack)
	greenStyle := lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack)
	hoverStyle := lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorDarkGreen).Bold(true)

	var lines []string
	lines = append(lines, "")
	lines = append(lines, EmptyStyle.Render("No repositories found in this directory."))
	lines = append(lines, dimStyle.Render("Add git repos here, or open LTS in a previous location:"))
	lines = append(lines, "")

	var hitZones []HitZone
	baseY := gridYOffset + GridMarginY

	if len(suggestions) > 0 {
		for i, s := range suggestions {
			dir := filepath.Base(s.Path)
			parent := filepath.Dir(s.Path)
			repoLabel := fmt.Sprintf("%d repos", s.RepoCount)

			var line string
			if i == hoveredHistory {
				line = hoverStyle.Render("▸ "+dir) + " " + dimStyle.Render(parent) + " " + greenStyle.Render("("+repoLabel+")")
			} else {
				line = whiteStyle.Render("  "+dir) + " " + dimStyle.Render(parent) + " " + greenStyle.Render("("+repoLabel+")")
			}
			lines = append(lines, line)

			// Hit zone for this suggestion
			hitZones = append(hitZones, HitZone{
				X: MarginH, Y: baseY + len(lines) - 1, W: termWidth - MarginH*2, H: 1,
				Type:    ZoneHistoryItem,
				RepoIdx: i,
				WTIdx:   -1,
			})
		}
	} else {
		lines = append(lines, dimStyle.Render("  No previous locations found."))
	}

	content := strings.Join(lines, "\n")
	view := lipgloss.NewStyle().Margin(GridMarginY, MarginH).Render(content)

	return GridResult{View: view, HitZones: hitZones}
}
