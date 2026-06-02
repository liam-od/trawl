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
// height minus the status bar, the two borders, and the title line.
func (m Model) entryAreaHeight() int {
	h := m.height - statusLines - panelBorders - titleLines
	if h < 1 {
		return 1
	}
	return h
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

	return lipgloss.JoinVertical(lipgloss.Left, panels, m.renderStatus())
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

func (m Model) renderStatus() string {
	const hints = "[Tab] switch  [Enter] open  [F5/c] copy  [Backspace] up  [r] refresh  [q] quit"
	text := hints
	if m.status != "" {
		text = m.status + "  |  " + hints
	}
	return statusStyle.Width(m.width).Render(text)
}
