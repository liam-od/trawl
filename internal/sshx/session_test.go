package sshx

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestConnect_Integration(t *testing.T) {
	hostSigner, _ := newSigner(t)
	clientSigner, clientPriv := newSigner(t)

	srvCfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if string(k.Marshal()) == string(clientSigner.PublicKey().Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unauthorized")
		},
	}
	srvCfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		serveOne(t, ln, srvCfg)
	}()

	der, err := x509.MarshalPKCS8PrivateKey(clientPriv)
	if err != nil {
		t.Fatalf("pkcs8: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	keyPath := filepath.Join(t.TempDir(), "id")
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	target := Target{User: "tester", Host: host, Port: port}
	cfg := Config{
		DisableAgent:   true,
		KeyPath:        keyPath,
		KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts"),
		HostKeyPrompt:  func(string, string) (bool, error) { return true, nil }, // accept on first use
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := Connect(ctx, target, cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if _, err := sess.SFTP.Getwd(); err != nil {
		t.Fatalf("sftp Getwd: %v", err)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-srvDone:
	case <-time.After(3 * time.Second):
		t.Fatalf("server goroutine did not exit after client close")
	}
}

func TestConnect_HostKeyMismatch(t *testing.T) {
	hostSigner, _ := newSigner(t)
	clientSigner, clientPriv := newSigner(t)

	srvCfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if string(k.Marshal()) == string(clientSigner.PublicKey().Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unauthorized")
		},
	}
	srvCfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go serveOne(t, ln, srvCfg)

	der, _ := x509.MarshalPKCS8PrivateKey(clientPriv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	keyPath := filepath.Join(t.TempDir(), "id")
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	addr := net.JoinHostPort(host, port)

	// Pre-seed known_hosts with the WRONG key for this address.
	wrongKey := genHostKey(t)
	khPath := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, wrongKey)
	if err := os.WriteFile(khPath, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cfg := Config{
		DisableAgent:   true,
		KeyPath:        keyPath,
		KnownHostsPath: khPath,
		HostKeyPrompt: func(string, string) (bool, error) {
			t.Error("prompt must not be called on a mismatch")
			return true, nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := Connect(ctx, Target{User: "tester", Host: host, Port: port}, cfg); err == nil {
		t.Fatal("Connect accepted a mismatched host key, want refusal")
	} else if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("error %q does not identify a host key mismatch", err)
	}
}

func newSigner(t *testing.T) (ssh.Signer, ed25519.PrivateKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer, priv
}

// serveOne accepts a single SSH connection, advertises an SFTP subsystem on the first
// "session" channel, and serves until the channel closes.
func serveOne(t *testing.T, ln net.Listener, srvCfg *ssh.ServerConfig) {
	t.Helper()
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	_, chans, reqs, err := ssh.NewServerConn(conn, srvCfg)
	if err != nil {
		t.Logf("server handshake: %v", err)
		return
	}
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "unknown")
			continue
		}
		ch, chanReqs, err := newCh.Accept()
		if err != nil {
			t.Logf("accept channel: %v", err)
			continue
		}
		go handleSession(ch, chanReqs)
	}
}

func handleSession(ch ssh.Channel, in <-chan *ssh.Request) {
	for req := range in {
		ok := req.Type == "subsystem" && len(req.Payload) >= 4 && string(req.Payload[4:]) == "sftp"
		if req.WantReply {
			_ = req.Reply(ok, nil)
		}
		if ok {
			srv, err := sftp.NewServer(ch)
			if err != nil {
				_ = ch.Close()
				return
			}
			_ = srv.Serve()
			_ = ch.Close()
			return
		}
	}
}
