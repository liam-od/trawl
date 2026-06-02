// Package ui implements the dual-pane terminal interface: a Bubble Tea program
// that renders a local and a remote panel side by side and navigates both. All
// filesystem access goes through fs.FS, so the model is agnostic to whether a
// side is local disk or a remote SFTP server.
package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/liam-od/trawl/internal/fs"
	"github.com/liam-od/trawl/internal/transfer"
)

// pane identifiers.
const (
	paneLocal = iota
	paneRemote
)

// dirLoadedMsg is emitted by loadDir when an asynchronous directory read
// finishes. A non-nil err means the read failed and the panel is left as-is.
type dirLoadedMsg struct {
	pane    int
	entries []fs.Entry
	path    string
	err     error
}

// Model is the root Bubble Tea model holding both panels and the filesystem for
// each side.
type Model struct {
	local    panel
	remote   panel
	localFS  fs.FS
	remoteFS fs.FS

	activePane int
	width      int
	height     int
	status     string

	// Single in-flight copy. copying gates a second copy from starting; the
	// channels stream progress and the final result from the copy goroutine.
	copying      bool
	copyProgress chan int64
	copyResult   chan error
	copyName     string
	copyTotal    int64
	copyDstPane  int
}

// New builds a Model with each panel rooted at the given start path. The two
// filesystems are supplied by the caller (local disk and/or a remote SFTP
// server); the model never constructs them itself.
func New(local, remote fs.FS, localStart, remoteStart string) Model {
	return Model{
		local:      panel{path: localStart, active: true},
		remote:     panel{path: remoteStart},
		localFS:    local,
		remoteFS:   remote,
		activePane: paneLocal,
	}
}

// Init kicks off the initial load of both directories.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadDir(m.localFS, paneLocal, m.local.path),
		loadDir(m.remoteFS, paneRemote, m.remote.path),
	)
}

// loadDir reads a directory off the Bubble Tea event loop and reports the
// result as a dirLoadedMsg. Update must never read directories inline — all FS
// access happens here, inside a tea.Cmd.
func loadDir(fsys fs.FS, pane int, dirPath string) tea.Cmd {
	return func() tea.Msg {
		entries, err := fsys.ReadDir(dirPath)
		return dirLoadedMsg{pane: pane, entries: entries, path: dirPath, err: err}
	}
}

// Update handles a single message and returns the next model plus any command.
// It performs no blocking I/O; directory reads are dispatched as commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Keep both cursors on-screen after a reflow: a shorter terminal can
		// otherwise leave the cursor scrolled out of the visible window until
		// the next keypress.
		entryH := m.entryAreaHeight()
		m.local.ensureVisible(entryH)
		m.remote.ensureVisible(entryH)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case dirLoadedMsg:
		m.applyDirLoaded(msg)
		return m, nil

	case transfer.CopyProgressMsg:
		if !m.copying {
			return m, nil // stale sample from a finished copy
		}
		if m.copyTotal > 0 {
			m.status = fmt.Sprintf("Copying %s… %d%%", m.copyName, msg.Written*100/m.copyTotal)
		} else {
			m.status = fmt.Sprintf("Copying %s…", m.copyName)
		}
		return m, waitForCopy(m.copyProgress, m.copyResult, m.copyTotal)

	case transfer.CopyDoneMsg:
		m.copying = false
		m.copyProgress = nil
		m.copyResult = nil
		if msg.Err != nil {
			m.status = "copy failed: " + msg.Err.Error()
			return m, nil
		}
		m.status = fmt.Sprintf("copied %s", m.copyName)
		// Refresh the destination panel so the new file appears; same path, so
		// applyDirLoaded preserves the cursor.
		dstFS := m.fsFor(m.copyDstPane)
		return m, loadDir(dstFS, m.copyDstPane, m.panelFor(m.copyDstPane).path)
	}
	return m, nil
}

// applyDirLoaded installs a freshly loaded listing into the target panel,
// resetting the cursor when the path changed and clamping it otherwise.
func (m *Model) applyDirLoaded(msg dirLoadedMsg) {
	if msg.err != nil {
		m.status = "error: " + msg.err.Error()
		return
	}
	p := m.panelFor(msg.pane)
	if msg.path != p.path {
		p.cursor = 0
		p.offset = 0
	}
	p.entries = msg.entries
	p.path = msg.path
	if p.cursor >= len(p.entries) {
		p.cursor = max(0, len(p.entries)-1)
		p.offset = 0
	}
	// Note: a successful load does not clear m.status — that would wipe the
	// "copied X" message produced by the post-copy destination refresh. Status
	// is owned by the actions that set it (errors, copy results).
}

// handleKey maps a keypress to navigation of the active panel. Directory reads
// are returned as commands rather than performed inline.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	active := m.panelFor(m.activePane)
	activeFS := m.fsFor(m.activePane)
	entryH := m.entryAreaHeight()

	switch msg.String() {
	case "q", "f10", "ctrl+c":
		return m, tea.Quit

	case "tab":
		m.local.active = !m.local.active
		m.remote.active = !m.remote.active
		m.activePane = 1 - m.activePane

	case "up", "k":
		active.moveUp()
		active.ensureVisible(entryH)

	case "down", "j":
		active.moveDown()
		active.ensureVisible(entryH)

	case "enter":
		e := active.selected()
		if e == nil || !e.IsDir {
			return m, nil
		}
		return m, loadDir(activeFS, m.activePane, activeFS.Join(active.path, e.Name))

	case "backspace", "h":
		// Join with ".." yields the cleaned parent for both POSIX and OS paths,
		// and stays put at the filesystem root.
		return m, loadDir(activeFS, m.activePane, activeFS.Join(active.path, ".."))

	case "r":
		return m, loadDir(activeFS, m.activePane, active.path)

	case "f5", "c":
		if m.copying {
			m.status = "copy already in progress"
			return m, nil
		}
		e := active.selected()
		if e == nil {
			return m, nil
		}
		if e.IsDir {
			m.status = "directory copy not supported yet"
			return m, nil
		}
		dstPane := 1 - m.activePane
		dstFS := m.fsFor(dstPane)
		srcPath := activeFS.Join(active.path, e.Name)
		dstPath := dstFS.Join(m.panelFor(dstPane).path, e.Name)
		return m.startCopy(e.Name, e.Size, activeFS, srcPath, dstFS, dstPath, dstPane)
	}
	return m, nil
}

// panelFor returns a pointer to the panel for the given pane id.
func (m *Model) panelFor(pane int) *panel {
	if pane == paneRemote {
		return &m.remote
	}
	return &m.local
}

// fsFor returns the filesystem backing the given pane id.
func (m *Model) fsFor(pane int) fs.FS {
	if pane == paneRemote {
		return m.remoteFS
	}
	return m.localFS
}
