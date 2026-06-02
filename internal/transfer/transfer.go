// Package transfer streams a single file between two fs.FS instances (local
// disk or a remote SFTP server) and reports progress as it goes. Recursive
// directory copy, queues, and resume are out of scope for v1.
package transfer

import (
	"context"
	"fmt"
	"io"

	"github.com/pkg/sftp"

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

// Copy streams the file at srcPath on src to dstPath on dst, reporting the
// running byte total on progress with non-blocking sends (progress may be nil).
// The copy aborts if ctx is cancelled. On any error a partial destination file
// may remain — atomic writes are deferred until recursive copy lands (ROADMAP).
//
// Throughput note: pkg/sftp only parallelises its SSH requests through
// sftp.File.WriteTo (downloads) and sftp.File.ReadFrom (uploads), which io.Copy
// selects by dynamic type. Wrapping the SFTP file in a counter would hide those
// methods and collapse the transfer to one blocking request at a time — crippling
// throughput on a high-latency link. So the byte counter goes on the *other*
// side: when downloading we count the writer (and let WriteTo drive concurrent
// reads); otherwise we count the reader (and let an SFTP destination's ReadFrom
// drive concurrent writes). Note *os.File implements both WriteTo and ReadFrom,
// so the SFTP file must be identified by type, not by interface.
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

	var copyErr error
	if _, ok := r.(*sftp.File); ok {
		// Download: io.Copy(dst, sftpSrc) uses sftpSrc.WriteTo → concurrent reads.
		cw := &countingWriter{w: w, ctx: ctx, progress: progress}
		_, copyErr = io.Copy(cw, r)
	} else {
		// Upload / local: io.Copy(dst, countingReader) lets an SFTP destination's
		// ReadFrom run (concurrent writes); a local destination stays sequential.
		cr := &countingReader{r: r, ctx: ctx, progress: progress}
		_, copyErr = io.Copy(w, cr)
	}
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

// countingWriter wraps an io.Writer, accumulating the byte count and reporting
// it on progress after each write. It is used on the download path so the SFTP
// source's concurrent WriteTo is preserved while still tracking progress. It
// also makes the copy ctx-cancellable.
type countingWriter struct {
	w        io.Writer
	ctx      context.Context
	written  int64
	progress chan<- int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.w.Write(p)
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
