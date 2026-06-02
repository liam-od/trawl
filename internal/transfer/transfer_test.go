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

func TestCopyLocalToLocal(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	dstPath := filepath.Join(dir, "dst.bin")

	// A few read buffers' worth so progress fires more than once.
	want := make([]byte, 256*1024)
	if _, err := rand.Read(want); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if err := os.WriteFile(srcPath, want, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	progress := make(chan int64, 1024)
	collected := make(chan []int64, 1)
	go func() {
		var samples []int64
		for v := range progress {
			samples = append(samples, v)
		}
		collected <- samples
	}()

	local := fs.NewLocal()
	if err := transfer.Copy(context.Background(), local, srcPath, local, dstPath, progress); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	close(progress)
	samples := <-collected

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("destination bytes differ from source (%d vs %d bytes)", len(got), len(want))
	}

	if len(samples) == 0 {
		t.Fatal("progress channel received no samples")
	}
	prev := int64(0)
	for i, v := range samples {
		if v < prev {
			t.Fatalf("progress not monotonic at %d: %d after %d", i, v, prev)
		}
		prev = v
	}
	if last := samples[len(samples)-1]; last != int64(len(want)) {
		t.Errorf("final progress = %d, want %d", last, len(want))
	}
}

func TestCopyRoundTripSFTP(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "a.bin")
	remotePath := filepath.Join(dir, "remote.bin")
	backPath := filepath.Join(dir, "b.bin")

	want := make([]byte, 128*1024)
	if _, err := rand.Read(want); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if err := os.WriteFile(srcPath, want, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	local := fs.NewLocal()
	remote := newTestSFTP(t)
	ctx := context.Background()

	// local -> remote
	if err := transfer.Copy(ctx, local, srcPath, remote, remotePath, nil); err != nil {
		t.Fatalf("local->remote: %v", err)
	}
	// remote -> local
	if err := transfer.Copy(ctx, remote, remotePath, local, backPath, nil); err != nil {
		t.Fatalf("remote->local: %v", err)
	}

	got, err := os.ReadFile(backPath)
	if err != nil {
		t.Fatalf("read round-tripped file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("round-trip bytes differ from original (%d vs %d bytes)", len(got), len(want))
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
