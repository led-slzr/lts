package ui

import "github.com/charmbracelet/lipgloss"

// Colors are assigned by ApplyTheme (theme.go) — every color in the UI
// flows through these vars, so themes are a hot swap. The derived styles
// below capture color values at construction, hence rebuildStyles().
var (
	ColorDarkGreen  lipgloss.Color
	ColorGreen      lipgloss.Color
	ColorClean      lipgloss.Color
	ColorCyan       lipgloss.Color
	ColorRed        lipgloss.Color
	ColorYellow     lipgloss.Color
	ColorBlue       lipgloss.Color
	ColorWhite      lipgloss.Color
	ColorGray       lipgloss.Color
	ColorDim        lipgloss.Color
	ColorBlack      lipgloss.Color
	ColorBtnBg      lipgloss.Color
	ColorBtnHoverBg lipgloss.Color
	ColorMagenta    lipgloss.Color
	ColorTeal       lipgloss.Color // tmux session indicators

	// Derived styles — rebuilt on every theme apply
	TitleStyle           lipgloss.Style
	CardBorderNormal     lipgloss.Style
	CardBorderFocused    lipgloss.Style
	CardBorderMigration  lipgloss.Style
	MigrateBtnStyle      lipgloss.Style
	MigrateBtnHoverStyle lipgloss.Style
	RepoNameStyle        lipgloss.Style
	BranchDimStyle       lipgloss.Style

	// Worktree status colors
	StatusCleanStyle     lipgloss.Style
	StatusChangedStyle   lipgloss.Style
	StatusDivergedStyle  lipgloss.Style
	StatusMissingStyle   lipgloss.Style
	StatusToPushStyle    lipgloss.Style
	StatusToPullStyle    lipgloss.Style
	StatusNewStyle       lipgloss.Style
	StatusNoRemoteStyle  lipgloss.Style
	StatusMergedStyle    lipgloss.Style
	StatusMergedDirStyle lipgloss.Style

	WTBranchStyle    lipgloss.Style
	WTHighlightStyle lipgloss.Style
	TreeCharStyle    lipgloss.Style

	ButtonStyle         lipgloss.Style
	ButtonHoverStyle    lipgloss.Style
	InlineBtnStyle      lipgloss.Style
	InlineBtnHoverStyle lipgloss.Style

	FooterStyle    lipgloss.Style
	StatusBarStyle lipgloss.Style

	ClickUsageActiveStyle   lipgloss.Style
	ClickUsageInactiveStyle lipgloss.Style
	ClickUsageLabelStyle    lipgloss.Style

	ModalStyle lipgloss.Style

	CreateBtnStyle      lipgloss.Style
	CreateBtnHoverStyle lipgloss.Style

	EmptyStyle lipgloss.Style

	// Margin wrapper
	MarginH = 2
)

func init() {
	ApplyTheme(ClassicLTS)
}

// rebuildStyles reconstructs every derived style from the current color
// vars. Called by ApplyTheme — styles capture colors at construction.
func rebuildStyles() {
	TitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorDarkGreen).
		Background(ColorBlack)

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

	RepoNameStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorWhite).
		Background(ColorBlack)

	BranchDimStyle = lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack)

	StatusCleanStyle = lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack)
	StatusChangedStyle = lipgloss.NewStyle().Foreground(ColorCyan).Background(ColorBlack)
	StatusDivergedStyle = lipgloss.NewStyle().Foreground(ColorRed).Background(ColorBlack)
	StatusMissingStyle = lipgloss.NewStyle().Foreground(ColorRed).Background(ColorBlack)
	StatusToPushStyle = lipgloss.NewStyle().Foreground(ColorYellow).Background(ColorBlack)
	StatusToPullStyle = lipgloss.NewStyle().Foreground(ColorYellow).Background(ColorBlack)
	StatusNewStyle = lipgloss.NewStyle().Foreground(ColorBlue).Background(ColorBlack)
	StatusNoRemoteStyle = lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	StatusMergedStyle = lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack)
	StatusMergedDirStyle = lipgloss.NewStyle().Foreground(ColorMagenta).Background(ColorBlack)

	WTBranchStyle = lipgloss.NewStyle().Foreground(ColorCyan).Background(ColorBlack)

	WTHighlightStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorWhite).
		Background(ColorBlack)

	TreeCharStyle = lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)

	ButtonStyle = lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorBtnBg).
		Padding(0, 1)

	ButtonHoverStyle = lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorBtnHoverBg).
		Bold(true).
		Padding(0, 1)

	InlineBtnStyle = lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack)

	InlineBtnHoverStyle = lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorBlack).
		Bold(true)

	FooterStyle = lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack)

	StatusBarStyle = lipgloss.NewStyle().
		Foreground(ColorGreen).
		Background(ColorBlack).
		Italic(true)

	ClickUsageActiveStyle = lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorDarkGreen).
		Bold(true).
		Padding(0, 1)

	ClickUsageInactiveStyle = lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack).
		Padding(0, 1)

	ClickUsageLabelStyle = lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack)

	ModalStyle = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(ColorGreen).
		BorderBackground(ColorBlack).
		Background(ColorBlack).
		Padding(1, 2).
		Width(50)

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
		BorderBackground(ColorBlack).
		Padding(0, 2)

	EmptyStyle = lipgloss.NewStyle().
		Foreground(ColorDim).
		Background(ColorBlack).
		Italic(true)
}
