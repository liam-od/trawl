package ui

import "github.com/liam-od/trawl/internal/fs"

// panel is one side of the dual-pane view: a directory path, its listing, and
// the cursor/scroll state used to render and navigate it. It carries no I/O of
// its own — the Model loads entries asynchronously and assigns them here.
type panel struct {
	path    string
	entries []fs.Entry
	cursor  int
	offset  int
	active  bool
}

// moveUp moves the cursor toward the top, stopping at the first entry.
func (p *panel) moveUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

// moveDown moves the cursor toward the bottom, stopping at the last entry.
func (p *panel) moveDown() {
	if p.cursor < len(p.entries)-1 {
		p.cursor++
	}
}

// selected returns the entry under the cursor, or nil for an empty listing.
func (p *panel) selected() *fs.Entry {
	if len(p.entries) == 0 {
		return nil
	}
	return &p.entries[p.cursor]
}

// ensureVisible adjusts the scroll offset so the cursor stays within a window
// of the given height.
func (p *panel) ensureVisible(height int) {
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if height > 0 && p.cursor >= p.offset+height {
		p.offset = p.cursor - height + 1
	}
}
