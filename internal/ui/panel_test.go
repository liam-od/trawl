package ui

import (
	"testing"

	"github.com/liam-od/trawl/internal/fs"
)

func entries(n int) []fs.Entry {
	es := make([]fs.Entry, n)
	for i := range es {
		es[i] = fs.Entry{Name: string(rune('a' + i))}
	}
	return es
}

func TestPanelMoveUp(t *testing.T) {
	tests := []struct {
		name        string
		entries     int
		startCursor int
		want        int
	}{
		{"empty stays at zero", 0, 0, 0},
		{"single entry stays", 1, 0, 0},
		{"mid moves up", 5, 3, 2},
		{"top clamps", 5, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := panel{entries: entries(tt.entries), cursor: tt.startCursor}
			p.moveUp()
			if p.cursor != tt.want {
				t.Errorf("cursor = %d, want %d", p.cursor, tt.want)
			}
		})
	}
}

func TestPanelMoveDown(t *testing.T) {
	tests := []struct {
		name        string
		entries     int
		startCursor int
		want        int
	}{
		{"empty stays at zero", 0, 0, 0},
		{"single entry clamps", 1, 0, 0},
		{"mid moves down", 5, 2, 3},
		{"bottom clamps", 5, 4, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := panel{entries: entries(tt.entries), cursor: tt.startCursor}
			p.moveDown()
			if p.cursor != tt.want {
				t.Errorf("cursor = %d, want %d", p.cursor, tt.want)
			}
		})
	}
}

func TestPanelSelected(t *testing.T) {
	if got := (&panel{}).selected(); got != nil {
		t.Errorf("empty panel: selected = %v, want nil", got)
	}
	p := panel{entries: entries(3), cursor: 1}
	if got := p.selected(); got == nil || got.Name != "b" {
		t.Errorf("selected = %v, want entry b", got)
	}
}

func TestPanelEnsureVisible(t *testing.T) {
	tests := []struct {
		name       string
		cursor     int
		offset     int
		height     int
		wantOffset int
	}{
		{"within window unchanged", 3, 0, 10, 0},
		{"cursor above window scrolls up", 2, 5, 10, 2},
		{"cursor below window scrolls down", 12, 0, 10, 3},
		{"cursor at window bottom edge", 9, 0, 10, 0},
		{"zero height is a no-op for the lower bound", 50, 4, 0, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := panel{cursor: tt.cursor, offset: tt.offset}
			p.ensureVisible(tt.height)
			if p.offset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", p.offset, tt.wantOffset)
			}
		})
	}
}
