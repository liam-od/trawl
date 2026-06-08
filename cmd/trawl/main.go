// Command trawl is a dual-pane terminal SFTP file manager. It connects to a
// single SSH/SFTP endpoint and presents the local working directory beside the
// remote path for keyboard-driven browsing and copying.
package main

import (
	"bufio"
	"context"
	"encoding/json"
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
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/liam-od/trawl/internal/config"
	"github.com/liam-od/trawl/internal/fs"
	"github.com/liam-od/trawl/internal/job"
	"github.com/liam-od/trawl/internal/sshx"
	"github.com/liam-od/trawl/internal/transfer"
	"github.com/liam-od/trawl/internal/tree"
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
		transferSpec   = fset.String("transfer", "", "run a single JSON-described transfer and exit (no TUI)")
		listSpec       = fset.String("list", "", "list a JSON-described directory tree and exit (no TUI)")
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

	set := map[string]bool{}
	fset.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	flags := cliFlags{
		port:       *portFlag,
		user:       *userFlag,
		key:        *keyFlag,
		password:   *passwordFlag,
		noPassword: *noPasswordFlag,
		set:        set,
	}

	// --transfer and --list each run one JSON-described operation headlessly and
	// exit, instead of taking a positional target and starting the TUI. They are
	// mutually exclusive.
	if *transferSpec != "" && *listSpec != "" {
		fmt.Fprintln(stderr, "error: --transfer and --list are mutually exclusive")
		return 1
	}
	if *transferSpec != "" {
		return runTransfer(*transferSpec, *configPath, *knownHosts, flags, stdout, stderr)
	}
	if *listSpec != "" {
		return runList(*listSpec, *configPath, *knownHosts, flags, stdout, stderr)
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

	target, cfg, wantPassword, err := mergeSettings(target, fileCfg, flags)
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

// runTransfer handles --transfer: it parses the JSON spec, resolves the named
// saved host through the same flag>host>default merge the TUI path uses, and
// runs one copy headlessly. It returns the process exit code.
func runTransfer(specJSON, configPath, knownHosts string, flags cliFlags, stdout, stderr io.Writer) int {
	spec, err := job.Parse(specJSON)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	fileCfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	// A spec always names a saved host (its default dirs are the whole point), so
	// reject anything that isn't one rather than letting resolveArg fall through
	// to live-target parsing on an "@".
	if _, ok := fileCfg.Host(spec.Name); !ok {
		fmt.Fprintf(stderr, "error: no saved host %q (run trawl --setup to add one)\n", spec.Name)
		return 1
	}

	target, localStart, fileCfg, err := resolveArg(spec.Name, fileCfg)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	target, cfg, wantPassword, err := mergeSettings(target, fileCfg, flags)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if wantPassword {
		cfg.PasswordPrompt = passwordPrompt
	}
	cfg.KeyPath = expandHome(cfg.KeyPath)
	cfg.KnownHostsPath = expandHome(knownHosts)
	cfg.HostKeyPrompt = hostKeyPrompt

	if err := connectAndTransfer(target, cfg, spec, localStart, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// connectAndTransfer opens the session and runs the single copy described by
// spec. It resolves each side's base directory the way connectAndServe resolves
// the panels — the remote base is the host's remote_dir (carried on target.Path)
// unless the spec overrides it, with a leading ~ expanded against the server home
// known only after connecting; the local base is the host's local_dir unless
// overridden. The destination's parent directory is created if missing.
func connectAndTransfer(target sshx.Target, cfg sshx.Config, spec job.Spec, localStart string, stdout, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sess, err := sshx.Connect(ctx, target, cfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer sess.Close()

	remoteBase := resolveRemoteBase(sess, spec.RemotePath, target.Path)
	localBase := resolveLocalBase(spec.LocalPath, localStart)

	remotePath, localPath, srcIsRemote := spec.Resolve(remoteBase, localBase)
	localFS, remoteFS := fs.NewLocal(), fs.NewSFTP(sess.SFTP)

	var (
		src, dst         fs.FS
		srcPath, dstPath string
		name, dstParent  string
	)
	if srcIsRemote {
		src, srcPath, dst, dstPath = remoteFS, remotePath, localFS, localPath
		name, dstParent = path.Base(remotePath), filepath.Dir(localPath)
	} else {
		src, srcPath, dst, dstPath = localFS, localPath, remoteFS, remotePath
		name, dstParent = filepath.Base(localPath), path.Dir(remotePath)
	}

	// Copy creates the leaf of a directory transfer but not the parent of a single
	// file, so ensure the destination directory exists first.
	if err := dst.MkdirAll(dstParent); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}

	return runCopy(ctx, src, srcPath, dst, dstPath, name, stdout, stderr)
}

// resolveRemoteBase picks the remote base directory for a headless operation:
// the override if given, else the saved host's remote_dir (hostDir), else the
// server home. A leading ~ is expanded against the server home, which is only
// knowable after connecting.
func resolveRemoteBase(sess *sshx.Session, override, hostDir string) string {
	remoteHome, err := sess.SFTP.Getwd()
	if err != nil {
		remoteHome = "/"
	}
	base := hostDir
	if override != "" {
		base = override
	}
	if base == "" {
		return remoteHome
	}
	return expandRemoteHome(base, remoteHome)
}

// resolveLocalBase picks the local base directory: the override (with a leading
// ~ expanded against the local home) if given, else the saved host's local_dir
// (hostDir, already ~-expanded by the caller), else the working directory.
func resolveLocalBase(override, hostDir string) string {
	if override != "" {
		return expandHome(override)
	}
	if hostDir != "" {
		return hostDir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// runList handles --list: it parses the JSON spec, resolves the named saved host
// through the same flag>host>default merge the TUI path uses, and walks one side
// of it headlessly. It returns the process exit code.
func runList(specJSON, configPath, knownHosts string, flags cliFlags, stdout, stderr io.Writer) int {
	spec, err := job.ParseList(specJSON)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	fileCfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	// Like --transfer, a list spec always names a saved host (its default dirs are
	// the point), so reject anything that isn't one rather than falling through to
	// live-target parsing.
	if _, ok := fileCfg.Host(spec.Name); !ok {
		fmt.Fprintf(stderr, "error: no saved host %q (run trawl --setup to add one)\n", spec.Name)
		return 1
	}

	target, localStart, fileCfg, err := resolveArg(spec.Name, fileCfg)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	target, cfg, wantPassword, err := mergeSettings(target, fileCfg, flags)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if wantPassword {
		cfg.PasswordPrompt = passwordPrompt
	}
	cfg.KeyPath = expandHome(cfg.KeyPath)
	cfg.KnownHostsPath = expandHome(knownHosts)
	cfg.HostKeyPrompt = hostKeyPrompt

	if err := connectAndList(target, cfg, spec, localStart, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// listOutput is the JSON emitted by --list: the resolved base directory and its
// recursive contents (the base dir's own entries, dirs-first then alphabetical).
type listOutput struct {
	Path string      `json:"path"`
	Tree []tree.Node `json:"tree"`
}

// connectAndList walks one side's base directory into a tree and writes it to
// stdout as JSON. A local listing needs no network, so it skips the SSH session
// entirely; only a remote listing connects. The remote base is the host's
// remote_dir (on target.Path) unless the spec overrides it, with a leading ~
// expanded against the server home; the local base is the host's local_dir
// unless overridden.
func connectAndList(target sshx.Target, cfg sshx.Config, spec job.ListSpec, localStart string, stdout, stderr io.Writer) error {
	if spec.Side == job.SideLocal {
		base := resolveLocalBase(spec.Path, localStart)
		return writeList(stdout, fs.NewLocal(), base, spec.Depth)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sess, err := sshx.Connect(ctx, target, cfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer sess.Close()

	base := resolveRemoteBase(sess, spec.Path, target.Path)
	return writeList(stdout, fs.NewSFTP(sess.SFTP), base, spec.Depth)
}

// writeList builds the tree under base on fsys and encodes it to w as indented
// JSON.
func writeList(w io.Writer, fsys fs.FS, base string, depth int) error {
	nodes, err := tree.Build(fsys, base, depth)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(listOutput{Path: base, Tree: nodes})
}

// runCopy streams one transfer to completion, rendering a live progress line to
// stderr when it is a terminal and otherwise staying silent until the end. A
// successful transfer prints one summary line to stdout; an error is returned
// for the caller to report.
func runCopy(ctx context.Context, src fs.FS, srcPath string, dst fs.FS, dstPath, name string, stdout, stderr io.Writer) error {
	progress := make(chan transfer.CopyProgressMsg, 64)
	result := make(chan error, 1)
	go func() { result <- transfer.Copy(ctx, src, srcPath, dst, dstPath, progress) }()

	tty := isTerminal(stderr)
	meter := newRateMeter()
	start := time.Now()
	var last transfer.CopyProgressMsg

	for {
		select {
		case p := <-progress:
			last = p
			if tty {
				fmt.Fprintf(stderr, "\r\033[K%s", meter.line(name, p))
			}
		case err := <-result:
			// Drain any buffered samples so the byte total reflects the final one
			// (the channel is FIFO; the newest sample may be behind older ones).
			for drained := true; drained; {
				select {
				case p := <-progress:
					last = p
				default:
					drained = false
				}
			}
			if tty {
				fmt.Fprint(stderr, "\r\033[K")
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "done: %s (%s in %s)\n",
				name, humanBytes(last.Written), time.Since(start).Round(time.Second))
			return nil
		}
	}
}

// rateMeter smooths the transfer rate over real elapsed wall-clock time with an
// EMA. Dividing the byte delta by a fixed nominal tick would flicker to zero
// whenever a sample lands in a gap between pkg/sftp's bursty concurrent reads.
type rateMeter struct {
	lastTime  time.Time
	lastBytes int64
	ema       float64 // bytes per second
}

func newRateMeter() *rateMeter { return &rateMeter{lastTime: time.Now()} }

// line formats the progress line, recomputing the smoothed rate at most a few
// times a second so a slow sample window doesn't distort it.
func (m *rateMeter) line(name string, p transfer.CopyProgressMsg) string {
	if dt := time.Since(m.lastTime).Seconds(); dt >= 0.25 {
		inst := float64(p.Written-m.lastBytes) / dt
		if m.ema == 0 {
			m.ema = inst
		} else {
			const alpha = 0.3
			m.ema = alpha*inst + (1-alpha)*m.ema
		}
		m.lastTime = time.Now()
		m.lastBytes = p.Written
	}
	pct := " --%"
	if p.Total > 0 {
		pct = fmt.Sprintf("%3d%%", p.Written*100/p.Total)
	}
	return fmt.Sprintf("%s  %s  %s/s", name, pct, humanBytes(int64(m.ema)))
}

// isTerminal reports whether w is an *os.File attached to a terminal, so the
// progress line is only drawn when there is a human watching.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// humanBytes formats a byte count with a binary (KiB/MiB/…) unit.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
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
  --transfer JSON   run one JSON-described transfer headlessly, then exit
  --list JSON       print one JSON-described directory tree, then exit
  --version         print version and exit
  --help            show this help and exit

Connection settings resolve as: CLI flag > saved host > config default > built-in.

--transfer takes a JSON object naming a saved host and a direction; paths fall
back to that host's configured remote_dir/local_dir:

  {"name":"box","type":"remote_to_local","object":"film.mkv"}

  name         saved host to connect to (required)
  type         remote_to_local (download) or local_to_remote (upload) (required)
  object       file or directory within the base dir to move (default: whole dir)
  remote_path  override the host's remote_dir base for this transfer (optional)
  local_path   override the host's local_dir base for this transfer (optional)

--list takes a JSON object naming a saved host and a side; it prints that side's
base directory and a recursive tree of its contents as JSON on stdout:

  {"name":"box","side":"remote","path":"/srv/media","depth":2}

  name         saved host to connect to (required)
  side         remote or local — which side's directory to walk (required)
  path         override the host's remote_dir/local_dir base (optional)
  depth        max levels to recurse below the base; omit/0 for no limit (optional)
`
