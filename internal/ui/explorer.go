package ui

import (
	"fmt"
	"strings"
	"time"

	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"

	"github.com/charmbracelet/lipgloss"
)

// ExplorerState holds the Explorer layout's selection and scroll state.
// Owned by the app model; the ui package only renders it.
type ExplorerState struct {
	SelectedRepo  int  // index into repos
	SelectedWT    int  // row in the selected repo's worktrees (-1 = none)
	FocusSheet    bool // false = sidebar focused
	SidebarScroll int
	SheetScroll   int
}

// Explorer layout constants
const (
	explorerMinSidebar = 18
	explorerMaxSidebar = 30
)

// WTSize is a worktree's scanned disk footprint for the SIZE column.
type WTSize struct {
	Total   int64
	Modules int64 // node_modules share of Total
	Known   bool
}

// formatSize renders bytes compactly: "312M", "1.2G", "45K".
func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	if v >= 10 {
		return fmt.Sprintf("%.0f%c", v, "KMGT"[exp])
	}
	return fmt.Sprintf("%.1f%c", v, "KMGT"[exp])
}

// formatWTSize renders the SIZE cell: "code+modules" when node_modules
// exist (the without/with answer at a glance), the total otherwise.
func formatWTSize(sz WTSize) string {
	if !sz.Known {
		return "…"
	}
	if sz.Modules > 0 {
		return formatSize(sz.Total-sz.Modules) + "+" + formatSize(sz.Modules)
	}
	return formatSize(sz.Total)
}

// formatAge renders a compact relative age: "now", "31m", "4h", "2d", "—".
func formatAge(ts int64) string {
	if ts <= 0 {
		return "—"
	}
	d := time.Since(time.Unix(ts, 0))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// ExplorerSidebarWidth returns the sidebar width for a terminal width.
func ExplorerSidebarWidth(termWidth int) int {
	w := (termWidth - MarginH*2) / 4
	if w < explorerMinSidebar {
		w = explorerMinSidebar
	}
	if w > explorerMaxSidebar {
		w = explorerMaxSidebar
	}
	return w
}

// stripAction is one entry of the Explorer's selected-row action strip.
type stripAction struct {
	Label string
	Btn   HoverButton
}

// explorerActions returns the action-strip entries for a worktree of repo.
// Open isn't listed — clicking the row (or enter) is the primary action.
func explorerActions(repo git.Repo, hasSession, canPR bool) []stripAction {
	var actions []stripAction
	if !repo.IsMonorepo {
		actions = append(actions, stripAction{"b rebase", BtnRebase})
	}
	if canPR {
		actions = append(actions, stripAction{"g pr", BtnCreatePR})
	}
	actions = append(actions,
		stripAction{"m rename", BtnRename},
		stripAction{"p modules", BtnCleanModules},
		stripAction{"d delete", BtnDelete},
	)
	if hasSession {
		actions = append(actions, stripAction{"x session", BtnKillSession})
	}
	return actions
}

// LayoutExplorer renders the master-detail layout: a repo sidebar (left) and
// a worktree sheet (right) with status, age, and tmux columns. The block is
// exactly `height` lines tall; panes scroll internally. Hit zones are in
// absolute screen coordinates (yOffset = first content line).
func LayoutExplorer(repos []git.Repo, termWidth, yOffset, height int, st ExplorerState, hoveredBtn HoverButton, busyCards map[string]bool, tmuxLive map[string]bool, ghState CloneAvail, sizes map[string]WTSize, canPR bool) GridResult {
	if height < 6 {
		height = 6
	}
	availW := termWidth - MarginH*2
	sidebarW := ExplorerSidebarWidth(termWidth)
	sheetW := availW - sidebarW - 1
	var zones []HitZone

	sel := st.SelectedRepo
	if sel < 0 || sel >= len(repos) {
		sel = 0
	}
	repo := repos[sel]

	// ---- Sidebar ----
	sbInner := sidebarW - 4 // border + padding
	innerH := height - 2    // box borders
	itemRows := innerH - 1  // minus title row

	titleStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	selStyle := lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorSelBg).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorBlack)
	monoStyle := lipgloss.NewStyle().Foreground(ColorMagenta).Background(ColorBlack)
	tealStyle := lipgloss.NewStyle().Foreground(ColorTeal).Background(ColorBlack)
	busyStyle := lipgloss.NewStyle().Foreground(ColorYellow).Background(ColorBlack)
	warnStyle := lipgloss.NewStyle().Foreground(ColorYellow).Background(ColorBlack)

	sbScroll := st.SidebarScroll
	if sbScroll > len(repos)-itemRows {
		sbScroll = len(repos) - itemRows
	}
	if sbScroll < 0 {
		sbScroll = 0
	}

	sbLines := []string{titleStyle.Render("REPOS")}
	// Sidebar item rows start after: border(1) + title(1)
	sbItemStartY := yOffset + 2
	for i := sbScroll; i < len(repos) && i-sbScroll < itemRows; i++ {
		r := repos[i]
		marker := ""
		if r.NeedsMigration {
			marker = warnStyle.Render(" ⚠")
		} else if busyCards[r.Name] {
			marker = busyStyle.Render(" ⟳")
		}
		count := dimStyle.Render(fmt.Sprintf(" %d", len(r.Worktrees)))
		var name string
		if i == sel {
			name = selStyle.Render(" " + r.Name + " ")
		} else if r.IsMonorepo {
			name = monoStyle.Render(" " + r.Name)
		} else {
			name = nameStyle.Render(" " + r.Name)
		}
		sbLines = append(sbLines, truncate(name+count+marker, sbInner))
		zones = append(zones, HitZone{
			X: MarginH, Y: sbItemStartY + (i - sbScroll), W: sidebarW, H: 1,
			Type: ZoneExplorerRepo, RepoIdx: i, WTIdx: -1,
		})
	}
	// "+ Clone a Repo" entry at the end of the list (hollow when gh isn't ready)
	if len(repos)-sbScroll < itemRows {
		cloneStyle := dimStyle
		if ghState == CloneReady {
			cloneStyle = lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack)
			if hoveredBtn == BtnClone {
				cloneStyle = lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorSelBg).Bold(true)
			}
		}
		sbLines = append(sbLines, truncate(cloneStyle.Render(" (c) Clone a Repo"), sbInner))
		zones = append(zones, HitZone{
			X: MarginH, Y: sbItemStartY + (len(repos) - sbScroll), W: sidebarW, H: 1,
			Type: ZoneClone, RepoIdx: -1, WTIdx: -1, Button: BtnClone,
		})
	}
	for len(sbLines) < innerH {
		sbLines = append(sbLines, "")
	}

	sidebarBorder := CardBorderNormal
	if !st.FocusSheet {
		sidebarBorder = CardBorderFocused
	}
	sidebar := sidebarBorder.Width(sidebarW - 2).Render(strings.Join(sbLines[:innerH], "\n"))

	// ---- Sheet ----
	shInner := sheetW - 4
	showSize := shInner >= 66 && sizes != nil // widest column drops first; nil = scanning off or no data yet
	showAge := shInner >= 52
	showTmux := shInner >= 44

	// Title bar: repo name + count, [+ new] right-aligned
	countTxt := fmt.Sprintf("%d worktrees", len(repo.Worktrees))
	if len(repo.Worktrees) == 1 {
		countTxt = "1 worktree"
	}
	title := lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorBlack).Bold(true).Render(repo.Name) +
		dimStyle.Render(" — "+countTxt)
	const newBtnText = "n New Worktree"
	keyStyle := lipgloss.NewStyle().Foreground(ColorGreen).Background(ColorBlack).Bold(true)
	newStyled := keyStyle.Render("n") + dimStyle.Render(" New Worktree")
	if hoveredBtn == BtnCreateWT {
		newStyled = lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorSelBg).Bold(true).Render(newBtnText)
	}
	titleLine := title
	newW := lipgloss.Width(newBtnText)
	pad := shInner - lipgloss.Width(title) - newW
	if pad > 0 {
		titleLine = title + strings.Repeat(" ", pad) + newStyled
		sheetX := MarginH + sidebarW + 1
		zones = append(zones, HitZone{
			X: sheetX + 2 + shInner - newW, Y: yOffset + 1, W: newW, H: 1,
			Type: ZoneExplorerNew, RepoIdx: sel, WTIdx: -1, Button: BtnCreateWT,
		})
	}

	// Column layout
	ageW, tmuxW, sizeW := 0, 0, 0
	if showAge {
		ageW = 6
	}
	if showTmux {
		tmuxW = 5
	}
	if showSize {
		sizeW = 13
	}
	// Status texts run up to ~21 cols ("3 changed | 2 to push") — give the
	// column full width when there's room, compact otherwise
	statusW := 16
	if shInner >= 60 {
		statusW = 22
	}
	branchW := shInner - statusW - sizeW - ageW - tmuxW - 2
	if branchW < 12 {
		branchW = 12
	}

	header := dimStyle.Render(fmt.Sprintf("  %-*s%-*s", branchW, "BRANCH", statusW, "STATUS"))
	if showSize {
		header += dimStyle.Render(fmt.Sprintf("%-*s", sizeW, "SIZE"))
	}
	if showAge {
		header += dimStyle.Render(fmt.Sprintf("%-*s", ageW, "AGE"))
	}
	if showTmux {
		header += dimStyle.Render("TMUX")
	}

	shLines := []string{truncate(titleLine, shInner), truncate(header, shInner)}
	// Sheet rows start after: border(1) + title(1) + column header(1);
	// the last inner line is reserved for the navigation hint
	rowStartY := yOffset + 3
	rowCapacity := innerH - 3

	if repo.NeedsMigration {
		notice := warnStyle.Render("⚠ existing work on "+repo.MigrationBranch) + dimStyle.Render(" — use the Board view to migrate")
		shLines = append(shLines, truncate(notice, shInner))
		rowStartY++
		rowCapacity--
	}

	shScroll := st.SheetScroll
	// Reserve one slot for the action strip when a row is selected & visible
	stripUsed := 0
	if st.FocusSheet && st.SelectedWT >= 0 && st.SelectedWT < len(repo.Worktrees) {
		stripUsed = 1
	}
	maxRows := rowCapacity - stripUsed
	if maxRows < 1 {
		maxRows = 1
	}
	if shScroll > len(repo.Worktrees)-maxRows {
		shScroll = len(repo.Worktrees) - maxRows
	}
	if shScroll < 0 {
		shScroll = 0
	}

	sheetX := MarginH + sidebarW + 1
	rowY := rowStartY
	for i := shScroll; i < len(repo.Worktrees) && i-shScroll < maxRows; i++ {
		wt := repo.Worktrees[i]
		isSel := st.FocusSheet && i == st.SelectedWT

		branch := wt.Branch
		if branch == "" {
			branch = wt.Name
		}
		branchCell := padCell(branch, branchW)
		statusCell := padCell(wt.StatusText, statusW)

		var line string
		if isSel {
			line = selStyle.Render("▸ "+branchCell) + statusStyle(wt.Status).Render(statusCell)
		} else {
			line = "  " + statusStyle(wt.Status).Render(branchCell) + statusStyle(wt.Status).Render(statusCell)
		}
		if showSize {
			line += dimStyle.Render(padCell(formatWTSize(sizes[wt.Path]), sizeW))
		}
		if showAge {
			line += dimStyle.Render(padCell(formatAge(wt.LastActivity), ageW))
		}
		if showTmux {
			if tmuxLive[opener.SessionName(wt.Path)] {
				line += tealStyle.Render("●")
			} else {
				line += dimStyle.Render(" ")
			}
		}
		shLines = append(shLines, truncate(line, shInner))
		zones = append(zones, HitZone{
			X: sheetX, Y: rowY, W: sheetW, H: 1,
			Type: ZoneExplorerRow, RepoIdx: sel, WTIdx: i,
		})
		rowY++

		// Action strip under the selected row
		if isSel {
			hasSession := tmuxLive[opener.SessionName(wt.Path)]
			actions := explorerActions(repo, hasSession, canPR)
			strip := "    "
			x := sheetX + 2 + lipgloss.Width(strip)
			for ai, a := range actions {
				seg := "[" + a.Label + "]"
				style := dimStyle
				if hoveredBtn != BtnNone && hoveredBtn == a.Btn {
					style = lipgloss.NewStyle().Foreground(ColorWhite).Background(ColorSelBg)
				}
				strip += style.Render(seg)
				segW := lipgloss.Width(seg)
				zones = append(zones, HitZone{
					X: x, Y: rowY, W: segW, H: 1,
					Type: ZoneExplorerAction, RepoIdx: sel, WTIdx: i, Button: a.Btn,
				})
				x += segW
				if ai < len(actions)-1 {
					strip += " "
					x++
				}
			}
			shLines = append(shLines, truncate(strip, shInner))
			rowY++
		}
	}
	if len(repo.Worktrees) == 0 && !repo.NeedsMigration {
		shLines = append(shLines, dimStyle.Render("  No worktrees — click [+ new] to create one"))
	}
	for len(shLines) < innerH-1 {
		shLines = append(shLines, "")
	}
	shLines = append(shLines, truncate(dimStyle.Render("↑/↓ navigate · ←/→ switch pane · ⏎ open · n new"), shInner))

	sheetBorder := CardBorderNormal
	if st.FocusSheet {
		sheetBorder = CardBorderFocused
	}
	sheet := sheetBorder.Width(sheetW - 2).Render(strings.Join(shLines[:innerH], "\n"))

	view := lipgloss.NewStyle().MarginLeft(MarginH).MarginRight(MarginH).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", sheet))

	return GridResult{View: view, HitZones: zones}
}

// padCell truncates/pads a plain string to exactly w display columns.
func padCell(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = truncatePlain(s, w-1)
	}
	if fill := w - lipgloss.Width(s); fill > 0 {
		s += strings.Repeat(" ", fill)
	}
	return s
}

// truncatePlain shortens a plain (unstyled) string to max runes with an
// ellipsis, for fixed-width table cells.
func truncatePlain(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}
