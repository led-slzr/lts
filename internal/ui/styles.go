package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	ColorDarkGreen  = lipgloss.Color("#006400")
	ColorGreen      = lipgloss.Color("#00AA00")
	ColorClean      = lipgloss.Color("#5F875F")
	ColorCyan       = lipgloss.Color("#00CCCC")
	ColorRed        = lipgloss.Color("#CC3333")
	ColorYellow     = lipgloss.Color("#CCAA00")
	ColorBlue       = lipgloss.Color("#3388FF")
	ColorWhite      = lipgloss.Color("#FFFFFF")
	ColorGray       = lipgloss.Color("#555555")
	ColorDim        = lipgloss.Color("#666666")
	ColorBlack      = lipgloss.Color("#000000")
	ColorBtnBg      = lipgloss.Color("#111111")
	ColorBtnHoverBg = lipgloss.Color("#006400")
	ColorMagenta    = lipgloss.Color("#CC55CC")

	// Title
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorDarkGreen).
			Background(ColorBlack)

	// Card borders
	CardBorderNormal = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorGray).
				BorderBackground(ColorBlack).
				Background(ColorBlack).
				Padding(0, 1)

	CardBorderFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorWhite).
				BorderBackground(ColorBlack).
				Background(ColorBlack).
				Padding(0, 1)

	CardBorderMigration = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorYellow).
				BorderBackground(ColorBlack).
				Background(ColorBlack).
				Padding(0, 1)

	MigrateBtnStyle = lipgloss.NewStyle().
				Foreground(ColorYellow).
				Background(ColorBlack).
				Bold(true)

	MigrateBtnHoverStyle = lipgloss.NewStyle().
				Foreground(ColorBlack).
				Background(ColorYellow).
				Bold(true)

	// Repo name in card
	RepoNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorWhite).
			Background(ColorBlack)

	BranchDimStyle = lipgloss.NewStyle().
			Foreground(ColorDim).
			Background(ColorBlack)

	// Worktree status colors
	StatusCleanStyle     = lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack)
	StatusChangedStyle   = lipgloss.NewStyle().Foreground(ColorCyan).Background(ColorBlack)
	StatusDivergedStyle  = lipgloss.NewStyle().Foreground(ColorRed).Background(ColorBlack)
	StatusMissingStyle   = lipgloss.NewStyle().Foreground(ColorRed).Background(ColorBlack)
	StatusToPushStyle    = lipgloss.NewStyle().Foreground(ColorYellow).Background(ColorBlack)
	StatusToPullStyle    = lipgloss.NewStyle().Foreground(ColorYellow).Background(ColorBlack)
	StatusNewStyle       = lipgloss.NewStyle().Foreground(ColorBlue).Background(ColorBlack)
	StatusNoRemoteStyle  = lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	StatusMergedStyle    = lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack)
	StatusMergedDirStyle = lipgloss.NewStyle().Foreground(ColorMagenta).Background(ColorBlack)

	// Worktree branch name (normal)
	WTBranchStyle = lipgloss.NewStyle().Foreground(ColorCyan).Background(ColorBlack)

	// Highlighted worktree (hovered)
	WTHighlightStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorWhite).
				Background(ColorBlack)

	// Tree character
	TreeCharStyle = lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)

	// Buttons
	ButtonStyle = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Background(ColorBtnBg).
			Padding(0, 1)

	ButtonHoverStyle = lipgloss.NewStyle().
				Foreground(ColorWhite).
				Background(ColorBtnHoverBg).
				Bold(true).
				Padding(0, 1)

	// Inline action buttons
	InlineBtnStyle = lipgloss.NewStyle().
			Foreground(ColorDim).
			Background(ColorBlack)

	InlineBtnHoverStyle = lipgloss.NewStyle().
				Foreground(ColorWhite).
				Background(ColorBlack).
				Bold(true)

	// Footer
	FooterStyle = lipgloss.NewStyle().
			Foreground(ColorDim).
			Background(ColorBlack)

	// Status bar
	StatusBarStyle = lipgloss.NewStyle().
			Foreground(ColorGreen).
			Background(ColorBlack).
			Italic(true)

	// Click usage
	ClickUsageActiveStyle = lipgloss.NewStyle().
				Foreground(ColorWhite).
				Background(ColorDarkGreen).
				Bold(true).
				Padding(0, 1)

	ClickUsageInactiveStyle = lipgloss.NewStyle().
				Foreground(ColorDim).
				Background(ColorBlack).
				Padding(0, 1)

	// Click usage label
	ClickUsageLabelStyle = lipgloss.NewStyle().
				Foreground(ColorDim).
				Background(ColorBlack)

	// Modal overlay
	ModalStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(ColorGreen).
			BorderBackground(ColorBlack).
			Background(ColorBlack).
			Padding(1, 2).
			Width(50)

	// Create button
	CreateBtnStyle = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Background(ColorBlack).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorGray).
			BorderBackground(ColorBlack).
			Padding(0, 2)

	CreateBtnHoverStyle = lipgloss.NewStyle().
				Foreground(ColorWhite).
				Background(ColorDarkGreen).
				Bold(true).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorWhite).
				BorderBackground(ColorDarkGreen).
				Padding(0, 2)

	// Empty state
	EmptyStyle = lipgloss.NewStyle().
			Foreground(ColorDim).
			Background(ColorBlack).
			Italic(true)

	// Margin wrapper
	MarginH = 2
)
