package ui

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// Theme is a semantic palette: 14 slots named for the role a color plays,
// not the color itself. Every color in the UI flows through the ColorX
// package vars, so applying a theme is a hot swap — reassign the vars,
// rebuild the derived styles, and the next frame renders in the new skin.
type Theme struct {
	Key  string // config value (THEME=)
	Name string // display name

	Bg         string // canvas — painted across the whole screen
	Text       string // primary text (was white)
	Dim        string // secondary text, hints, footers
	Gray       string // borders, inactive chrome
	BtnBg      string // button fill
	Accent     string // the brand color: titles, status bar, modal borders
	AccentDark string // hover/selection background (Text must read on it)
	Clean      string // muted ok-ness
	Danger     string // destructive, diverged, errors
	Warn       string // to push/pull, migrations, cautions
	Info       string // new branches
	Changed    string // branch names, changed status (was cyan)
	Special    string // merged-but-dirty (was magenta)
	Session    string // tmux indicators (was teal)
}

// ClassicLTS is the original green-on-black — these hexes are the exact
// values the UI shipped with, so applying it is pixel-identical.
var ClassicLTS = Theme{
	Key: "classic-lts", Name: "Classic LTS",
	Bg: "#000000", Text: "#FFFFFF", Dim: "#666666", Gray: "#555555", BtnBg: "#111111",
	Accent: "#00AA00", AccentDark: "#006400",
	Clean: "#5F875F", Danger: "#CC3333", Warn: "#CCAA00", Info: "#3388FF",
	Changed: "#00CCCC", Special: "#CC55CC", Session: "#00CCCC",
}

// LedThereBeLight is the light theme: warm paper, not clinical white —
// every foreground deepened to hold contrast on a bright canvas.
var LedThereBeLight = Theme{
	Key: "led-there-be-light", Name: "Led There Be Light",
	Bg: "#FBF8F1", Text: "#21201C", Dim: "#8C8577", Gray: "#C8C0AE", BtnBg: "#F0EBDE",
	Accent: "#128A2E", AccentDark: "#1F6B33",
	Clean: "#4E7A50", Danger: "#C4322C", Warn: "#A96A00", Info: "#1763C4",
	Changed: "#0F7E8A", Special: "#A03296", Session: "#0C8577",
}

// JelaMyLove is the purple one — a love letter in twilight violet. Deep
// plum canvas (warmer than black), moonlit lavender text, amethyst
// accents, branches in soft orchid, danger in raspberry rose, and a
// single spark of mint for the tmux dots. Gentle saturation throughout —
// made to be looked at for hours.
var JelaMyLove = Theme{
	Key: "jela-my-love", Name: "Jela My Love",
	Bg: "#191126", Text: "#F1E8FE", Dim: "#8578A6", Gray: "#453960", BtnBg: "#241A38",
	Accent: "#C89BFA", AccentDark: "#5D3E8E",
	Clean: "#A5B4FC", Danger: "#F97393", Warn: "#E7B96B", Info: "#86A8F8",
	Changed: "#D3B3F5", Special: "#F49AD2", Session: "#86E8D0",
}

// Photosynthesis: deep forest — moss canvas, leaf accent, pollen warns,
// sky-through-canopy info, wildflower special.
var Photosynthesis = Theme{
	Key: "photosynthesis", Name: "Photosynthesis",
	Bg: "#0F1710", Text: "#E4EED8", Dim: "#71836B", Gray: "#35452F", BtnBg: "#16211A",
	Accent: "#8FBF4D", AccentDark: "#3E6322",
	Clean: "#6E9E58", Danger: "#E06C4B", Warn: "#D9A93F", Info: "#5FA8C9",
	Changed: "#B4C95E", Special: "#C98BC9", Session: "#55BFA0",
}

// LightsOut: retro amber CRT — a phosphor ramp on true black, danger in
// red-orange because even a Model 33 had an alarm light.
var LightsOut = Theme{
	Key: "lights-out", Name: "Lights Out",
	Bg: "#000000", Text: "#FFB000", Dim: "#8A5E00", Gray: "#4A3300", BtnBg: "#120C00",
	Accent: "#FFB000", AccentDark: "#5C4200",
	Clean: "#D08E00", Danger: "#FF3D1F", Warn: "#FFD34D", Info: "#E0940A",
	Changed: "#FFCB6B", Special: "#FF7A1A", Session: "#FFE39B",
}

// Dracula (draculatheme.com), mapped onto the LTS slots.
var Dracula = Theme{
	Key: "dracula", Name: "Dracula",
	Bg: "#282A36", Text: "#F8F8F2", Dim: "#6272A4", Gray: "#44475A", BtnBg: "#21222C",
	Accent: "#50FA7B", AccentDark: "#44475A",
	Clean: "#69B076", Danger: "#FF5555", Warn: "#F1FA8C", Info: "#BD93F9",
	Changed: "#8BE9FD", Special: "#FF79C6", Session: "#FFB86C",
}

// Nord (nordtheme.com) — polar night canvas, frost and aurora accents.
var Nord = Theme{
	Key: "nord", Name: "Nord",
	Bg: "#2E3440", Text: "#ECEFF4", Dim: "#7B88A1", Gray: "#434C5E", BtnBg: "#3B4252",
	Accent: "#A3BE8C", AccentDark: "#5E81AC",
	Clean: "#8FA876", Danger: "#BF616A", Warn: "#EBCB8B", Info: "#81A1C1",
	Changed: "#88C0D0", Special: "#B48EAD", Session: "#8FBCBB",
}

// GruvboxDark (github.com/morhetz/gruvbox) — retro warmth.
var GruvboxDark = Theme{
	Key: "gruvbox-dark", Name: "Gruvbox Dark",
	Bg: "#282828", Text: "#EBDBB2", Dim: "#928374", Gray: "#504945", BtnBg: "#32302F",
	Accent: "#B8BB26", AccentDark: "#665C54",
	Clean: "#98971A", Danger: "#FB4934", Warn: "#FABD2F", Info: "#83A598",
	Changed: "#8EC07C", Special: "#D3869B", Session: "#689D6A",
}

// CatppuccinMocha (catppuccin.com) — soothing pastels on mocha.
var CatppuccinMocha = Theme{
	Key: "catppuccin-mocha", Name: "Catppuccin Mocha",
	Bg: "#1E1E2E", Text: "#CDD6F4", Dim: "#7F849C", Gray: "#45475A", BtnBg: "#181825",
	Accent: "#A6E3A1", AccentDark: "#585B70",
	Clean: "#8FCE9B", Danger: "#F38BA8", Warn: "#F9E2AF", Info: "#89B4FA",
	Changed: "#94E2D5", Special: "#F5C2E7", Session: "#74C7EC",
}

// SolarizedLight (ethanschoonover.com/solarized) — the second light option.
var SolarizedLight = Theme{
	Key: "solarized-light", Name: "Solarized Light",
	Bg: "#FDF6E3", Text: "#073642", Dim: "#93A1A1", Gray: "#CCC4B0", BtnBg: "#EEE8D5",
	Accent: "#859900", AccentDark: "#586E75",
	Clean: "#6E8000", Danger: "#DC322F", Warn: "#B58900", Info: "#268BD2",
	Changed: "#2AA198", Special: "#D33682", Session: "#6C71C4",
}

// Themes is the registry, in studio display order: LTS originals first,
// then the familiar palettes.
var Themes = []Theme{
	ClassicLTS,
	LedThereBeLight,
	JelaMyLove,
	Photosynthesis,
	LightsOut,
	Dracula,
	Nord,
	GruvboxDark,
	CatppuccinMocha,
	SolarizedLight,
}

// CurrentTheme is the theme the UI is rendering with right now.
var CurrentTheme = ClassicLTS

// ThemeByKey resolves a config value, falling back to Classic LTS.
func ThemeByKey(key string) Theme {
	for _, t := range Themes {
		if t.Key == key {
			return t
		}
	}
	return ClassicLTS
}

// ApplyTheme hot-swaps the palette: color vars, derived styles, canvas.
func ApplyTheme(t Theme) {
	CurrentTheme = t
	ColorBlack = lipgloss.Color(t.Bg)
	ColorWhite = lipgloss.Color(t.Text)
	ColorDim = lipgloss.Color(t.Dim)
	ColorGray = lipgloss.Color(t.Gray)
	ColorBtnBg = lipgloss.Color(t.BtnBg)
	ColorGreen = lipgloss.Color(t.Accent)
	ColorDarkGreen = lipgloss.Color(t.AccentDark)
	ColorBtnHoverBg = lipgloss.Color(t.AccentDark)
	ColorClean = lipgloss.Color(t.Clean)
	ColorRed = lipgloss.Color(t.Danger)
	ColorYellow = lipgloss.Color(t.Warn)
	ColorBlue = lipgloss.Color(t.Info)
	ColorCyan = lipgloss.Color(t.Changed)
	ColorMagenta = lipgloss.Color(t.Special)
	ColorTeal = lipgloss.Color(t.Session)
	rebuildStyles()
}

// ThemeBgSeq is the ANSI sequence painting the theme canvas — the screen
// painter re-applies it after every reset so the whole terminal cell grid
// carries the theme background (this is what makes light themes possible).
func ThemeBgSeq() string {
	r, g, b := hexRGB(CurrentTheme.Bg)
	return fmt.Sprintf("\033[48;2;%d;%d;%dm", r, g, b)
}

func hexRGB(hex string) (int, int, int) {
	if len(hex) == 7 && hex[0] == '#' {
		r, err1 := strconv.ParseUint(hex[1:3], 16, 8)
		g, err2 := strconv.ParseUint(hex[3:5], 16, 8)
		b, err3 := strconv.ParseUint(hex[5:7], 16, 8)
		if err1 == nil && err2 == nil && err3 == nil {
			return int(r), int(g), int(b)
		}
	}
	return 0, 0, 0
}
