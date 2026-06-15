package transfer_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"

	"github.com/liam-od/trawl/internal/fs"
	"github.com/liam-od/trawl/internal/transfer"
)

// collectProgress drains a progress channel into a slice. The returned finish
// closes the channel and returns everything received.
func collectProgress() (chan transfer.CopyProgressMsg, func() []transfer.CopyProgressMsg) {
	progress := make(chan transfer.CopyProgressMsg, 1024)
	done := make(chan []transfer.CopyProgressMsg, 1)
	go func() {
		var msgs []transfer.CopyProgressMsg
		for p := range progress {
			msgs = append(msgs, p)
		}
		done <- msgs
	}()
	return progress, func() []transfer.CopyProgressMsg {
		close(progress)
		return <-done
	}
}

// assertProgress checks the samples are monotonic in Written and end at total.
func assertProgress(t *testing.T, msgs []transfer.CopyProgressMsg, total int64) {
	t.Helper()
	if len(msgs) == 0 {
		t.Fatal("no progress samples received")
	}
	prev := int64(0)
	for i, m := range msgs {
		if m.Written < prev {
			t.Fatalf("written not monotonic at %d: %d after %d", i, m.Written, prev)
		}
		prev = m.Written
	}
	if last := msgs[len(msgs)-1].Written; last != total {
		t.Errorf("final written = %d, want %d", last, total)
	}
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}

func TestCopyLocalToLocal(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	dstPath := filepath.Join(dir, "dst.bin")
	want := randBytes(t, 256*1024)
	if err := os.WriteFile(srcPath, want, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	progress, finish := collectProgress()
	local := fs.NewLocal()
	if err := transfer.Copy(context.Background(), local, srcPath, local, dstPath, nil, progress); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	msgs := finish()

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("destination bytes differ (%d vs %d)", len(got), len(want))
	}
	assertProgress(t, msgs, int64(len(want)))
}

func TestCopyDownloadReportsProgress(t *testing.T) {
	// SFTP source (download) path — counts on the writer so sftp.File.WriteTo's
	// concurrent reads stay in play.
	dir := t.TempDir()
	remotePath := filepath.Join(dir, "remote.bin")
	localPath := filepath.Join(dir, "local.bin")
	want := randBytes(t, 256*1024)
	if err := os.WriteFile(remotePath, want, 0o644); err != nil {
		t.Fatalf("write remote: %v", err)
	}

	progress, finish := collectProgress()
	if err := transfer.Copy(context.Background(), newTestSFTP(t), remotePath, fs.NewLocal(), localPath, nil, progress); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	msgs := finish()

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("downloaded bytes differ (%d vs %d)", len(got), len(want))
	}
	assertProgress(t, msgs, int64(len(want)))
}

func TestCopyUploadReportsProgress(t *testing.T) {
	// SFTP destination (upload) path — the local source is wrapped in a
	// countingReader, hiding os.File.WriteTo so sftp.File.ReadFrom's concurrent
	// writes run.
	dir := t.TempDir()
	localPath := filepath.Join(dir, "local.bin")
	remotePath := filepath.Join(dir, "remote.bin")
	want := randBytes(t, 256*1024)
	if err := os.WriteFile(localPath, want, 0o644); err != nil {
		t.Fatalf("write local: %v", err)
	}

	progress, finish := collectProgress()
	if err := transfer.Copy(context.Background(), fs.NewLocal(), localPath, newTestSFTP(t), remotePath, nil, progress); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	msgs := finish()

	got, err := os.ReadFile(remotePath)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("uploaded bytes differ (%d vs %d)", len(got), len(want))
	}
	assertProgress(t, msgs, int64(len(want)))
}

func TestCopyRoundTripSFTP(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "a.bin")
	remotePath := filepath.Join(dir, "remote.bin")
	backPath := filepath.Join(dir, "b.bin")
	want := randBytes(t, 128*1024)
	if err := os.WriteFile(srcPath, want, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	local := fs.NewLocal()
	remote := newTestSFTP(t)
	ctx := context.Background()
	if err := transfer.Copy(ctx, local, srcPath, remote, remotePath, nil, nil); err != nil {
		t.Fatalf("local->remote: %v", err)
	}
	if err := transfer.Copy(ctx, remote, remotePath, local, backPath, nil, nil); err != nil {
		t.Fatalf("remote->local: %v", err)
	}

	got, err := os.ReadFile(backPath)
	if err != nil {
		t.Fatalf("read round-tripped file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("round-trip bytes differ (%d vs %d)", len(got), len(want))
	}
}

func TestCopyDirectoryTree(t *testing.T) {
	dir := t.TempDir()
	srcRoot := filepath.Join(dir, "src")
	dstRoot := filepath.Join(dir, "dst") // created on the (sftp) destination

	files := map[string][]byte{
		"a.txt":       []byte("hello"),
		"sub/b.txt":   randBytes(t, 64*1024),
		"sub/c/d.bin": randBytes(t, 32*1024),
	}
	var total int64
	for rel, content := range files {
		p := filepath.Join(srcRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		total += int64(len(content))
	}

	progress, finish := collectProgress()
	if err := transfer.Copy(context.Background(), fs.NewLocal(), srcRoot, newTestSFTP(t), dstRoot, nil, progress); err != nil {
		t.Fatalf("Copy dir: %v", err)
	}
	msgs := finish()

	// Every file landed under dstRoot with identical bytes (sftp serves real fs).
	for rel, content := range files {
		got, err := os.ReadFile(filepath.Join(dstRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read copied %s: %v", rel, err)
		}
		if !bytes.Equal(got, content) {
			t.Errorf("%s differs after tree copy", rel)
		}
	}
	assertProgress(t, msgs, total)
	if last := msgs[len(msgs)-1].Total; last != total {
		t.Errorf("final reported Total = %d, want %d", last, total)
	}
}

// TestCopyExcludesPrunedSubtree copies a tree from an SFTP source (exercising
// the SFTP walker's SkipDir path) and asserts that excluded directories are
// never created on the destination and their bytes never counted.
func TestCopyExcludesPrunedSubtree(t *testing.T) {
	dir := t.TempDir()
	srcRoot := filepath.Join(dir, "src")
	dstRoot := filepath.Join(dir, "dst")

	kept := map[string][]byte{
		"a.txt":     []byte("hello"),
		"sub/b.txt": randBytes(t, 16*1024),
	}
	pruned := map[string][]byte{
		".venv/lib/x.so":      randBytes(t, 8*1024),
		"sub/__pycache__/y.c": []byte("cached"),
	}
	var keptTotal int64
	for set, files := range map[bool]map[string][]byte{true: kept, false: pruned} {
		for rel, content := range files {
			p := filepath.Join(srcRoot, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(p, content, 0o644); err != nil {
				t.Fatalf("write %s: %v", rel, err)
			}
			if set {
				keptTotal += int64(len(content))
			}
		}
	}

	exclude := []string{".venv", "__pycache__"}
	progress, finish := collectProgress()
	if err := transfer.Copy(context.Background(), newTestSFTP(t), srcRoot, fs.NewLocal(), dstRoot, exclude, progress); err != nil {
		t.Fatalf("Copy dir: %v", err)
	}
	msgs := finish()

	for rel := range kept {
		if _, err := os.Stat(filepath.Join(dstRoot, filepath.FromSlash(rel))); err != nil {
			t.Errorf("kept file %s missing at destination: %v", rel, err)
		}
	}
	for rel := range pruned {
		if _, err := os.Stat(filepath.Join(dstRoot, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("excluded file %s should not exist at destination (err=%v)", rel, err)
		}
	}
	// The excluded directories themselves must never be created.
	for _, d := range []string{".venv", "sub/__pycache__"} {
		if _, err := os.Stat(filepath.Join(dstRoot, filepath.FromSlash(d))); !os.IsNotExist(err) {
			t.Errorf("excluded dir %s should not exist at destination (err=%v)", d, err)
		}
	}
	assertProgress(t, msgs, keptTotal)
	if last := msgs[len(msgs)-1].Total; last != keptTotal {
		t.Errorf("final reported Total = %d, want %d (pruned bytes leaked into total)", last, keptTotal)
	}
}

// TestCopySkipsSymlinks copies a tree containing a symlink to a directory (the
// shape that produced SSH_FX_FAILURE: a venv's lib64 -> lib) and a symlink to a
// file. Both must be skipped, not dereferenced and streamed, with no error.
func TestCopySkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	srcRoot := filepath.Join(dir, "src")
	dstRoot := filepath.Join(dir, "dst")

	if err := os.MkdirAll(filepath.Join(srcRoot, "lib"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	want := []byte("real")
	if err := os.WriteFile(filepath.Join(srcRoot, "lib", "f.txt"), want, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcRoot, "real.txt"), want, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink("lib", filepath.Join(srcRoot, "lib64")); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}
	if err := os.Symlink("real.txt", filepath.Join(srcRoot, "link.txt")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}

	if err := transfer.Copy(context.Background(), newTestSFTP(t), srcRoot, fs.NewLocal(), dstRoot, nil, nil); err != nil {
		t.Fatalf("Copy dir: %v", err)
	}

	// Real entries copied; symlinks absent (neither followed nor recreated).
	if _, err := os.Stat(filepath.Join(dstRoot, "lib", "f.txt")); err != nil {
		t.Errorf("real file missing at destination: %v", err)
	}
	for _, link := range []string{"lib64", "link.txt"} {
		if _, err := os.Lstat(filepath.Join(dstRoot, link)); !os.IsNotExist(err) {
			t.Errorf("symlink %s should not exist at destination (err=%v)", link, err)
		}
	}
}

// newTestSFTP stands up an in-process pkg/sftp server over two io.Pipes serving
// the real OS filesystem, and returns an fs.FS wired to a client on the far end.
func newTestSFTP(t *testing.T) fs.FS {
	t.Helper()

	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	serverConn := struct {
		io.Reader
		io.WriteCloser
	}{serverR, serverW}

	server, err := sftp.NewServer(serverConn)
	if err != nil {
		t.Fatalf("sftp server: %v", err)
	}
	go func() { _ = server.Serve() }()

	client, err := sftp.NewClientPipe(clientR, clientW)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	t.Cleanup(func() {
		// Close the server first so the client's read pipe EOFs and its recv
		// goroutine exits, letting client.Close() return.
		_ = server.Close()
		_ = client.Close()
	})
	return fs.NewSFTP(client)
}
