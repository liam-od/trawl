// Package transfer streams a single file between two fs.FS instances (local
// disk or a remote SFTP server) and reports progress as it goes. Recursive
// directory copy, queues, and resume are out of scope for v1.
package transfer

import (
	"context"
	"fmt"
	"io"

	"github.com/liam-od/trawl/internal/fs"
)

// CopyProgressMsg reports the bytes written so far for an in-flight copy. Total
// is the source size when known (0 if unknown).
type CopyProgressMsg struct {
	Written int64
	Total   int64
}

// CopyDoneMsg reports the completion of a copy; Err is nil on success.
type CopyDoneMsg struct {
	Err error
}

// Copy streams the file at srcPath on src to dstPath on dst. As bytes are read
// it reports the running total on progress with non-blocking sends, so a slow
// consumer never stalls the transfer; progress may be nil. The copy aborts if
// ctx is cancelled. On any error a partial destination file may remain — atomic
// writes are deferred until recursive copy lands (see ROADMAP).
func Copy(ctx context.Context, src fs.FS, srcPath string, dst fs.FS, dstPath string, progress chan<- int64) error {
	r, err := src.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer r.Close()

	w, err := dst.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}

	cr := &countingReader{r: r, ctx: ctx, progress: progress}
	_, copyErr := io.Copy(w, cr)
	closeErr := w.Close()

	switch {
	case copyErr != nil:
		return fmt.Errorf("copy %s: %w", srcPath, copyErr)
	case closeErr != nil:
		return fmt.Errorf("close destination: %w", closeErr)
	}
	return nil
}

// countingReader wraps an io.Reader, accumulating the byte count and reporting
// it on progress after each read. It also makes the copy ctx-cancellable.
type countingReader struct {
	r        io.Reader
	ctx      context.Context
	written  int64
	progress chan<- int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if n > 0 {
		c.written += int64(n)
		if c.progress != nil {
			select {
			case c.progress <- c.written:
			default: // consumer is behind; drop this sample, the total only grows
			}
		}
	}
	return n, err
}
