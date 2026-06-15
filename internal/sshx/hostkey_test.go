package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func genHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer.PublicKey()
}

var testAddr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 22}

// writeKnownHosts writes a single plaintext entry mapping host to key.
func writeKnownHosts(t *testing.T, dir, host string, key ssh.PublicKey) string {
	t.Helper()
	path := filepath.Join(dir, "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(host)}, key)
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

func TestHostKeyCallback_KnownGood(t *testing.T) {
	host := "example.com:22"
	key := genHostKey(t)
	path := writeKnownHosts(t, t.TempDir(), host, key)

	cb, err := loadHostKeyCallback(path, nil)
	if err != nil {
		t.Fatalf("loadHostKeyCallback: %v", err)
	}
	if err := cb(host, testAddr, key); err != nil {
		t.Errorf("known good host rejected: %v", err)
	}
}

func TestHostKeyCallback_Mismatch(t *testing.T) {
	host := "example.com:22"
	good := genHostKey(t)
	evil := genHostKey(t)
	path := writeKnownHosts(t, t.TempDir(), host, good)

	cb, err := loadHostKeyCallback(path, func(string, string) (bool, error) {
		t.Error("prompt must not be called on a key mismatch")
		return true, nil
	})
	if err != nil {
		t.Fatalf("loadHostKeyCallback: %v", err)
	}
	err = cb(host, testAddr, evil)
	if err == nil {
		t.Fatal("changed host key was accepted, want refusal")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("error %q does not identify a mismatch", err)
	}
}

func TestHostKeyCallback_UnknownPromptRefused(t *testing.T) {
	good := genHostKey(t)
	path := writeKnownHosts(t, t.TempDir(), "known.example:22", good)

	called := false
	cb, err := loadHostKeyCallback(path, func(host, fp string) (bool, error) {
		called = true
		if !strings.HasPrefix(fp, "SHA256:") {
			t.Errorf("fingerprint %q not SHA256", fp)
		}
		return false, nil // user declines
	})
	if err != nil {
		t.Fatalf("loadHostKeyCallback: %v", err)
	}
	if err := cb("new.example:22", testAddr, genHostKey(t)); err == nil {
		t.Error("declined unknown host was accepted")
	}
	if !called {
		t.Error("prompt was not called for an unknown host")
	}
}

func TestHostKeyCallback_UnknownPromptAcceptedPersists(t *testing.T) {
	dir := t.TempDir()
	path := writeKnownHosts(t, dir, "known.example:22", genHostKey(t))
	newKey := genHostKey(t)

	cb, err := loadHostKeyCallback(path, func(string, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("loadHostKeyCallback: %v", err)
	}
	if err := cb("new.example:22", testAddr, newKey); err != nil {
		t.Fatalf("accepted unknown host still errored: %v", err)
	}

	// A fresh callback (re-reading the file) must now treat the host as known,
	// even with no prompt available — proving the key was persisted.
	cb2, err := loadHostKeyCallback(path, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := cb2("new.example:22", testAddr, newKey); err != nil {
		t.Errorf("persisted host key not recognized on reload: %v", err)
	}
}

func TestHostKeyCallback_MissingFileIsCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "known_hosts")
	cb, err := loadHostKeyCallback(path, func(string, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("loadHostKeyCallback on missing file: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("known_hosts not created: %v", err)
	}
	// First contact on an empty file is an unknown host → prompt path.
	if err := cb("first.example:22", testAddr, genHostKey(t)); err != nil {
		t.Errorf("first-use accept errored: %v", err)
	}
}

func TestLoadHostKeyCallback_EmptyPath(t *testing.T) {
	if _, err := loadHostKeyCallback("", nil); err == nil {
		t.Error("empty known_hosts path should error")
	}
}

func TestHostKeyAlgorithms_DerivesFromKnownHosts(t *testing.T) {
	host := "example.com:22"
	path := writeKnownHosts(t, t.TempDir(), host, genHostKey(t)) // ed25519

	got := hostKeyAlgorithms(path, host)
	if want := []string{ssh.KeyAlgoED25519}; !slices.Equal(got, want) {
		t.Errorf("hostKeyAlgorithms = %v, want %v", got, want)
	}
}

func TestHostKeyAlgorithms_RSAExpandsToSHA2(t *testing.T) {
	host := "rsa.example.com:22"
	path := writeKnownHosts(t, t.TempDir(), host, genRSAHostKey(t))

	got := hostKeyAlgorithms(path, host)
	want := []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	if !slices.Equal(got, want) {
		t.Errorf("hostKeyAlgorithms = %v, want %v", got, want)
	}
}

func TestHostKeyAlgorithms_UnknownHostIsNil(t *testing.T) {
	path := writeKnownHosts(t, t.TempDir(), "known.example:22", genHostKey(t))

	if got := hostKeyAlgorithms(path, "stranger.example:22"); got != nil {
		t.Errorf("unknown host should yield nil algorithms, got %v", got)
	}
}

func genRSAHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer.PublicKey()
}
