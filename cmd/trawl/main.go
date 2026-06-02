// Command trawl is a dual-pane terminal SFTP file manager. It connects to a
// single SSH/SFTP endpoint and presents the local working directory beside the
// remote path for keyboard-driven browsing and copying.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/liam-od/trawl/internal/config"
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
		portFlag       = fset.Int("port", 0, "SSH port (overrides :port in the target; default 22)")
		userFlag       = fset.String("user", "", "override the user in the target")
		keyFlag        = fset.String("key", "", "private key file (otherwise the SSH agent is used)")
		passwordFlag   = fset.Bool("password", true, "allow password authentication as a fallback")
		noPasswordFlag = fset.Bool("no-password", false, "disable the password fallback")
		knownHosts     = fset.String("known-hosts", defaultKnownHosts(), "known_hosts file")
		configPath     = fset.String("config", defaultConfigPath(), "config file path")
		setup          = fset.Bool("setup", false, "run the interactive setup wizard and exit")
		showVersion    = fset.Bool("version", false, "print version and exit")
	)

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

	if *setup {
		if err := config.RunSetup(os.Stdin, stdout, *configPath); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}

	rest := fset.Args()
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "error: expected exactly one target or saved host name")
		fset.Usage()
		return 1
	}

	fileCfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	target, localStart, fileCfg, err := resolveArg(rest[0], fileCfg)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		if strings.Contains(rest[0], "@") {
			fset.Usage() // a malformed live target; show the format
		}
		return 1
	}

	set := map[string]bool{}
	fset.Visit(func(fl *flag.Flag) { set[fl.Name] = true })

	target, cfg, wantPassword, err := mergeSettings(target, fileCfg, cliFlags{
		port:       *portFlag,
		user:       *userFlag,
		key:        *keyFlag,
		password:   *passwordFlag,
		noPassword: *noPasswordFlag,
		set:        set,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fset.Usage()
		return 1
	}
	if wantPassword {
		cfg.PasswordPrompt = passwordPrompt
	}
	// Expand a leading ~ in paths that may come from the config file, where (unlike
	// the command line) no shell did it for us.
	cfg.KeyPath = expandHome(cfg.KeyPath)
	cfg.KnownHostsPath = expandHome(*knownHosts)
	cfg.HostKeyPrompt = hostKeyPrompt

	if err := connectAndServe(target, cfg, localStart); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// resolveArg turns the single positional argument into a connection target. An
// argument containing "@" is parsed as a live "user@host[:port][:/path]" target;
// anything else is looked up as a saved host name (a miss is an error). For a
// saved host it returns the local start directory (with a leading ~ expanded
// against the local home) and a copy of fileCfg with the host's key overlaid on
// the global default, so the later flag>host>default merge stays correct.
func resolveArg(arg string, fileCfg config.File) (sshx.Target, string, config.File, error) {
	if strings.Contains(arg, "@") {
		target, err := sshx.ParseTarget(arg)
		return target, "", fileCfg, err
	}

	host, ok := fileCfg.Host(arg)
	if !ok {
		return sshx.Target{}, "", fileCfg,
			fmt.Errorf("no saved host %q (run trawl --setup to add one)", arg)
	}
	target := sshx.Target{User: host.User, Host: host.Host, Path: host.RemoteDir}
	if host.Port != 0 {
		target.Port = strconv.Itoa(host.Port)
	}
	if host.KeyPath != "" {
		fileCfg.KeyPath = host.KeyPath
	}
	return target, expandHome(host.LocalDir), fileCfg, nil
}

// cliFlags carries the parsed flag values plus the set of flag names the user
// actually passed, so mergeSettings can apply the precedence CLI > config >
// default.
type cliFlags struct {
	port       int
	user       string
	key        string
	password   bool
	noPassword bool
	set        map[string]bool
}

// mergeSettings resolves the target user and connection config from the parsed
// target, the config file, and the CLI flags, applying the order: explicit CLI
// flag > config file > built-in default. It returns the resolved target, the
// sshx.Config (without PasswordPrompt), and whether password auth should be
// offered. An empty resolved user is an error.
func mergeSettings(target sshx.Target, fileCfg config.File, f cliFlags) (sshx.Target, sshx.Config, bool, error) {
	if f.set["user"] {
		target.User = f.user
	}
	if target.User == "" {
		target.User = fileCfg.DefaultUser
	}
	if target.User == "" {
		return target, sshx.Config{}, false,
			errors.New("no user given; pass user@host, --user, or set default_user via --setup")
	}

	// Port precedence: --port > target :port > config default_port > 22.
	// Leaving cfg.Port at 0 defers to sshx.resolvePort, which then prefers the
	// target's own port. So config's default only applies when the target
	// carries no explicit port.
	port := 0
	switch {
	case f.set["port"]:
		port = f.port
	case target.Port == "":
		port = fileCfg.DefaultPort
	}
	key := fileCfg.KeyPath
	if f.set["key"] {
		key = f.key
	}
	password := fileCfg.PasswordFallback
	if f.set["password"] || f.set["no-password"] {
		password = f.password && !f.noPassword
	}

	return target, sshx.Config{Port: port, KeyPath: key}, password, nil
}

// connectAndServe opens the SSH/SFTP session and runs the TUI until the user
// quits or a termination signal arrives, tearing the session down in order. An
// empty localStart defaults to the current working directory.
func connectAndServe(target sshx.Target, cfg sshx.Config, localStart string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sess, err := sshx.Connect(ctx, target, cfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer sess.Close()

	if localStart == "" {
		if localStart, err = os.Getwd(); err != nil {
			localStart = "/"
		}
	}

	// The remote home is only knowable once connected, so a remote ~ (and the
	// no-path default) is resolved here against the server's working directory.
	remoteHome, err := sess.SFTP.Getwd()
	if err != nil {
		remoteHome = "/"
	}
	remoteStart := target.Path
	if remoteStart == "" {
		remoteStart = remoteHome
	} else {
		remoteStart = expandRemoteHome(remoteStart, remoteHome)
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

// hostKeyPrompt asks the user, on a first connection, whether to trust an
// unknown host key. It runs before the TUI takes over the screen.
func hostKeyPrompt(host, fingerprint string) (bool, error) {
	fmt.Fprintf(os.Stderr, "The authenticity of host %q can't be established.\n", host)
	fmt.Fprintf(os.Stderr, "Key fingerprint is %s.\n", fingerprint)
	fmt.Fprint(os.Stderr, "Add to known_hosts? [y/N]: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
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

// expandHome replaces a leading ~ (alone or before a separator) with the user's
// home directory. It leaves the path unchanged if home can't be determined or
// there's no leading ~, so callers can apply it unconditionally.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// expandRemoteHome replaces a leading ~ (alone or before a slash) in a remote
// path with the remote home directory. Remote paths are always POSIX, so it uses
// the path package, never filepath. A path without a leading ~ is returned
// unchanged, so callers can apply it unconditionally.
func expandRemoteHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return path.Join(home, p[2:])
	}
	return p
}

// defaultConfigPath returns the config file location, or an empty string if it
// can't be determined.
func defaultConfigPath() string {
	p, err := config.DefaultPath()
	if err != nil {
		return ""
	}
	return p
}

const usageText = `usage: trawl [flags] user@host[:port][:/remote/path]
       trawl [flags] <saved-host-name>

A dual-pane terminal SFTP file manager. The argument is either a live target of
the form user@host[:port][:/remote/path], or — if it contains no "@" — the name
of a host saved via "trawl --setup".

Flags:
  --port N          SSH port (overrides :port in the target; default 22)
  --user NAME       override the user in the target
  --key PATH        private key file (otherwise the SSH agent is used)
  --password        allow password authentication as a fallback (default true)
  --no-password     disable the password fallback
  --known-hosts P   known_hosts file (default ~/.ssh/known_hosts)
  --config PATH     config file (default ~/.config/trawl/config.json)
  --setup           manage saved hosts and global defaults, then exit
  --version         print version and exit
  --help            show this help and exit

Connection settings resolve as: CLI flag > saved host > config default > built-in.
`
