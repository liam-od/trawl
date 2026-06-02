package sshx

import (
	"io"
	"os"

	"golang.org/x/crypto/ssh/agent"
)

// Config controls how Connect authenticates and dials. The package contains no hardcoded key
// paths; callers (typically cmd/trawl) supply KeyPath and PasswordPrompt. With a zero Config,
// only the agent is tried — if it isn't available, Connect returns a helpful error.
type Config struct {
	// Port overrides the port carried in Target. Zero falls back to Target.Port, then 22.
	Port int

	// DisableAgent skips SSH_AUTH_SOCK / OpenSSH-on-Windows agent discovery. The zero value
	// (false) means the agent is consulted — see auth.buildAuthMethods for the order.
	DisableAgent bool

	// KeyPath, when set, names a single private key file to load. Failures (missing,
	// unreadable, unparsable) are reported loudly because the caller asked for this key.
	KeyPath string

	// PasswordPrompt, when non-nil, is appended as a final auth method. Implementations are
	// expected to read from a controlling terminal (e.g. golang.org/x/term.ReadPassword).
	PasswordPrompt func() (string, error)

	dialAgent func() (agent.Agent, io.Closer, error)
	readKey   func(path string) ([]byte, error)
}

func (c *Config) applyDefaults() {
	if c.dialAgent == nil {
		c.dialAgent = dialAgent
	}
	if c.readKey == nil {
		c.readKey = os.ReadFile
	}
}
