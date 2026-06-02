package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/liam-od/trawl/internal/fs"
	"github.com/liam-od/trawl/internal/transfer"
)

// startCopy launches a single-file copy from the active panel toward the
// inactive panel's directory and returns the command that streams its progress.
// The caller has already ensured no copy is in flight and that the entry is a
// regular file.
func (m Model) startCopy(name string, srcFS fs.FS, srcPath string, dstFS fs.FS, dstPath string, dstPane int) (tea.Model, tea.Cmd) {
	progress := make(chan transfer.CopyProgressMsg, 64)
	result := make(chan error, 1)

	m.copying = true
	m.copyProgress = progress
	m.copyResult = result
	m.copyName = name
	m.copyDstPane = dstPane
	m.rateEMA = 0
	m.lastRateBytes = 0
	m.lastRateSample = time.Now()
	m.status = formatCopyStatus(name, 0, 0, 0)

	go func() {
		// No mid-copy cancel binding yet; Ctrl+C tears the program down and the
		// goroutine exits with it. Cancellation is post-v1. transfer.Copy recurses
		// when srcPath is a directory.
		result <- transfer.Copy(context.Background(), srcFS, srcPath, dstFS, dstPath, progress)
	}()

	return m, waitForCopy(progress, result)
}

// waitForCopy blocks (off the event loop, inside a tea.Cmd) for the next copy
// event: either a progress sample or the final result. Update re-issues it after
// each CopyProgressMsg and stops on CopyDoneMsg. The result channel is buffered,
// so once Copy returns the done signal is always deliverable even if buffered
// progress samples go unread.
func waitForCopy(progress <-chan transfer.CopyProgressMsg, result <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case p := <-progress:
			return p
		case err := <-result:
			return transfer.CopyDoneMsg{Err: err}
		}
	}
}
