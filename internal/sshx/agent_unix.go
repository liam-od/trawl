//go:build !windows

package sshx

import (
	"errors"
	"io"
	"net"
	"os"

	"golang.org/x/crypto/ssh/agent"
)

func dialAgent() (agent.Agent, io.Closer, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, nil, errors.New("SSH_AUTH_SOCK not set")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, nil, err
	}
	return agent.NewClient(conn), conn, nil
}
