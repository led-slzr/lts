package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FooterButtonInfo holds rendered button text and its width for hit testing.
type FooterButtonInfo struct {
	Label string
	X     int // start X position (screen coords)
	W     int // width
	Btn   HoverButton
}

// RenderFooter renders footer and returns button positions for hit testing.
func RenderFooter(width int, hoveredBtn HoverButton) string {
	refreshBtn := renderFooterBtn("Refresh All Overviews", hoveredBtn == BtnRefreshAll)
	cleanupBtn := renderFooterBtn("Cleanup Merged Cleanables", hoveredBtn == BtnCleanupMerged)
	settingsBtn := renderFooterBtn("Settings", hoveredBtn == BtnSettings)
	exitBtn := renderFooterBtn("Exit", hoveredBtn == BtnExit)

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

// GetFooterButtonAtX returns which button is at the given X coordinate.
func GetFooterButtonAtX(x, width int) HoverButton {
	// Approximate button positions based on rendered widths
	// Left side: [Refresh All Overviews]  [Cleanup Merged Cleanables]
	// Right side: [Settings]  [Exit]
	refreshW := 23 // "Refresh All Overviews" + padding
	cleanupW := 27 // "Cleanup Merged Cleanables" + padding
	settingsW := 10 // "Settings" + padding
	exitW := 6     // "Exit" + padding

	// Left buttons
	x1 := MarginH
	if x >= x1 && x < x1+refreshW {
		return BtnRefreshAll
	}
	x2 := x1 + refreshW + 2
	if x >= x2 && x < x2+cleanupW {
		return BtnCleanupMerged
	}

	// Right buttons
	availWidth := width - (MarginH * 2)
	rightStart := MarginH + availWidth - settingsW - 2 - exitW
	if x >= rightStart && x < rightStart+settingsW {
		return BtnSettings
	}
	exitStart := rightStart + settingsW + 2
	if x >= exitStart && x < exitStart+exitW {
		return BtnExit
	}

	return BtnNone
}

func renderFooterBtn(label string, hovered bool) string {
	if hovered {
		return ButtonHoverStyle.Render(label)
	}
	return ButtonStyle.Render(label)
}

func RenderCreateButton(width int, hovered bool) string {
	var btn string
	if hovered {
		btn = CreateBtnHoverStyle.Render("Create Worktree")
	} else {
		btn = CreateBtnStyle.Render("Create Worktree")
	}

	return lipgloss.NewStyle().
		Width(width - (MarginH * 2)).
		Align(lipgloss.Center).
		Margin(0, MarginH).
		Render(btn)
}
