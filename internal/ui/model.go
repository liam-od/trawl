// Package ui implements the dual-pane terminal interface: a Bubble Tea program
// that renders a local and a remote panel side by side and navigates both. All
// filesystem access goes through fs.FS, so the model is agnostic to whether a
// side is local disk or a remote SFTP server.
package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/liam-od/trawl/internal/fs"
	"github.com/liam-od/trawl/internal/transfer"
)

// Transfer-rate smoothing. The rate is recomputed at most every rateWindow over
// the real elapsed wall-clock time since the last sample (never a fixed nominal
// interval), then exponentially smoothed. With these values the EMA settles over
// roughly a 1s window, so the bursty writes pkg/sftp produces under concurrent
// reads don't make the rate flicker. See the M4 carry-forward note in ROADMAP.md.
const (
	rateWindow = 250 * time.Millisecond
	rateAlpha  = 0.25
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

	// exclude holds the base-name globs pruned from directory transfers,
	// passed straight to transfer.Copy.
	exclude []string

	// queue holds all transfers; one runs at a time. The channels stream the
	// active transfer's progress and final result from its copy goroutine.
	queue        *transferQueue
	copyProgress chan transfer.CopyProgressMsg
	copyResult   chan error

	// Transfer-rate state for the active transfer, EMA-smoothed over real time.
	rateEMA        float64   // bytes/sec, 0 until the first sample
	lastRateBytes  int64     // cumulative bytes at the last rate sample
	lastRateSample time.Time // wall-clock time of the last rate sample
}

// New builds a Model with each panel rooted at the given start path. The two
// filesystems are supplied by the caller (local disk and/or a remote SFTP
// server); the model never constructs them itself.
func New(local, remote fs.FS, localStart, remoteStart string, exclude []string) Model {
	return Model{
		local:      panel{path: localStart, active: true},
		remote:     panel{path: remoteStart},
		localFS:    local,
		remoteFS:   remote,
		activePane: paneLocal,
		exclude:    exclude,
		queue:      &transferQueue{visible: true},
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
		it := m.queue.active()
		if it == nil {
			return m, nil // stale sample from a finished transfer
		}
		it.written = msg.Written
		it.total = msg.Total
		// Recompute the rate only once per rateWindow, over the true elapsed time
		// since the last sample, so the EMA averages over ~1s regardless of how
		// often samples arrive.
		now := time.Now()
		if elapsed := now.Sub(m.lastRateSample).Seconds(); elapsed >= rateWindow.Seconds() {
			m.rateEMA = emaRate(m.rateEMA, float64(msg.Written-m.lastRateBytes), elapsed)
			m.lastRateBytes = msg.Written
			m.lastRateSample = now
		}
		return m, waitForCopy(m.copyProgress, m.copyResult)

	case transfer.CopyDoneMsg:
		it := m.queue.active()
		m.copyProgress = nil
		m.copyResult = nil
		var cmds []tea.Cmd
		if it != nil {
			if msg.Err != nil {
				it.status = statusFailed
				it.err = msg.Err
				m.status = "copy failed: " + msg.Err.Error()
			} else {
				it.status = statusDone
				m.status = "copied " + it.name
				// Refresh the destination panel so the new entry appears.
				cmds = append(cmds, loadDir(m.fsFor(it.dstPane), it.dstPane, m.panelFor(it.dstPane).path))
			}
		}
		if next := m.startNext(); next != nil {
			cmds = append(cmds, next)
		}
		return m, tea.Batch(cmds...)
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

	case "f7":
		m.queue.visible = !m.queue.visible
		entryH := m.entryAreaHeight()
		m.local.ensureVisible(entryH)
		m.remote.ensureVisible(entryH)

	case "f5", "c":
		e := active.selected()
		if e == nil {
			return m, nil
		}
		// Files and directories both copy toward the inactive panel's directory;
		// transfer.Copy recurses when the source is a directory.
		dstPane := 1 - m.activePane
		dstFS := m.fsFor(dstPane)
		m.queue.enqueue(&queueItem{
			name:    e.Name,
			srcFS:   activeFS,
			srcPath: activeFS.Join(active.path, e.Name),
			dstFS:   dstFS,
			dstPath: dstFS.Join(m.panelFor(dstPane).path, e.Name),
			dstPane: dstPane,
		})
		m.queue.visible = true
		if m.queue.active() == nil {
			cmd := m.startNext()
			return m, cmd
		}
		m.status = "queued " + e.Name
		return m, nil
	}
	return m, nil
}

// emaRate returns the exponentially-smoothed transfer rate (bytes/sec) given the
// previous EMA, the bytes moved since the last sample, and the real elapsed
// seconds. A zero or negative interval leaves the rate unchanged; the first
// sample seeds the EMA directly.
func emaRate(prevEMA, deltaBytes, elapsedSec float64) float64 {
	if elapsedSec <= 0 {
		return prevEMA
	}
	inst := deltaBytes / elapsedSec
	if prevEMA == 0 {
		return inst
	}
	return rateAlpha*inst + (1-rateAlpha)*prevEMA
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
