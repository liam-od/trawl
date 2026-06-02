package fs

import (
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
)

// localFS is an FS backed by the local operating-system filesystem.
type localFS struct{}

// NewLocal returns an FS backed by the local operating-system filesystem.
func NewLocal() FS { return localFS{} }

func (localFS) ReadDir(path string) ([]Entry, error) {
	infos, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", path, err)
	}
	entries := make([]Entry, 0, len(infos))
	for _, de := range infos {
		fi, err := de.Info()
		if err != nil {
			// The entry vanished between ReadDir and Info; skip it rather
			// than failing the whole listing.
			continue
		}
		entries = append(entries, entryFromInfo(fi))
	}
	sortEntries(entries)
	return entries, nil
}

func (localFS) Open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}

func (localFS) Create(path string) (io.WriteCloser, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	return f, nil
}

func (localFS) Stat(path string) (Entry, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Entry{}, fmt.Errorf("stat %s: %w", path, err)
	}
	return entryFromInfo(fi), nil
}

func (localFS) MkdirAll(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	return nil
}

func (localFS) Walk(root string, fn WalkFunc) error {
	return filepath.WalkDir(root, func(p string, d iofs.DirEntry, err error) error {
		rel := relSlash(root, p)
		if err != nil {
			return fn(rel, Entry{}, err)
		}
		info, ierr := d.Info()
		if ierr != nil {
			return fn(rel, Entry{}, ierr)
		}
		return fn(rel, entryFromInfo(info), nil)
	})
}

func (localFS) Join(elem ...string) string { return filepath.Join(elem...) }

// relSlash returns p relative to root in forward-slash form ("" for the root).
func relSlash(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}
