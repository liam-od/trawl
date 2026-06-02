package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/liam-od/trawl/internal/fs"
	"github.com/liam-od/trawl/internal/transfer"
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

func TestModelCopyFlow(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	want := bytes.Repeat([]byte("trawl"), 40*1024) // 200 KiB
	if err := os.WriteFile(filepath.Join(srcDir, "f.bin"), want, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	local := fs.NewLocal()
	m := New(local, local, srcDir, dstDir)
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneLocal, path: srcDir, entries: []fs.Entry{{Name: "f.bin", Size: int64(len(want))}}})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneRemote, path: dstDir})

	// Copy the file under the (local) cursor toward the inactive (remote) pane.
	m, cmd := drive(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !m.copying {
		t.Fatal("expected copying flag set after 'c'")
	}

	// Pump the command chain to completion: progress samples re-issue the wait
	// command; CopyDoneMsg ends it and triggers a destination refresh.
	sawProgress, sawDone := false, false
	for i := 0; cmd != nil && i < 10000; i++ {
		msg := cmd()
		switch msg.(type) {
		case transfer.CopyProgressMsg:
			sawProgress = true
		case transfer.CopyDoneMsg:
			sawDone = true
		}
		m, cmd = drive(t, m, msg)
	}

	if !sawDone {
		t.Fatal("copy never reported done")
	}
	if !sawProgress {
		t.Error("copy reported no progress samples")
	}
	if m.copying {
		t.Error("copying flag still set after completion")
	}
	if !strings.HasPrefix(m.status, "copied ") {
		t.Errorf("status = %q, want a 'copied …' message", m.status)
	}

	got, err := os.ReadFile(filepath.Join(dstDir, "f.bin"))
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("copied bytes differ (%d vs %d)", len(got), len(want))
	}

	// A second copy attempt while one is in flight must be rejected — simulate
	// by forcing the flag and pressing 'c' again.
	m.copying = true
	m, _ = drive(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.status != "copy already in progress" {
		t.Errorf("re-entrant copy status = %q, want rejection", m.status)
	}
}

func TestModelCopyRejectsDirectory(t *testing.T) {
	local := fs.NewLocal()
	m := New(local, local, "/a", "/b")
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneLocal, path: "/a", entries: []fs.Entry{{Name: "sub", IsDir: true}}})

	m, cmd := drive(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.copying || cmd != nil {
		t.Fatal("copying a directory should be rejected, not started")
	}
	if !strings.Contains(m.status, "directory copy not supported") {
		t.Errorf("status = %q, want directory-copy rejection", m.status)
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
