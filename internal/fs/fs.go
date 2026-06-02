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

// FS is the common interface over the local and remote filesystems. Paths are
// absolute and use the convention native to the implementation: OS-native for
// LocalFS, POSIX (forward-slash) for SFTPFS. Build child paths with Join rather
// than concatenating strings, so the right separator is always used.
type FS interface {
	ReadDir(path string) ([]Entry, error)
	Open(path string) (io.ReadCloser, error)
	Create(path string) (io.WriteCloser, error)
	Stat(path string) (Entry, error)
	Join(elem ...string) string
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
