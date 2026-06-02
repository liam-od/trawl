package fs

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSortEntries(t *testing.T) {
	in := []Entry{
		{Name: "b.txt"},
		{Name: "alpha", IsDir: true},
		{Name: "a.txt"},
		{Name: "zeta", IsDir: true},
	}
	sortEntries(in)

	want := []string{"alpha", "zeta", "a.txt", "b.txt"}
	for i, w := range want {
		if in[i].Name != w {
			t.Errorf("position %d: got %q, want %q", i, in[i].Name, w)
		}
	}
}

// fsMatrix exercises an FS implementation against a freshly created fixture
// tree rooted at root (a real OS directory both LocalFS and the in-process
// SFTP server can see). Both implementations run the same matrix.
func fsMatrix(t *testing.T, fsys FS, root string) {
	t.Helper()

	// Empty directory.
	empty := filepath.Join(root, "empty")
	mustMkdir(t, empty)
	if got, err := fsys.ReadDir(fsys.Join(root, "empty")); err != nil {
		t.Fatalf("ReadDir(empty): %v", err)
	} else if len(got) != 0 {
		t.Errorf("ReadDir(empty): got %d entries, want 0", len(got))
	}

	// Populated directory with a known sort order: dirs first (alpha, zeta),
	// then files (a.txt, b.txt).
	pop := filepath.Join(root, "pop")
	mustMkdir(t, pop)
	mustMkdir(t, filepath.Join(pop, "zeta"))
	mustMkdir(t, filepath.Join(pop, "alpha"))
	mustWrite(t, filepath.Join(pop, "b.txt"), "bbb")
	mustWrite(t, filepath.Join(pop, "a.txt"), "a")

	got, err := fsys.ReadDir(fsys.Join(root, "pop"))
	if err != nil {
		t.Fatalf("ReadDir(pop): %v", err)
	}
	wantNames := []string{"alpha", "zeta", "a.txt", "b.txt"}
	if len(got) != len(wantNames) {
		t.Fatalf("ReadDir(pop): got %d entries, want %d", len(got), len(wantNames))
	}
	for i, w := range wantNames {
		if got[i].Name != w {
			t.Errorf("ReadDir(pop)[%d]: got %q, want %q", i, got[i].Name, w)
		}
	}

	// Missing path is an error.
	if _, err := fsys.ReadDir(fsys.Join(root, "does-not-exist")); err == nil {
		t.Error("ReadDir(missing): want error, got nil")
	}

	// Stat distinguishes file from dir and reports size.
	fileEnt, err := fsys.Stat(fsys.Join(pop, "b.txt"))
	if err != nil {
		t.Fatalf("Stat(file): %v", err)
	}
	if fileEnt.IsDir {
		t.Error("Stat(file): IsDir = true, want false")
	}
	if fileEnt.Size != 3 {
		t.Errorf("Stat(file): Size = %d, want 3", fileEnt.Size)
	}
	dirEnt, err := fsys.Stat(fsys.Join(pop, "alpha"))
	if err != nil {
		t.Fatalf("Stat(dir): %v", err)
	}
	if !dirEnt.IsDir {
		t.Error("Stat(dir): IsDir = false, want true")
	}

	// Stat of a missing path is an error.
	if _, err := fsys.Stat(fsys.Join(root, "does-not-exist")); err == nil {
		t.Error("Stat(missing): want error, got nil")
	}

	// Create then Open round-trips the bytes.
	const payload = "hello fs abstraction"
	dst := fsys.Join(root, "created.txt")
	w, err := fsys.Create(dst)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := io.WriteString(w, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	r, err := fsys.Open(dst)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	gotBytes, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(gotBytes, []byte(payload)) {
		t.Errorf("round trip: got %q, want %q", gotBytes, payload)
	}

	// Open of a missing path is an error.
	if _, err := fsys.Open(fsys.Join(root, "does-not-exist")); err == nil {
		t.Error("Open(missing): want error, got nil")
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
