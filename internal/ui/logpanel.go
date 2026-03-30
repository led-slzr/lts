package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// LogEntry represents a single log line with timestamp.
type LogEntry struct {
	Time    time.Time
	Context string // repo/operation context, e.g. "core"
	Message string
	IsError bool
}

// LogPanelModel holds the state for the scrollable log panel.
type LogPanelModel struct {
	Entries    []LogEntry
	ScrollPos  int  // 0 = showing latest entries, >0 = scrolled back
	MaxEntries int  // max entries to retain
	Visible    bool // whether the panel has content to show
}

// NewLogPanel creates a new log panel.
func NewLogPanel() LogPanelModel {
	return LogPanelModel{
		MaxEntries: 200,
	}
}

// Add appends a log entry.
func (lp *LogPanelModel) Add(ctx, msg string, isError bool) {
	lp.Entries = append(lp.Entries, LogEntry{
		Time:    time.Now(),
		Context: ctx,
		Message: msg,
		IsError: isError,
	})
	if len(lp.Entries) > lp.MaxEntries {
		lp.Entries = lp.Entries[len(lp.Entries)-lp.MaxEntries:]
	}
	lp.Visible = true
	// Auto-scroll to bottom when new entry arrives
	lp.ScrollPos = 0
}

// Clear removes all log entries and hides the panel.
func (lp *LogPanelModel) Clear() {
	lp.Entries = nil
	lp.ScrollPos = 0
	lp.Visible = false
}

// ScrollUp moves the viewport up (towards older entries).
func (lp *LogPanelModel) ScrollUp(lines int) {
	lp.ScrollPos += lines
	max := len(lp.Entries) - 1
	if max < 0 {
		max = 0
	}
	if lp.ScrollPos > max {
		lp.ScrollPos = max
	}
}

// ScrollDown moves the viewport down (towards newer entries).
func (lp *LogPanelModel) ScrollDown(lines int) {
	lp.ScrollPos -= lines
	if lp.ScrollPos < 0 {
		lp.ScrollPos = 0
	}
}

// RenderLogPanel renders the log panel below the footer.
// availHeight is how many lines are available for the panel.
func RenderLogPanel(lp LogPanelModel, width, availHeight int) string {
	if !lp.Visible || len(lp.Entries) == 0 {
		return ""
	}

	panelWidth := width - (MarginH * 2)
	if panelWidth < 24 {
		return "" // terminal too narrow for log panel
	}
	innerWidth := panelWidth - 4 // border(2) + padding(2)
	if innerWidth < 20 {
		innerWidth = 20
	}

	// Max visible lines: border takes 2 lines
	maxLines := availHeight - 2
	if maxLines < 1 {
		maxLines = 1
	}
	if maxLines > 12 {
		maxLines = 12
	}

	// Only show as many lines as we have entries (grow organically)
	total := len(lp.Entries)
	visibleLines := maxLines
	if total < visibleLines {
		visibleLines = total
	}

	// Compute window into entries
	endIdx := total - lp.ScrollPos
	if endIdx > total {
		endIdx = total
	}
	if endIdx < 0 {
		endIdx = 0
	}
	startIdx := endIdx - visibleLines
	if startIdx < 0 {
		startIdx = 0
	}

	hasOlder := startIdx > 0
	hasNewer := lp.ScrollPos > 0

	msgStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)
	errStyle := lipgloss.NewStyle().Foreground(ColorRed).Background(ColorBlack)
	timeStyle := lipgloss.NewStyle().Foreground(ColorGray).Background(ColorBlack)
	ctxStyle := lipgloss.NewStyle().Foreground(ColorGray).Background(ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBlack)

	var lines []string

	// Title line inside the border
	scrollHint := ""
	if hasOlder && hasNewer {
		scrollHint = "  ↑↓ scroll"
	} else if hasOlder {
		scrollHint = "  ↑ scroll for more"
	} else if hasNewer {
		scrollHint = "  ↓ scroll to latest"
	}
	titleLine := dimStyle.Render("LOGS") + dimStyle.Render(scrollHint)
	lines = append(lines, titleLine)

	// Log entries
	for i := startIdx; i < endIdx; i++ {
		entry := lp.Entries[i]
		ts := timeStyle.Render(entry.Time.Format("15:04:05"))

		ctx := ""
		ctxWidth := 0
		if entry.Context != "" {
			ctx = " " + ctxStyle.Render("["+entry.Context+"]")
			ctxWidth = len(entry.Context) + 3 // [ ] + space
		}

		var msg string
		msgMaxWidth := innerWidth - 10 - ctxWidth
		if msgMaxWidth < 10 {
			msgMaxWidth = 10
		}
		if entry.IsError {
			msg = errStyle.Render(truncate(entry.Message, msgMaxWidth))
		} else {
			msg = msgStyle.Render(truncate(entry.Message, msgMaxWidth))
		}
		lines = append(lines, ts+ctx+" "+msg)
	}

	content := strings.Join(lines, "\n")

	// Bordered content
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorGray).
		BorderBackground(ColorBlack).
		Background(ColorBlack).
		Padding(0, 1).
		Width(panelWidth).
		Render(content)

	return lipgloss.NewStyle().
		Margin(0, MarginH).
		Render(panel)
}
