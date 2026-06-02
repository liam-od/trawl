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
func (m Model) startCopy(name string, total int64, srcFS fs.FS, srcPath string, dstFS fs.FS, dstPath string, dstPane int) (tea.Model, tea.Cmd) {
	progress := make(chan int64, 64)
	result := make(chan error, 1)

	m.copying = true
	m.copyProgress = progress
	m.copyResult = result
	m.copyName = name
	m.copyTotal = total
	m.copyDstPane = dstPane
	m.rateEMA = 0
	m.lastRateBytes = 0
	m.lastRateSample = time.Now()
	m.status = formatCopyStatus(name, 0, total, 0)

	go func() {
		// M4 has no mid-copy cancel binding; Ctrl+C tears the program down and
		// the goroutine exits with it. Cancellation is post-v1.
		result <- transfer.Copy(context.Background(), srcFS, srcPath, dstFS, dstPath, progress)
	}()

	return m, waitForCopy(progress, result, total)
}

// waitForCopy blocks (off the event loop, inside a tea.Cmd) for the next copy
// event: either a progress sample or the final result. Update re-issues it after
// each CopyProgressMsg and stops on CopyDoneMsg. The result channel is buffered,
// so once Copy returns the done signal is always deliverable even if buffered
// progress samples go unread.
func waitForCopy(progress <-chan int64, result <-chan error, total int64) tea.Cmd {
	return func() tea.Msg {
		select {
		case written := <-progress:
			return transfer.CopyProgressMsg{Written: written, Total: total}
		case err := <-result:
			return transfer.CopyDoneMsg{Err: err}
		}
	}
}
