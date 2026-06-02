package sshx

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Session bundles the single SSH connection and its single SFTP client. Close tears them down
// in order: SFTP first, then SSH, then any auxiliary closers (e.g. the agent socket).
type Session struct {
	SSH     *ssh.Client
	SFTP    *sftp.Client
	closers []io.Closer
}

// Connect dials the target via TCP, performs the SSH handshake using the auth methods
// assembled from cfg, and opens a single SFTP client over the resulting transport. The
// context governs the TCP dial; the SSH handshake itself is not cancellable.
func Connect(ctx context.Context, t Target, cfg Config) (*Session, error) {
	cfg.applyDefaults()

	methods, closers, err := buildAuthMethods(cfg)
	if err != nil {
		return nil, err
	}

	addr := net.JoinHostPort(t.Host, resolvePort(t, cfg))

	var d net.Dialer
	rawConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		closeAll(closers)
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	clientCfg := &ssh.ClientConfig{
		User: t.User,
		Auth: methods,
		// TODO(M6): replace with knownhosts.New from golang.org/x/crypto/ssh/knownhosts.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	cConn, chans, reqs, err := ssh.NewClientConn(rawConn, addr, clientCfg)
	if err != nil {
		_ = rawConn.Close()
		closeAll(closers)
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	sshClient := ssh.NewClient(cConn, chans, reqs)

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		closeAll(closers)
		return nil, fmt.Errorf("sftp client: %w", err)
	}

	return &Session{SSH: sshClient, SFTP: sftpClient, closers: closers}, nil
}

// Close tears down SFTP first, then SSH, then any auxiliary closers. Returns the first
// non-nil error encountered.
func (s *Session) Close() error {
	var first error
	if err := s.SFTP.Close(); err != nil {
		first = err
	}
	if err := s.SSH.Close(); err != nil && first == nil {
		first = err
	}
	closeAll(s.closers)
	return first
}

func resolvePort(t Target, cfg Config) string {
	if cfg.Port > 0 {
		return strconv.Itoa(cfg.Port)
	}
	if t.Port != "" {
		return t.Port
	}
	return "22"
}
