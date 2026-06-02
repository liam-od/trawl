package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh/agent"
)

type nopCloser struct{ closed bool }

func (n *nopCloser) Close() error { n.closed = true; return nil }

func makePrivateKeyPEM(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("pkcs8: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestBuildAuthMethods_AgentAndKey(t *testing.T) {
	keyPEM := makePrivateKeyPEM(t)
	closer := &nopCloser{}
	var dialed, read int
	cfg := Config{
		KeyPath: "/fake/key",
		dialAgent: func() (agent.Agent, io.Closer, error) {
			dialed++
			return agent.NewKeyring(), closer, nil
		},
		readKey: func(string) ([]byte, error) {
			read++
			return keyPEM, nil
		},
	}
	methods, closers, err := buildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if dialed != 1 || read != 1 {
		t.Fatalf("dialed=%d read=%d, want 1/1", dialed, read)
	}
	if len(methods) != 2 {
		t.Fatalf("want 2 methods, got %d", len(methods))
	}
	if len(closers) != 1 || closers[0] != closer {
		t.Fatalf("expected agent closer to be returned")
	}
}

func TestBuildAuthMethods_AgentFailsKeyOnly(t *testing.T) {
	keyPEM := makePrivateKeyPEM(t)
	cfg := Config{
		KeyPath: "/fake/key",
		dialAgent: func() (agent.Agent, io.Closer, error) {
			return nil, nil, errors.New("no socket")
		},
		readKey: func(string) ([]byte, error) { return keyPEM, nil },
	}
	methods, closers, err := buildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("want 1 method, got %d", len(methods))
	}
	if len(closers) != 0 {
		t.Fatalf("want 0 closers, got %d", len(closers))
	}
}

func TestBuildAuthMethods_KeyReadErrorIsLoud(t *testing.T) {
	closer := &nopCloser{}
	cfg := Config{
		KeyPath: "/fake/key",
		dialAgent: func() (agent.Agent, io.Closer, error) {
			return agent.NewKeyring(), closer, nil
		},
		readKey: func(string) ([]byte, error) {
			return nil, errors.New("permission denied")
		},
	}
	_, _, err := buildAuthMethods(cfg)
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "/fake/key") {
		t.Fatalf("error should include path: %v", err)
	}
	if !closer.closed {
		t.Fatalf("agent closer should be released on auth error")
	}
}

func TestBuildAuthMethods_KeyParseErrorIsLoud(t *testing.T) {
	cfg := Config{
		DisableAgent: true,
		KeyPath:      "/fake/key",
		readKey:      func(string) ([]byte, error) { return []byte("not a key"), nil },
	}
	_, _, err := buildAuthMethods(cfg)
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "/fake/key") {
		t.Fatalf("error should include path: %v", err)
	}
}

func TestBuildAuthMethods_NothingConfigured(t *testing.T) {
	cfg := Config{
		DisableAgent: true,
		dialAgent: func() (agent.Agent, io.Closer, error) {
			t.Fatal("dialAgent should not be called when DisableAgent is set")
			return nil, nil, nil
		},
		readKey: func(string) ([]byte, error) {
			t.Fatal("readKey should not be called with empty KeyPath")
			return nil, nil
		},
	}
	_, _, err := buildAuthMethods(cfg)
	if err == nil {
		t.Fatalf("want error, got nil")
	}
}

func TestBuildAuthMethods_PasswordAppendedLast(t *testing.T) {
	keyPEM := makePrivateKeyPEM(t)
	closer := &nopCloser{}
	cfg := Config{
		KeyPath:        "/fake/key",
		PasswordPrompt: func() (string, error) { return "hunter2", nil },
		dialAgent: func() (agent.Agent, io.Closer, error) {
			return agent.NewKeyring(), closer, nil
		},
		readKey: func(string) ([]byte, error) { return keyPEM, nil },
	}
	methods, _, err := buildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(methods) != 3 {
		t.Fatalf("want 3 methods (agent, key, password), got %d", len(methods))
	}
}

func TestBuildAuthMethods_PasswordOnly(t *testing.T) {
	cfg := Config{
		DisableAgent:   true,
		PasswordPrompt: func() (string, error) { return "hunter2", nil },
	}
	methods, _, err := buildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("want 1 method, got %d", len(methods))
	}
}
