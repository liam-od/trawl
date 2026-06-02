package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/liam-od/trawl/internal/fs"
)

// drive applies a message and returns the concrete Model back.
func drive(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want ui.Model", next)
	}
	return got, cmd
}

func TestModelInitLoadsBothPanes(t *testing.T) {
	local := fs.NewLocal()
	m := New(local, local, ".", ".")
	if m.Init() == nil {
		t.Fatal("Init returned nil command, want batched directory loads")
	}
}

func TestModelDirLoadedAndNavigation(t *testing.T) {
	local := fs.NewLocal()
	m := New(local, local, "/start", "/remote")
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	loaded := dirLoadedMsg{
		pane: paneLocal,
		path: "/start",
		entries: []fs.Entry{
			{Name: "dir", IsDir: true},
			{Name: "file.txt", Size: 100},
		},
	}
	m, _ = drive(t, m, loaded)

	if len(m.local.entries) != 2 {
		t.Fatalf("local entries = %d, want 2", len(m.local.entries))
	}

	// Down then up returns to the top; up at the top clamps.
	m, _ = drive(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.local.cursor != 1 {
		t.Fatalf("after down: cursor = %d, want 1", m.local.cursor)
	}
	m, _ = drive(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.local.cursor != 0 {
		t.Fatalf("after up: cursor = %d, want 0", m.local.cursor)
	}

	// Tab moves focus to the remote pane.
	m, _ = drive(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.activePane != paneRemote || !m.remote.active || m.local.active {
		t.Fatalf("after tab: activePane=%d local.active=%v remote.active=%v", m.activePane, m.local.active, m.remote.active)
	}

	// Enter on the active (remote) pane's empty listing is a no-op (no panic).
	if _, cmd := drive(t, m, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter on empty pane should not issue a load command")
	}
}

func TestModelResizeKeepsCursorVisible(t *testing.T) {
	local := fs.NewLocal()
	m := New(local, local, "/start", "/remote")
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 40})

	es := make([]fs.Entry, 30)
	for i := range es {
		es[i] = fs.Entry{Name: string(rune('a' + i%26))}
	}
	m, _ = drive(t, m, dirLoadedMsg{pane: paneLocal, path: "/start", entries: es})

	// Move the cursor well down the tall listing.
	for range 20 {
		m, _ = drive(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.local.cursor != 20 {
		t.Fatalf("cursor = %d, want 20", m.local.cursor)
	}

	// Shrink the terminal; the offset must follow so the cursor stays visible.
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 10})
	entryH := m.entryAreaHeight()
	if m.local.cursor < m.local.offset || m.local.cursor >= m.local.offset+entryH {
		t.Errorf("cursor %d not within window [%d, %d) after resize", m.local.cursor, m.local.offset, m.local.offset+entryH)
	}
}

func TestModelEnterOnDirIssuesLoad(t *testing.T) {
	local := fs.NewLocal()
	m := New(local, local, "/start", "/remote")
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneLocal, path: "/start", entries: []fs.Entry{{Name: "sub", IsDir: true}}})

	_, cmd := drive(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a directory should issue a load command")
	}
}

func TestModelQuitKeys(t *testing.T) {
	local := fs.NewLocal()
	m := New(local, local, "/", "/")
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyCtrlC},
	} {
		if _, cmd := drive(t, m, key); cmd == nil {
			t.Errorf("key %v: expected quit command, got nil", key)
		}
	}
}

func TestModelViewRenders(t *testing.T) {
	local := fs.NewLocal()
	m := New(local, local, "/home/user", "/tmp")

	// Before a size is known, View shows a placeholder rather than panicking.
	if got := m.View(); got != "loading…" {
		t.Errorf("pre-size View = %q, want loading…", got)
	}

	// Too-small terminals get a hint, not a broken frame.
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 10, Height: 4})
	if !strings.Contains(m.View(), "too small") {
		t.Errorf("small View = %q, want a too-small hint", m.View())
	}

	// A normal size renders both panel labels and the status hints.
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneLocal, path: "/home/user", entries: []fs.Entry{{Name: "f", Size: 2048}}})
	out := m.View()
	for _, want := range []string{"LOCAL", "REMOTE", "[Tab] switch", "[q] quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q\n%s", want, out)
		}
	}
}
