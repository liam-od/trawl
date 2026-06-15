package ui

import (
	"bytes"
	"os"
	"path/filepath"
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
	m := New(local, local, ".", ".", nil)
	if m.Init() == nil {
		t.Fatal("Init returned nil command, want batched directory loads")
	}
}

func TestModelDirLoadedAndNavigation(t *testing.T) {
	local := fs.NewLocal()
	m := New(local, local, "/start", "/remote", nil)
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
	m := New(local, local, "/start", "/remote", nil)
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
	m := New(local, local, "/start", "/remote", nil)
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneLocal, path: "/start", entries: []fs.Entry{{Name: "sub", IsDir: true}}})

	_, cmd := drive(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a directory should issue a load command")
	}
}

// run drives a command chain to completion, expanding tea.BatchMsg, and returns
// the final model. It blocks on commands (e.g. waitForCopy) until their
// goroutines produce, so use it only on flows that terminate.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = run(t, m, c)
		}
		return m
	}
	next, c := m.Update(msg)
	return run(t, next.(Model), c)
}

func TestModelCopyFlow(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	want := bytes.Repeat([]byte("trawl"), 40*1024) // 200 KiB
	if err := os.WriteFile(filepath.Join(srcDir, "f.bin"), want, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	local := fs.NewLocal()
	m := New(local, local, srcDir, dstDir, nil)
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneLocal, path: srcDir, entries: []fs.Entry{{Name: "f.bin", Size: int64(len(want))}}})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneRemote, path: dstDir})

	// 'c' enqueues the file and starts the transfer; the queue panel shows.
	m, cmd := drive(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.queue.active() == nil {
		t.Fatal("expected an active transfer after 'c'")
	}
	if !m.queueVisible() {
		t.Error("queue panel should be visible during a copy")
	}

	m = run(t, m, cmd)

	if a := m.queue.active(); a != nil {
		t.Errorf("transfer still active after completion: %+v", a)
	}
	if len(m.queue.items) != 1 || m.queue.items[0].status != statusDone {
		t.Fatalf("queue item not marked done: %+v", m.queue.items)
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
}

func TestModelCopyDirectory(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	treeRoot := filepath.Join(srcDir, "tree")
	if err := os.MkdirAll(filepath.Join(treeRoot, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := bytes.Repeat([]byte("x"), 4096)
	if err := os.WriteFile(filepath.Join(treeRoot, "sub", "f.bin"), content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	local := fs.NewLocal()
	m := New(local, local, srcDir, dstDir, nil)
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneLocal, path: srcDir, entries: []fs.Entry{{Name: "tree", IsDir: true}}})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneRemote, path: dstDir})

	// Copying a directory starts a recursive transfer.
	m, cmd := drive(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.queue.active() == nil {
		t.Fatal("directory copy should start")
	}
	m = run(t, m, cmd)

	if m.queue.items[0].status != statusDone {
		t.Fatalf("dir transfer not done: status=%v", m.queue.items[0].status)
	}
	got, err := os.ReadFile(filepath.Join(dstDir, "tree", "sub", "f.bin"))
	if err != nil {
		t.Fatalf("read copied tree file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("copied tree file differs from source")
	}
}

func TestModelQueueMultiple(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	for _, name := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join(srcDir, name), bytes.Repeat([]byte("z"), 2048), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	local := fs.NewLocal()
	m := New(local, local, srcDir, dstDir, nil)
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneLocal, path: srcDir, entries: []fs.Entry{{Name: "a.bin", Size: 2048}, {Name: "b.bin", Size: 2048}}})
	m, _ = drive(t, m, dirLoadedMsg{pane: paneRemote, path: dstDir})

	// First 'c' starts a.bin; move the cursor down; second 'c' queues b.bin behind it.
	m, cmd := drive(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m, _ = drive(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, cmd2 := drive(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if len(m.queue.items) != 2 {
		t.Fatalf("queue has %d items, want 2", len(m.queue.items))
	}
	if cmd2 != nil {
		t.Error("second copy while one is active should be queued, not started")
	}

	m = run(t, m, cmd) // draining the first transfer starts the second via startNext

	for _, it := range m.queue.items {
		if it.status != statusDone {
			t.Errorf("item %s status = %v, want done", it.name, it.status)
		}
	}
	for _, name := range []string{"a.bin", "b.bin"} {
		if _, err := os.ReadFile(filepath.Join(dstDir, name)); err != nil {
			t.Errorf("copied %s missing: %v", name, err)
		}
	}
}

func TestEMARate(t *testing.T) {
	// First sample seeds the EMA with the instantaneous rate.
	if got := emaRate(0, 1_000_000, 1.0); got != 1_000_000 {
		t.Errorf("first sample: got %v, want 1e6", got)
	}
	// Subsequent sample blends: alpha*inst + (1-alpha)*prev.
	if got := emaRate(1_000_000, 2_000_000, 1.0); got != rateAlpha*2_000_000+(1-rateAlpha)*1_000_000 {
		t.Errorf("blend: got %v", got)
	}
	// A non-positive interval leaves the rate unchanged (no divide-by-zero).
	if got := emaRate(1_000_000, 500_000, 0); got != 1_000_000 {
		t.Errorf("zero elapsed: got %v, want unchanged 1e6", got)
	}
}

func TestRenderProgressBar(t *testing.T) {
	if got := renderProgressBar(0, 10); got != "[░░░░░░░░░░]" {
		t.Errorf("0%%: %q", got)
	}
	if got := renderProgressBar(100, 10); got != "[██████████]" {
		t.Errorf("100%%: %q", got)
	}
	if got := renderProgressBar(50, 10); got != "[█████░░░░░]" {
		t.Errorf("50%%: %q", got)
	}
	// Out-of-range percentages clamp rather than overflow the bar.
	if got := renderProgressBar(150, 4); got != "[████]" {
		t.Errorf(">100%% should clamp: %q", got)
	}
}

func TestModelViewShowsQueue(t *testing.T) {
	local := fs.NewLocal()
	m := New(local, local, "/a", "/b", nil)
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	m.queue.items = []*queueItem{
		{name: "report.pdf", dstPane: paneRemote, status: statusDone},
		{name: "video.mp4", dstPane: paneLocal, status: statusActive, written: 50, total: 100},
	}
	out := m.View()
	for _, want := range []string{"Transfer queue", "report.pdf", "video.mp4", "50%"} {
		if !strings.Contains(out, want) {
			t.Errorf("queue panel missing %q\n%s", want, out)
		}
	}

	// F7 hides the panel even with items present.
	m, _ = drive(t, m, tea.KeyMsg{Type: tea.KeyF7})
	if strings.Contains(m.View(), "Transfer queue") {
		t.Error("queue panel should be hidden after F7")
	}
}

func TestModelQuitKeys(t *testing.T) {
	local := fs.NewLocal()
	m := New(local, local, "/", "/", nil)
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
	m := New(local, local, "/home/user", "/tmp", nil)

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
