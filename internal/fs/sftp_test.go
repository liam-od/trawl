package fs

import (
	"io"
	"testing"

	"github.com/pkg/sftp"
)

func TestSFTPFS(t *testing.T) {
	fsMatrix(t, newTestSFTP(t), t.TempDir())
}

func TestSFTPFSWalkMkdir(t *testing.T) {
	fsWalkMkdir(t, newTestSFTP(t), t.TempDir())
}

func TestSFTPFS_Join(t *testing.T) {
	got := NewSFTP(nil).Join("/a", "b", "c")
	if want := "/a/b/c"; got != want {
		t.Errorf("Join: got %q, want %q", got, want)
	}
}

// newTestSFTP stands up an in-process pkg/sftp server over two io.Pipes and
// returns an SFTPFS wired to a client on the other end. The server serves the
// real OS filesystem, so the FS sees the same absolute paths the test creates.
func newTestSFTP(t *testing.T) FS {
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
		// Close the server first: that EOFs the client's read pipe so the
		// client's recv goroutine exits, letting client.Close() return
		// instead of blocking forever on its internal WaitGroup.
		_ = server.Close()
		_ = client.Close()
	})
	return NewSFTP(client)
}
