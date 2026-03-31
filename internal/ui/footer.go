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

// RenderFooter renders footer and returns button positions for hit testing.
func RenderFooter(width int, hoveredBtn HoverButton) string {
	keyStyle := lipgloss.NewStyle().Foreground(ColorDarkGreen).Background(ColorBtnBg)
	keyHoverStyle := lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorBtnHoverBg).Bold(true)

	refreshBtn := renderFooterBtnWithKey("r", "Refresh All", hoveredBtn == BtnRefreshAll, keyStyle, keyHoverStyle)
	cleanupBtn := renderFooterBtnWithKey("c", "Cleanup Merged", hoveredBtn == BtnCleanupMerged, keyStyle, keyHoverStyle)
	settingsBtn := renderFooterBtnWithKey("s", "Settings", hoveredBtn == BtnSettings, keyStyle, keyHoverStyle)
	exitBtn := renderFooterBtnWithKey("q", "Exit", hoveredBtn == BtnExit, keyStyle, keyHoverStyle)

	left := refreshBtn + "  " + cleanupBtn
	right := settingsBtn + "  " + exitBtn

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	availWidth := width - (MarginH * 2)
	gap := availWidth - leftWidth - rightWidth
	if gap < 2 {
		gap = 2
	}

	footer := left + strings.Repeat(" ", gap) + right

	return lipgloss.NewStyle().
		Margin(0, MarginH).
		Render(footer)
}

// FooterHitZones computes the screen X positions and widths of all footer buttons.
// Matches the layout logic in RenderFooter exactly.
func FooterHitZones(width int) []FooterButtonInfo {
	refreshW := lipgloss.Width("r Refresh All") + 2  // +2 for ButtonStyle padding
	cleanupW := lipgloss.Width("c Cleanup Merged") + 2
	settingsW := lipgloss.Width("s Settings") + 2
	exitW := lipgloss.Width("q Exit") + 2

	leftW := refreshW + 2 + cleanupW // 2 = gap between left buttons
	rightW := settingsW + 2 + exitW

	availWidth := width - (MarginH * 2)
	gap := availWidth - leftW - rightW
	if gap < 2 {
		gap = 2
	}

	return []FooterButtonInfo{
		{X: MarginH, W: refreshW, Btn: BtnRefreshAll},
		{X: MarginH + refreshW + 2, W: cleanupW, Btn: BtnCleanupMerged},
		{X: MarginH + leftW + gap, W: settingsW, Btn: BtnSettings},
		{X: MarginH + leftW + gap + settingsW + 2, W: exitW, Btn: BtnExit},
	}
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

func renderFooterBtn(label string, hovered bool) string {
	if hovered {
		return ButtonHoverStyle.Render(label)
	}
	return ButtonStyle.Render(label)
}

func renderFooterBtnWithKey(key, label string, hovered bool, keyNormal, keyHover lipgloss.Style) string {
	if hovered {
		return ButtonHoverStyle.Render(key + " " + label)
	}
	return keyNormal.Render(key) + ButtonStyle.Render(" " + label)
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
