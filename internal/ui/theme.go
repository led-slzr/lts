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

// Themes is the registry, in studio display order.
var Themes = []Theme{ClassicLTS}

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
