// Package fs provides a minimal filesystem abstraction shared by the local disk
// and a remote SFTP server, so that UI and transfer code need not care which
// side a given path lives on.
package fs

import (
	"io"
	iofs "io/fs"
	"sort"
)

// Entry describes a single directory entry.
type Entry struct {
	Name  string
	Size  int64
	IsDir bool
	Mode  iofs.FileMode
}

// SkipDir, returned from a WalkFunc for a directory entry, tells Walk to skip
// that directory's subtree without descending into it; the walk then continues
// with the next sibling. It mirrors (and equals) io/fs.SkipDir, so both walker
// backends recognise it. Returned for a non-directory it skips the rest of the
// containing directory, so to skip a single file return nil instead.
var SkipDir = iofs.SkipDir

// WalkFunc is called for each entry under a walk root, including the root
// itself. rel is the entry's path relative to the root, always forward-slash
// separated regardless of platform ("" for the root). A non-nil err reports a
// failure reading that path; returning a non-nil error from the callback stops
// the walk and Walk returns it, except SkipDir, which prunes rather than stops.
type WalkFunc func(rel string, entry Entry, err error) error

// FS is the common interface over the local and remote filesystems. Paths are
// absolute and use the convention native to the implementation: OS-native for
// LocalFS, POSIX (forward-slash) for SFTPFS. Build child paths with Join rather
// than concatenating strings, so the right separator is always used.
type FS interface {
	ReadDir(path string) ([]Entry, error)
	Open(path string) (io.ReadCloser, error)
	Create(path string) (io.WriteCloser, error)
	Stat(path string) (Entry, error)
	MkdirAll(path string) error
	Walk(root string, fn WalkFunc) error
	Join(elem ...string) string

	// CleanName rewrites a single path component into a name this filesystem
	// accepts, returning it unchanged when it is already legal. It lets a name
	// that is legal on one side of a transfer (e.g. one containing ':' on a
	// POSIX server) land on a stricter destination (e.g. Windows local disk).
	// The result is never empty for a non-empty input.
	CleanName(name string) string
}

func entryFromInfo(fi iofs.FileInfo) Entry {
	return Entry{
		Name:  fi.Name(),
		Size:  fi.Size(),
		IsDir: fi.IsDir(),
		Mode:  fi.Mode(),
	}
}

// sortEntries orders entries directories-first, then alphabetically by name —
// the convention carried forward from the prototype's old/local.go.
func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
}
