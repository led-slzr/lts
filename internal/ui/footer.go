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

// IsOperationBtn returns true for buttons that trigger operations
// and should be disabled during loading.
func IsOperationBtn(btn HoverButton) bool {
	return btn == BtnRefreshAll || btn == BtnCleanupMerged || btn == BtnCreateWT || btn == BtnMigrate
}

// RenderFooter renders footer using the shared layout computation.
func RenderFooter(width int, hoveredBtn HoverButton, loading bool) string {
	keyStyle := lipgloss.NewStyle().Foreground(ColorDarkGreen).Background(ColorBtnBg)
	disabledStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBtnBg).Padding(0, 1)

	_, _, gap := computeFooterLayout(width)

	renderBtn := func(b footerButton, hovered bool) string {
		if loading && IsOperationBtn(b.Btn) {
			return disabledStyle.Render(b.Key + " " + b.Label)
		}
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

// RenderFooterMinimal renders a minimal footer with only Settings and Exit.
func RenderFooterMinimal(width int, hoveredBtn HoverButton) string {
	keyStyle := lipgloss.NewStyle().Foreground(ColorDarkGreen).Background(ColorBtnBg)

	renderBtn := func(b footerButton, hovered bool) string {
		if hovered {
			return ButtonHoverStyle.Render(b.Key + " " + b.Label)
		}
		return keyStyle.Render(b.Key) + ButtonStyle.Render(" "+b.Label)
	}

	right := renderBtn(footerRight[0], hoveredBtn == footerRight[0].Btn) + "  " +
		renderBtn(footerRight[1], hoveredBtn == footerRight[1].Btn)

	return lipgloss.NewStyle().
		Width(width - (MarginH * 2)).
		Align(lipgloss.Right).
		Margin(0, MarginH).
		Render(right)
}

// FooterMinimalHitZones computes positions for the minimal footer (Settings + Exit only).
func FooterMinimalHitZones(width int) []FooterButtonInfo {
	settingsW := lipgloss.Width(ButtonStyle.Render(footerRight[0].Key + " " + footerRight[0].Label))
	exitW := lipgloss.Width(ButtonStyle.Render(footerRight[1].Key + " " + footerRight[1].Label))

	// Right-aligned: total = settingsW + 2(gap) + exitW
	totalW := settingsW + 2 + exitW
	availW := width - (MarginH * 2)
	startX := MarginH + availW - totalW

	return []FooterButtonInfo{
		{X: startX, W: settingsW, Btn: footerRight[0].Btn},
		{X: startX + settingsW + 2, W: exitW, Btn: footerRight[1].Btn},
	}
}

// GetFooterMinimalButtonAtX returns which button is at the given X in minimal footer.
func GetFooterMinimalButtonAtX(x, width int) HoverButton {
	for _, info := range FooterMinimalHitZones(width) {
		if x >= info.X && x < info.X+info.W {
			return info.Btn
		}
	}
	return BtnNone
}

// CreateBtnHitZone returns the X position and width of the centered create button.
func CreateBtnHitZone(termWidth int) (x, w int) {
	btnW := lipgloss.Width(CreateBtnStyle.Render("n Create Worktree"))
	availW := termWidth - (MarginH * 2)
	btnX := MarginH + (availW-btnW)/2
	return btnX, btnW
}

func RenderCreateButton(width int, hovered bool, loading bool) string {
	var btn string
	if loading {
		btn = CreateBtnStyle.Foreground(ColorDim).BorderForeground(ColorDim).Render("n Create Worktree")
	} else if hovered {
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
