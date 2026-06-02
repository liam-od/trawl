package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/liam-od/trawl/internal/fs"
)

// Minimum usable terminal size; below this the layout would collapse, so we
// show a hint instead of rendering a broken frame.
const (
	minWidth  = 40
	minHeight = 10
)

// Chrome accounting: each panel has a 1-cell border on top and bottom, plus a
// title line; the status bar takes the final row.
const (
	panelBorders = 2 // top + bottom
	titleLines   = 1
	statusLines  = 1
)

var (
	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("63"))

	inactiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240"))

	titleStyle  = lipgloss.NewStyle().Bold(true)
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	dirColor = lipgloss.Color("33")
)

// entryAreaHeight is the number of entry rows each panel can show: terminal
// height minus the status bar, the two borders, the title line, and the queue
// panel when it is showing.
func (m Model) entryAreaHeight() int {
	h := m.height - statusLines - panelBorders - titleLines
	if m.queueVisible() {
		h -= m.queuePanelHeight()
	}
	if h < 1 {
		return 1
	}
	return h
}

// queueVisible reports whether the queue panel should be drawn: the user hasn't
// hidden it and there is at least one transfer to show.
func (m Model) queueVisible() bool {
	return m.queue != nil && m.queue.visible && len(m.queue.items) > 0
}

// View renders the full frame: two panels side by side over a status bar.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("terminal too small (need at least %d×%d)", minWidth, minHeight)
	}

	half := m.width / 2
	// Panel content width excludes the left+right border cells.
	contentW := half - 2
	if contentW < 4 {
		contentW = 4
	}
	entryH := m.entryAreaHeight()
	// Panel height passed to lipgloss is the inner content height: entry rows
	// plus the title line. The border adds the surrounding two rows.
	contentH := entryH + titleLines

	left := renderPanel(m.local, "LOCAL", contentW, contentH, entryH)
	right := renderPanel(m.remote, "REMOTE", contentW, contentH, entryH)
	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	parts := []string{panels}
	if m.queueVisible() {
		parts = append(parts, m.renderQueuePanel())
	}
	parts = append(parts, m.renderStatus())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func renderPanel(p panel, label string, width, height, entryH int) string {
	title := titleStyle.Width(width).Render(label + "  " + shortenPath(p.path, width-len(label)-2))

	start := p.offset
	end := start + entryH
	if end > len(p.entries) {
		end = len(p.entries)
	}
	lines := make([]string, 0, entryH)
	for i, e := range p.entries[start:end] {
		lines = append(lines, renderEntryLine(e, start+i == p.cursor, width))
	}

	content := title
	if len(lines) > 0 {
		content += "\n" + strings.Join(lines, "\n")
	}

	border := inactiveBorderStyle
	if p.active {
		border = activeBorderStyle
	}
	return border.Width(width).Height(height).Render(content)
}

func renderEntryLine(e fs.Entry, isCursor bool, width int) string {
	const sizeW = 8
	nameW := width - sizeW - 1
	if nameW < 1 {
		nameW = 1
	}

	sizeStr := strings.Repeat(" ", sizeW)
	if !e.IsDir {
		sizeStr = fmt.Sprintf("%*s", sizeW, formatSize(e.Size))
	}

	name := e.Name
	if e.IsDir {
		name += "/"
	}
	if runes := []rune(name); len(runes) > nameW {
		if nameW <= 1 {
			name = "…"
		} else {
			name = string(runes[:nameW-1]) + "…"
		}
	}

	s := lipgloss.NewStyle().Width(width)
	if isCursor {
		s = s.Reverse(true)
	}
	if e.IsDir {
		s = s.Bold(true).Foreground(dirColor)
	}
	return s.Render(fmt.Sprintf("%-*s %s", nameW, name, sizeStr))
}

// shortenPath trims a path from the left with a leading ellipsis so it fits in
// max columns.
func shortenPath(p string, max int) string {
	if max < 1 {
		max = 1
	}
	if len(p) <= max {
		return p
	}
	if max == 1 {
		return "…"
	}
	return "…" + p[len(p)-(max-1):]
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// maxQueueRows caps how many transfer rows the queue panel shows at once.
const maxQueueRows = 8

// queuePanelHeight is the queue panel's total height: one row per shown item
// (capped at maxQueueRows), plus the title line and the top/bottom border. It
// grows with the queue so a single transfer gets a compact panel rather than a
// fixed block of blank rows.
func (m Model) queuePanelHeight() int {
	n := len(m.queue.items)
	if n > maxQueueRows {
		n = maxQueueRows
	}
	return n + 3 // title + top border + bottom border
}

var (
	queueBorderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240"))
	queueTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	pendingRowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	doneRowStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	failedRowStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// renderQueuePanel draws the transfer queue: a fixed-height bordered panel with
// one row per item, scrolled to keep the active transfer in view.
func (m Model) renderQueuePanel() string {
	innerW := m.width - 2
	if innerW < 4 {
		innerW = 4
	}
	items := m.queue.items
	title := queueTitleStyle.Width(innerW).Render(fmt.Sprintf("Transfer queue (%d)", len(items)))

	window := len(items)
	if window > maxQueueRows {
		window = maxQueueRows
	}
	start := queueStart(items)

	rows := make([]string, 0, window)
	for _, it := range items[start : start+window] {
		rows = append(rows, renderQueueRow(it, m.rateEMA, innerW))
	}

	content := title + "\n" + strings.Join(rows, "\n")
	return queueBorderStyle.Width(innerW).Render(content)
}

// queueStart picks the first row index to display so the active item (or, if
// none is active, the last item) stays visible.
func queueStart(items []*queueItem) int {
	if len(items) <= maxQueueRows {
		return 0
	}
	focus := len(items) - 1
	for i, it := range items {
		if it.status == statusActive {
			focus = i
			break
		}
	}
	start := focus - maxQueueRows/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(items) - maxQueueRows; start > maxStart {
		start = maxStart
	}
	return start
}

func renderQueueRow(it *queueItem, rateEMA float64, width int) string {
	arrow := "←" // destination is local: a download
	if it.dstPane == paneRemote {
		arrow = "→"
	}
	switch it.status {
	case statusActive:
		var pct int64
		if it.total > 0 {
			pct = it.written * 100 / it.total
		}
		rate := ""
		if rateEMA > 0 {
			rate = "  " + formatSize(int64(rateEMA)) + "/s"
		}
		text := fmt.Sprintf(" ▶ %s %s %s %3d%%%s", arrow, it.name, renderProgressBar(pct, 16), pct, rate)
		return lipgloss.NewStyle().Width(width).Render(clip(text, width))
	case statusDone:
		return doneRowStyle.Width(width).Render(clip(fmt.Sprintf(" ✓ %s %s", arrow, it.name), width))
	case statusFailed:
		reason := "failed"
		if it.err != nil {
			reason = it.err.Error()
		}
		return failedRowStyle.Width(width).Render(clip(fmt.Sprintf(" ✗ %s %s: %s", arrow, it.name, reason), width))
	default: // pending
		return pendingRowStyle.Width(width).Render(clip(fmt.Sprintf(" · %s %s", arrow, it.name), width))
	}
}

func renderProgressBar(pct int64, width int) string {
	filled := int(pct) * width / 100
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// clip truncates s to at most width runes, adding an ellipsis when it overflows.
func clip(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
}

func (m Model) renderStatus() string {
	const hints = "[Tab] switch  [Enter] open  [F5/c] copy  [F7] queue  [Backspace] up  [r] refresh  [q] quit"
	text := hints
	if m.status != "" {
		text = m.status + "  |  " + hints
	}
	return statusStyle.Width(m.width).Render(text)
}
