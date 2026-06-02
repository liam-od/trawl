package sshx

import (
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// buildAuthMethods assembles the ssh.AuthMethod list in fixed order: agent (silent skip if
// unavailable), explicit key (loud failure if KeyPath is set but unreadable/unparsable),
// password prompt (if non-nil). Returns the closers tied to the agent connection so Session
// can tear them down on Close.
func buildAuthMethods(cfg Config) ([]ssh.AuthMethod, []io.Closer, error) {
	var methods []ssh.AuthMethod
	var closers []io.Closer

	if !cfg.DisableAgent {
		if a, c, err := cfg.dialAgent(); err == nil {
			methods = append(methods, ssh.PublicKeysCallback(a.Signers))
			if c != nil {
				closers = append(closers, c)
			}
		}
	}

	if cfg.KeyPath != "" {
		data, err := cfg.readKey(cfg.KeyPath)
		if err != nil {
			closeAll(closers)
			return nil, nil, fmt.Errorf("read key %s: %w", cfg.KeyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			closeAll(closers)
			return nil, nil, fmt.Errorf("parse key %s: %w", cfg.KeyPath, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if cfg.PasswordPrompt != nil {
		methods = append(methods, ssh.PasswordCallback(cfg.PasswordPrompt))
	}

	if len(methods) == 0 {
		return nil, nil, errors.New("no auth methods available: enable the agent, pass a key, or wire a password prompt")
	}
	return methods, closers, nil
}

func closeAll(cs []io.Closer) {
	for _, c := range cs {
		_ = c.Close()
	}
}
