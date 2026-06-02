// Command trawl is a dual-pane terminal SFTP file manager. It connects to a
// single SSH/SFTP endpoint and presents the local working directory beside the
// remote path for keyboard-driven browsing and copying.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/liam-od/trawl/internal/fs"
	"github.com/liam-od/trawl/internal/sshx"
	"github.com/liam-od/trawl/internal/ui"
)

var version = "0.0.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses arguments and either handles an early-exit flag (--version,
// --help, bad input) or hands off to connectAndServe. It returns the process
// exit code and writes all user-facing output to the provided writers, so the
// argument-handling paths are testable without a network or a terminal.
func run(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("trawl", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() { fmt.Fprint(stderr, usageText) }

	var (
		port        = fset.Int("port", 0, "SSH port (overrides :port in the target; default 22)")
		user        = fset.String("user", "", "override the user in the target")
		keyPath     = fset.String("key", "", "private key file (otherwise the SSH agent is used)")
		password    = fset.Bool("password", true, "allow password authentication as a fallback")
		noPassword  = fset.Bool("no-password", false, "disable the password fallback")
		knownHosts  = fset.String("known-hosts", defaultKnownHosts(), "known_hosts file (used from M6)")
		showVersion = fset.Bool("version", false, "print version and exit")
	)
	_ = knownHosts // TODO(M6): feed into the host-key callback.

	switch err := fset.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		return 0 // Parse already printed the usage text.
	case err != nil:
		return 1 // Parse already printed the error and usage.
	}

	if *showVersion {
		fmt.Fprintf(stdout, "trawl %s\n", version)
		return 0
	}

	rest := fset.Args()
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "error: expected exactly one target")
		fset.Usage()
		return 1
	}

	target, err := sshx.ParseTarget(rest[0])
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fset.Usage()
		return 1
	}
	if *user != "" {
		target.User = *user
	}
	if target.User == "" {
		fmt.Fprintln(stderr, "error: no user given; target must be user@host[:port][:/path]")
		fset.Usage()
		return 1
	}

	cfg := sshx.Config{Port: *port, KeyPath: *keyPath}
	if *password && !*noPassword {
		cfg.PasswordPrompt = passwordPrompt
	}

	if err := connectAndServe(target, cfg); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// connectAndServe opens the SSH/SFTP session and runs the TUI until the user
// quits or a termination signal arrives, tearing the session down in order.
func connectAndServe(target sshx.Target, cfg sshx.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sess, err := sshx.Connect(ctx, target, cfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer sess.Close()

	localStart, err := os.Getwd()
	if err != nil {
		localStart = "/"
	}
	remoteStart := target.Path
	if remoteStart == "" {
		// No path given: start at the remote working directory (the user's home
		// on most servers).
		if wd, err := sess.SFTP.Getwd(); err == nil {
			remoteStart = wd
		} else {
			remoteStart = "/"
		}
	}

	model := ui.New(fs.NewLocal(), fs.NewSFTP(sess.SFTP), localStart, remoteStart)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// A termination signal quits the program so the deferred session teardown
	// runs in order (SFTP → SSH → agent socket).
	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	_, err = p.Run()
	return err
}

// passwordPrompt reads a password from the controlling terminal without echo.
func passwordPrompt() (string, error) {
	fmt.Fprint(os.Stderr, "password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(b), nil
}

// defaultKnownHosts returns ~/.ssh/known_hosts, or an empty string if the home
// directory can't be determined.
func defaultKnownHosts() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

const usageText = `usage: trawl [flags] user@host[:port][:/remote/path]

A dual-pane terminal SFTP file manager.

Flags:
  --port N          SSH port (overrides :port in the target; default 22)
  --user NAME       override the user in the target
  --key PATH        private key file (otherwise the SSH agent is used)
  --password        allow password authentication as a fallback (default true)
  --no-password     disable the password fallback
  --known-hosts P   known_hosts file (default ~/.ssh/known_hosts)
  --version         print version and exit
  --help            show this help and exit
`
