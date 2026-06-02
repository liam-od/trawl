// Package sshx provides the authenticated SSH+SFTP transport that the rest of trawl talks
// through. It wraps golang.org/x/crypto/ssh and github.com/pkg/sftp behind a config-driven API
// so that nothing in the package depends on hardcoded key paths or environment globals.
package sshx

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Target identifies an SFTP endpoint parsed from a string of the form
// "user@host[:port][:/remote/path]". User and Path may be empty; Host is required.
type Target struct {
	User string
	Host string
	Port string
	Path string
}

// ParseTarget parses "user@host[:port][:/remote/path]". The leading "user@" and trailing
// ":port" / ":/path" segments are optional; Host is required. Port (if present) must be a
// digits-only number in the valid TCP range; Path (if present) must begin with "/".
func ParseTarget(s string) (Target, error) {
	if s == "" {
		return Target{}, errors.New("empty target")
	}

	var t Target
	rest := s
	if i := strings.Index(rest, "@"); i >= 0 {
		t.User = rest[:i]
		rest = rest[i+1:]
		if t.User == "" {
			return Target{}, fmt.Errorf("empty user in %q", s)
		}
	}

	parts := strings.SplitN(rest, ":", 3)
	t.Host = parts[0]
	if t.Host == "" {
		return Target{}, fmt.Errorf("empty host in %q", s)
	}

	switch len(parts) {
	case 1:
	case 2:
		seg := parts[1]
		switch {
		case strings.HasPrefix(seg, "/"):
			t.Path = seg
		case isPort(seg):
			t.Port = seg
		default:
			return Target{}, fmt.Errorf("expected port or /path after host in %q", s)
		}
	case 3:
		if !isPort(parts[1]) {
			return Target{}, fmt.Errorf("expected port in %q", s)
		}
		t.Port = parts[1]
		if !strings.HasPrefix(parts[2], "/") {
			return Target{}, fmt.Errorf("path must start with / in %q", s)
		}
		t.Path = parts[2]
	}

	return t, nil
}

func isPort(s string) bool {
	if s == "" {
		return false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n >= 1 && n <= 65535
}
