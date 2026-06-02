package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/liam-od/trawl/internal/transfer"
)

// startNext marks the next pending queue item active and launches its transfer,
// returning the command that streams its progress. It returns nil when nothing
// is pending (queue empty or all finished). The caller has already ensured no
// other transfer is active.
func (m *Model) startNext() tea.Cmd {
	it := m.queue.nextPending()
	if it == nil {
		return nil
	}
	it.status = statusActive

	progress := make(chan transfer.CopyProgressMsg, 64)
	result := make(chan error, 1)
	m.copyProgress = progress
	m.copyResult = result
	m.rateEMA = 0
	m.lastRateBytes = 0
	m.lastRateSample = time.Now()

	src, srcPath, dst, dstPath := it.srcFS, it.srcPath, it.dstFS, it.dstPath
	go func() {
		// No mid-copy cancel binding yet; Ctrl+C tears the program down and the
		// goroutine exits with it. transfer.Copy recurses when srcPath is a dir.
		result <- transfer.Copy(context.Background(), src, srcPath, dst, dstPath, progress)
	}()

	return waitForCopy(progress, result)
}

// waitForCopy blocks (off the event loop, inside a tea.Cmd) for the next event
// from the active transfer: a progress sample or the final result. Update
// re-issues it after each CopyProgressMsg and stops on CopyDoneMsg.
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
