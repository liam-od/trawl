// Package media executes a sort plan: it relocates files and directories from an
// inbox into a sorted library, one move at a time. The plan's intelligence
// (parsing release names, matching the library, choosing destinations) lives in
// the calling skill; this package is the mechanical executor and knows nothing
// about naming conventions.
package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/liam-od/trawl/internal/fs"
	"github.com/liam-od/trawl/internal/job"
	"github.com/liam-od/trawl/internal/transfer"
)

// Result reports the outcome of one move. Src and Dest are echoed back as given
// (relative to the inbox and library roots); Err is nil on success.
type Result struct {
	Src  string
	Dest string
	Err  error
}

// Sort executes each move under the inbox and library roots, which must already
// be expanded to absolute paths. It continues past a failed move so one bad
// release name doesn't strand the rest, returning one Result per move in order.
// A cancelled ctx stops the run; the remaining moves are reported as cancelled.
func Sort(ctx context.Context, inbox, library string, moves []job.Move) []Result {
	results := make([]Result, 0, len(moves))
	for _, m := range moves {
		err := ctx.Err()
		if err == nil {
			src := filepath.Join(inbox, filepath.FromSlash(m.Src))
			dst := filepath.Join(library, filepath.FromSlash(m.Dest))
			err = move(ctx, src, dst)
		}
		results = append(results, Result{Src: m.Src, Dest: m.Dest, Err: err})
	}
	return results
}

// move relocates srcAbs to dstAbs. It creates the destination's parent, refuses
// to overwrite an existing destination, then tries an atomic rename. When source
// and destination sit on different filesystems (the inbox disk vs the library
// mount) rename fails with EXDEV, so it falls back to a copy followed by removing
// the source — a non-atomic move, but the only option across a device boundary.
func move(ctx context.Context, srcAbs, dstAbs string) error {
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}
	switch _, err := os.Lstat(dstAbs); {
	case err == nil:
		return fmt.Errorf("destination already exists: %s", dstAbs)
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("stat destination: %w", err)
	}

	switch err := os.Rename(srcAbs, dstAbs); {
	case err == nil:
		return nil
	case !errors.Is(err, syscall.EXDEV):
		return fmt.Errorf("move %s: %w", srcAbs, err)
	}

	// Cross-device: copy the tree, then drop the source only once the copy
	// succeeded, so a failed copy leaves the inbox untouched.
	local := fs.NewLocal()
	if err := transfer.Copy(ctx, local, srcAbs, local, dstAbs, nil); err != nil {
		return fmt.Errorf("copy %s: %w", srcAbs, err)
	}
	if err := os.RemoveAll(srcAbs); err != nil {
		return fmt.Errorf("remove source after copy: %w", err)
	}
	return nil
}

// Summary counts the successes and failures in results, for a one-line report.
func Summary(results []Result) (moved, failed int) {
	for _, r := range results {
		if r.Err != nil {
			failed++
		} else {
			moved++
		}
	}
	return moved, failed
}
