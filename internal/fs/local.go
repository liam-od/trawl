package fs

import (
	"fmt"
	"io"
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

func (localFS) Join(elem ...string) string { return filepath.Join(elem...) }
