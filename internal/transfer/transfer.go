// Package transfer streams files and directory trees between two fs.FS
// instances (local disk or a remote SFTP server) and reports progress as it
// goes. Multi-transfer queues and resume are out of scope for v1.
package transfer

import (
	"context"
	"fmt"
	"io"
	iofs "io/fs"
	"path"
	"strings"

	"github.com/pkg/sftp"

	"github.com/liam-od/trawl/internal/fs"
)

// CopyProgressMsg reports the running byte total for an in-flight copy. Total is
// the size of the whole transfer when known (0 while a directory is still being
// scanned).
type CopyProgressMsg struct {
	Written int64
	Total   int64
}

// CopyDoneMsg reports the completion of a copy; Err is nil on success.
type CopyDoneMsg struct {
	Err error
}

// Copy transfers srcPath on src to dstPath on dst, recursing if srcPath is a
// directory (dstPath is then the destination directory to create). exclude lists
// base-name globs (path.Match syntax) pruned from a directory walk — a matching
// directory is skipped whole — and does not apply to an explicitly named single
// file. It reports CopyProgressMsg values on progress with non-blocking sends
// (progress may be nil) and aborts if ctx is cancelled. On error a partial
// destination may remain — atomic writes are deferred until post-v1.
//
// dstPath is used exactly as given — callers pick (and confirm) the top-level
// name — but nested entries of a tree pass through dst.CleanName so names the
// destination filesystem rejects are rewritten rather than failing the copy.
func Copy(ctx context.Context, src fs.FS, srcPath string, dst fs.FS, dstPath string, exclude []string, progress chan<- CopyProgressMsg) error {
	info, err := src.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	c := &counter{ctx: ctx, progress: progress}
	if info.IsDir {
		return copyTree(c, src, srcPath, dst, dstPath, excluder(exclude))
	}
	c.total = info.Size
	return copyFile(c, src, srcPath, dst, dstPath)
}

// excluder compiles base-name glob patterns into a predicate reporting whether
// an entry name should be skipped. A malformed pattern (path.ErrBadPattern)
// simply never matches, so a typo can't abort a transfer.
func excluder(patterns []string) func(name string) bool {
	return func(name string) bool {
		for _, p := range patterns {
			if ok, _ := path.Match(p, name); ok {
				return true
			}
		}
		return false
	}
}

// copyTree copies the directory rooted at srcRoot into dstRoot, creating dstRoot
// and mirroring the tree. It walks twice: once to total the bytes (so progress
// has a denominator), then once to create directories and copy files. The
// counter accumulates across every file so progress is cumulative for the tree.
func copyTree(c *counter, src fs.FS, srcRoot string, dst fs.FS, dstRoot string, skip func(name string) bool) error {
	var total int64
	if err := src.Walk(srcRoot, func(rel string, e fs.Entry, err error) error {
		if err != nil {
			return err
		}
		if rel != "" && skip(e.Name) {
			if e.IsDir {
				return fs.SkipDir
			}
			return nil
		}
		if e.Mode&iofs.ModeSymlink != 0 {
			return nil // symlinks aren't streamable; skip rather than deref
		}
		if !e.IsDir {
			total += e.Size
		}
		return nil
	}); err != nil {
		return fmt.Errorf("scan %s: %w", srcRoot, err)
	}
	c.total = total
	c.report() // publish the total before any bytes move

	nc := newNameCleaner(dst, dstRoot)
	return src.Walk(srcRoot, func(rel string, e fs.Entry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", srcRoot, err)
		}
		if rel != "" && skip(e.Name) {
			if e.IsDir {
				return fs.SkipDir // prune to match the scan pass, so totals agree
			}
			return nil
		}
		if e.Mode&iofs.ModeSymlink != 0 {
			return nil // skip, matching the scan pass
		}
		destPath := nc.dest(rel)
		if e.IsDir {
			return dst.MkdirAll(destPath)
		}
		return copyFile(c, src, joinRel(src, srcRoot, rel), dst, destPath)
	})
}

// joinRel resolves a slash-separated path relative to root into fsys's native
// path. An empty rel denotes the root itself.
func joinRel(fsys fs.FS, root, rel string) string {
	if rel == "" {
		return root
	}
	return fsys.Join(append([]string{root}, strings.Split(rel, "/")...)...)
}

// nameCleaner is joinRel for destination paths: every component of a source-
// relative path passes through fsys.CleanName, so entries legal on the source
// filesystem land under names the destination accepts (e.g. a ':' in a name
// downloaded to Windows). Cleaning is silent — callers confirm the top-level
// name with the user before the copy starts, but aborting a tree mid-transfer
// over one nested name would be worse than renaming it.
//
// Cleaning can collapse two distinct source names in the same directory to one
// legal name ("ep:1" and "ep?1" both clean to "ep_1" on Windows). Left alone the
// second entry would silently overwrite the first (or a dir/file collision would
// abort the copy), so nameCleaner disambiguates: the second claimant of a cleaned
// name gets a " (2)" suffix. Walk is pre-order, so a renamed directory is
// recorded before any child resolves its parent, keeping the subtree consistent.
type nameCleaner struct {
	fsys     fs.FS
	assigned map[string]string   // source rel -> chosen destination path
	taken    map[string]struct{} // destination paths already claimed
}

func newNameCleaner(fsys fs.FS, root string) *nameCleaner {
	return &nameCleaner{
		fsys:     fsys,
		assigned: map[string]string{"": root},
		taken:    map[string]struct{}{root: {}},
	}
}

// dest returns the destination path for a source-relative path, cleaning every
// component and disambiguating collisions within a directory.
func (nc *nameCleaner) dest(rel string) string {
	if d, ok := nc.assigned[rel]; ok {
		return d
	}
	parentRel, leaf := "", rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		parentRel, leaf = rel[:i], rel[i+1:]
	}
	parent := nc.assigned[parentRel] // recorded first: Walk is pre-order
	name := nc.fsys.CleanName(leaf)
	dest := nc.fsys.Join(parent, name)
	for i := 2; ; i++ {
		if _, clash := nc.taken[dest]; !clash {
			break
		}
		dest = nc.fsys.Join(parent, disambiguate(name, i))
	}
	nc.taken[dest] = struct{}{}
	nc.assigned[rel] = dest
	return dest
}

// disambiguate inserts " (n)" before a name's extension: "ep (2).mkv", "dir (2)".
func disambiguate(name string, n int) string {
	ext := path.Ext(name)
	return fmt.Sprintf("%s (%d)%s", name[:len(name)-len(ext)], n, ext)
}

// copyFile streams a single regular file, accumulating bytes into c.
//
// Throughput note: pkg/sftp only parallelises its SSH requests through
// sftp.File.WriteTo (downloads) and sftp.File.ReadFrom (uploads), which io.Copy
// selects by dynamic type. Wrapping the SFTP file in a counter would hide those
// methods and collapse the transfer to one blocking request at a time. So the
// counter goes on the *other* side: downloads count the writer (letting WriteTo
// drive concurrent reads); otherwise we count the reader (letting an SFTP
// destination's ReadFrom drive concurrent writes). *os.File implements both
// WriteTo and ReadFrom, so the SFTP file is identified by type, not interface.
func copyFile(c *counter, src fs.FS, srcPath string, dst fs.FS, dstPath string) error {
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
		copyErr = errFromCopy(io.Copy(&countingWriter{w: w, c: c}, r))
	} else {
		copyErr = errFromCopy(io.Copy(w, &countingReader{r: r, c: c}))
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

func errFromCopy(_ int64, err error) error { return err }

// counter accumulates bytes transferred across one Copy (one file, or every
// file in a tree) and reports the running total over progress with non-blocking
// sends. It also carries the cancellation context for the counting wrappers.
type counter struct {
	ctx      context.Context
	progress chan<- CopyProgressMsg
	written  int64
	total    int64
}

func (c *counter) add(n int) {
	c.written += int64(n)
	c.report()
}

func (c *counter) report() {
	if c.progress == nil {
		return
	}
	select {
	case c.progress <- CopyProgressMsg{Written: c.written, Total: c.total}:
	default: // consumer is behind; drop this sample, the total only grows
	}
}

// countingReader counts bytes read and makes the copy ctx-cancellable.
type countingReader struct {
	r io.Reader
	c *counter
}

func (cr *countingReader) Read(p []byte) (int, error) {
	if err := cr.c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.c.add(n)
	}
	return n, err
}

// countingWriter counts bytes written (used on the download path so the SFTP
// source's concurrent WriteTo is preserved) and makes the copy ctx-cancellable.
type countingWriter struct {
	w io.Writer
	c *counter
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	if err := cw.c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := cw.w.Write(p)
	if n > 0 {
		cw.c.add(n)
	}
	return n, err
}
