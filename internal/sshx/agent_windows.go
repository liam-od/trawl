//go:build windows

package sshx

import (
	"io"

	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh/agent"
)

const windowsAgentPipe = `\\.\pipe\openssh-ssh-agent`

func dialAgent() (agent.Agent, io.Closer, error) {
	conn, err := winio.DialPipe(windowsAgentPipe, nil)
	if err != nil {
		return nil, nil, err
	}
	return agent.NewClient(conn), conn, nil
}
