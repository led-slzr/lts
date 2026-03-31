package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FooterButtonInfo holds position info for hit testing a footer button.
type FooterButtonInfo struct {
	X   int // start X position (screen coords)
	W   int // width
	Btn HoverButton
}

// footerButton defines a footer button's content.
type footerButton struct {
	Key   string
	Label string
	Btn   HoverButton
}

var (
	footerLeft = []footerButton{
		{"r", "Refresh All", BtnRefreshAll},
		{"c", "Cleanup Merged", BtnCleanupMerged},
	}
	footerRight = []footerButton{
		{"s", "Settings", BtnSettings},
		{"q", "Exit", BtnExit},
	}
)

// computeFooterLayout returns the rendered button widths and gap for the footer.
// This is the single source of truth used by both rendering and hit testing.
func computeFooterLayout(width int) (leftBtns []int, rightBtns []int, gap int) {
	for _, b := range footerLeft {
		// Measure using the normal (non-hover) rendering which determines layout width
		rendered := ButtonStyle.Render(b.Key + " " + b.Label)
		leftBtns = append(leftBtns, lipgloss.Width(rendered))
	}
	for _, b := range footerRight {
		rendered := ButtonStyle.Render(b.Key + " " + b.Label)
		rightBtns = append(rightBtns, lipgloss.Width(rendered))
	}

	leftW := 0
	for i, w := range leftBtns {
		leftW += w
		if i < len(leftBtns)-1 {
			leftW += 2 // gap between buttons
		}
	}
	rightW := 0
	for i, w := range rightBtns {
		rightW += w
		if i < len(rightBtns)-1 {
			rightW += 2
		}
	}

	availWidth := width - (MarginH * 2)
	gap = availWidth - leftW - rightW
	if gap < 2 {
		gap = 2
	}
	return
}

// FooterHitZones computes the screen X positions and widths of all footer buttons.
func FooterHitZones(width int) []FooterButtonInfo {
	leftBtns, rightBtns, gap := computeFooterLayout(width)

	var zones []FooterButtonInfo
	x := MarginH
	for i, w := range leftBtns {
		zones = append(zones, FooterButtonInfo{X: x, W: w, Btn: footerLeft[i].Btn})
		x += w + 2
	}

	// Right side starts after left + gap
	leftW := 0
	for i, w := range leftBtns {
		leftW += w
		if i < len(leftBtns)-1 {
			leftW += 2
		}
	}
	x = MarginH + leftW + gap
	for i, w := range rightBtns {
		zones = append(zones, FooterButtonInfo{X: x, W: w, Btn: footerRight[i].Btn})
		x += w + 2
	}

	return zones
}

// GetFooterButtonAtX returns which button is at the given X coordinate.
func GetFooterButtonAtX(x, width int) HoverButton {
	for _, info := range FooterHitZones(width) {
		if x >= info.X && x < info.X+info.W {
			return info.Btn
		}
	}
	return BtnNone
}

// RenderFooter renders footer using the shared layout computation.
func RenderFooter(width int, hoveredBtn HoverButton) string {
	keyStyle := lipgloss.NewStyle().Foreground(ColorDarkGreen).Background(ColorBtnBg)

	_, _, gap := computeFooterLayout(width)

	renderBtn := func(b footerButton, hovered bool) string {
		if hovered {
			return ButtonHoverStyle.Render(b.Key + " " + b.Label)
		}
		return keyStyle.Render(b.Key) + ButtonStyle.Render(" "+b.Label)
	}

	var leftParts []string
	for _, b := range footerLeft {
		leftParts = append(leftParts, renderBtn(b, hoveredBtn == b.Btn))
	}
	var rightParts []string
	for _, b := range footerRight {
		rightParts = append(rightParts, renderBtn(b, hoveredBtn == b.Btn))
	}

	left := strings.Join(leftParts, "  ")
	right := strings.Join(rightParts, "  ")
	footer := left + strings.Repeat(" ", gap) + right

	return lipgloss.NewStyle().
		Margin(0, MarginH).
		Render(footer)
}

// CreateBtnHitZone returns the X position and width of the centered create button.
func CreateBtnHitZone(termWidth int) (x, w int) {
	btnW := lipgloss.Width(CreateBtnStyle.Render("n Create Worktree"))
	availW := termWidth - (MarginH * 2)
	btnX := MarginH + (availW-btnW)/2
	return btnX, btnW
}

func RenderCreateButton(width int, hovered bool) string {
	var btn string
	if hovered {
		btn = CreateBtnHoverStyle.Render("n Create Worktree")
	} else {
		keyStyle := lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack)
		btn = CreateBtnStyle.Render(keyStyle.Render("n") + " Create Worktree")
	}

	return lipgloss.NewStyle().
		Width(width - (MarginH * 2)).
		Align(lipgloss.Center).
		Margin(0, MarginH).
		Render(btn)
}
