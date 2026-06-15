package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// PromptFunc is consulted when connecting to a host whose key is not yet in
// known_hosts (trust on first use). It receives the host and the key's SHA256
// fingerprint and returns true to accept and record the key. It is injectable
// so tests can drive the TOFU path without a terminal.
type PromptFunc func(host, fingerprint string) (bool, error)

// loadHostKeyCallback builds an ssh.HostKeyCallback that verifies host keys
// against the known_hosts file at path. A known key passes silently; a changed
// key is refused loudly; an unknown key is offered to prompt (TOFU) and, if
// accepted, appended to the file. A nil prompt refuses unknown hosts.
func loadHostKeyCallback(path string, prompt PromptFunc) (ssh.HostKeyCallback, error) {
	if path == "" {
		return nil, errors.New("no known_hosts path configured")
	}
	if err := ensureFile(path); err != nil {
		return nil, err
	}
	base, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts %s: %w", path, err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := base(hostname, remote, key)
		if err == nil {
			return nil // key already trusted
		}

		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err
		}
		if len(keyErr.Want) > 0 {
			return mismatchError(hostname, key, keyErr.Want)
		}

		// Unknown host: trust on first use, only with an interactive prompt.
		fingerprint := ssh.FingerprintSHA256(key)
		if prompt == nil {
			return fmt.Errorf("host key for %s is unknown (fingerprint %s) and cannot be confirmed", hostname, fingerprint)
		}
		ok, err := prompt(hostname, fingerprint)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("host key for %s was not accepted", hostname)
		}
		if err := appendKnownHost(path, hostname, key); err != nil {
			return fmt.Errorf("record host key: %w", err)
		}
		return nil
	}, nil
}

// hostKeyAlgorithms returns the host-key algorithms to advertise for addr,
// derived from the key types already recorded for it in the known_hosts file at
// path. Pinning these makes the handshake negotiate a host key we already trust,
// rather than letting Go pick its own preferred type (e.g. ECDSA): a server
// commonly offers several host keys, and known_hosts may record only one of
// them, so Go's default choice can be a key the file has never seen — a
// spurious "host key mismatch" even though the host is genuine.
//
// It returns nil for a host with no recorded keys (or on any read error), so a
// first-time connection falls back to Go's defaults and the normal TOFU path.
// Each user's own known_hosts drives the result, so no key type is locked out.
func hostKeyAlgorithms(path, addr string) []string {
	base, err := knownhosts.New(path)
	if err != nil {
		return nil
	}
	probe, err := probeHostKey()
	if err != nil {
		return nil
	}

	// A random probe key never matches a recorded one, so knownhosts reports a
	// KeyError whose Want lists every host key on file for addr.
	var keyErr *knownhosts.KeyError
	if !errors.As(base(addr, zeroAddr{}, probe), &keyErr) || len(keyErr.Want) == 0 {
		return nil
	}

	var algos []string
	seen := make(map[string]bool)
	for _, k := range keyErr.Want {
		for _, a := range algosForKeyType(k.Key.Type()) {
			if !seen[a] {
				seen[a] = true
				algos = append(algos, a)
			}
		}
	}
	return algos
}

// algosForKeyType maps a recorded host key's type to the signature algorithms a
// client should advertise to negotiate it. For RSA keys this expands to the
// SHA-2 variants, since modern servers reject the legacy SHA-1 ssh-rsa
// signature; every other type maps to itself.
func algosForKeyType(keyType string) []string {
	switch keyType {
	case ssh.KeyAlgoRSA:
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	case ssh.CertAlgoRSAv01:
		return []string{ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSAv01}
	default:
		return []string{keyType}
	}
}

// probeHostKey returns a throwaway public key used only to interrogate the
// known_hosts database; it is never sent over the wire.
func probeHostKey() (ssh.PublicKey, error) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return ssh.NewPublicKey(pub)
}

// zeroAddr is a placeholder net.Addr for the known_hosts probe. The knownhosts
// callback parses remote.String() as host:port before preferring the supplied
// address, so it must be non-nil and well-formed.
type zeroAddr struct{}

func (zeroAddr) Network() string { return "tcp" }
func (zeroAddr) String() string  { return "0.0.0.0:0" }

// mismatchError reports a changed host key, listing the offered and known
// fingerprints. This is the man-in-the-middle case; the connection is refused.
func mismatchError(hostname string, offered ssh.PublicKey, want []knownhosts.KnownKey) error {
	known := make([]string, 0, len(want))
	for _, k := range want {
		known = append(known, ssh.FingerprintSHA256(k.Key))
	}
	return fmt.Errorf("host key mismatch for %s: offered %s but known_hosts has %s — refusing to connect",
		hostname, ssh.FingerprintSHA256(offered), strings.Join(known, ", "))
}

// appendKnownHost appends a hashed known_hosts entry for hostname/key.
func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	line := knownhosts.Line([]string{knownhosts.HashHostname(knownhosts.Normalize(hostname))}, key)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, line); err != nil {
		return err
	}
	return nil
}

// ensureFile makes sure path (and its parent directory) exists so knownhosts.New
// can open it; a fresh install has no known_hosts yet, and every host is then
// treated as unknown (TOFU).
func ensureFile(path string) error {
	switch _, err := os.Stat(path); {
	case err == nil:
		return nil
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("stat known_hosts %s: %w", path, err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create known_hosts %s: %w", path, err)
	}
	return f.Close()
}
