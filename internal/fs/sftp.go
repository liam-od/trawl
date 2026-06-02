package fs

import (
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/pkg/sftp"
)

// sftpFS is an FS backed by a single established SFTP client. Remote paths are
// POSIX, so it joins with path rather than path/filepath.
type sftpFS struct {
	client *sftp.Client
}

// NewSFTP returns an FS backed by an established SFTP client. The client is
// owned by the caller (the sshx.Session); this FS never closes it.
func NewSFTP(c *sftp.Client) FS { return &sftpFS{client: c} }

func (s *sftpFS) ReadDir(p string) ([]Entry, error) {
	infos, err := s.client.ReadDir(p)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", p, err)
	}
	entries := make([]Entry, 0, len(infos))
	for _, fi := range infos {
		entries = append(entries, entryFromInfo(fi))
	}
	sortEntries(entries)
	return entries, nil
}

func (s *sftpFS) Open(p string) (io.ReadCloser, error) {
	f, err := s.client.Open(p)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", p, err)
	}
	return f, nil
}

func (s *sftpFS) Create(p string) (io.WriteCloser, error) {
	f, err := s.client.Create(p)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", p, err)
	}
	return f, nil
}

func (s *sftpFS) Stat(p string) (Entry, error) {
	fi, err := s.client.Stat(p)
	if err != nil {
		return Entry{}, fmt.Errorf("stat %s: %w", p, err)
	}
	return entryFromInfo(fi), nil
}

func (s *sftpFS) MkdirAll(path string) error {
	if err := s.client.MkdirAll(path); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	return nil
}

func (s *sftpFS) Walk(root string, fn WalkFunc) error {
	w := s.client.Walk(root)
	for w.Step() {
		rel := strings.TrimPrefix(strings.TrimPrefix(w.Path(), root), "/")
		if err := w.Err(); err != nil {
			if ferr := fn(rel, Entry{}, err); ferr != nil {
				return ferr
			}
			continue
		}
		if ferr := fn(rel, entryFromInfo(w.Stat()), nil); ferr != nil {
			return ferr
		}
	}
	return nil
}

func (s *sftpFS) Join(elem ...string) string { return path.Join(elem...) }
