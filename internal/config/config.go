// Package config persists the connection defaults that cmd/trawl would
// otherwise demand on every invocation (key path, default user/port, whether to
// offer password auth) plus any number of named saved hosts. It reads and writes
// a small JSON file and provides an interactive wizard to manage it. Theming is
// out of scope for v1.
package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// File is the on-disk configuration. Zero-valued string/int fields mean "not
// set" and let the built-in default apply during the CLI merge. PasswordFallback
// defaults to true (see Load), since password auth is offered unless disabled.
type File struct {
	KeyPath          string `json:"key_path,omitempty"`
	DefaultUser      string `json:"default_user,omitempty"`
	DefaultPort      int    `json:"default_port,omitempty"`
	PasswordFallback bool   `json:"password_fallback"`
	// Exclude lists base-name glob patterns (path.Match syntax) pruned from
	// directory transfers, in the TUI and headless alike. A matching directory is
	// skipped whole, never descended into. It defaults to the patterns in
	// defaults(); set it to [] in the file to copy everything.
	Exclude []string        `json:"exclude,omitempty"`
	Hosts   map[string]Host `json:"hosts,omitempty"`
}

// Host is a saved connection, addressable by the name it is keyed under in
// File.Hosts. Empty fields inherit the global defaults (or built-in defaults)
// during the CLI merge. RemoteDir/LocalDir set where each panel opens; a leading
// ~ in LocalDir is the local home, in RemoteDir the remote home (expanded by the
// caller, since the two homes live on different machines).
type Host struct {
	User      string `json:"user,omitempty"`
	Host      string `json:"host"`
	Port      int    `json:"port,omitempty"`
	KeyPath   string `json:"key_path,omitempty"`
	RemoteDir string `json:"remote_dir,omitempty"`
	LocalDir  string `json:"local_dir,omitempty"`
}

// Host returns the saved host stored under name, and whether it was found.
func (f File) Host(name string) (Host, bool) {
	h, ok := f.Hosts[name]
	return h, ok
}

// defaults returns the File used when no config file exists yet, and the seed
// for unmarshalling an existing one. PasswordFallback is true so that an absent
// file — or one that omits the key — leaves password auth enabled. Exclude seeds
// the transfer skip-list; a file that omits "exclude" inherits it, while one
// that sets "exclude" (including to []) replaces it wholesale.
func defaults() File {
	return File{
		PasswordFallback: true,
		Exclude:          []string{".venv", "__pycache__"},
	}
}

// DefaultPath returns ${XDG_CONFIG_HOME:-~/.config}/trawl/config.json.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "trawl", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "trawl", "config.json"), nil
}

// Load reads the config file at path. A missing file is not an error: it returns
// the built-in defaults. An existing file is overlaid on those defaults, so a
// key omitted from the file keeps its default (notably PasswordFallback=true).
func Load(path string) (File, error) {
	f := defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return f, nil
}

// Save writes f to path as indented JSON with 0600 permissions, creating the
// parent directory (0700) if needed.
func (f File) Save(path string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create config dir %s: %w", dir, err)
		}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// RunSetup runs the interactive menu that manages the config file at path:
// editing the global defaults and adding, editing, or removing saved hosts. It
// loops until the user chooses to quit, saving after each change. Reading from in
// and writing prompts to out keeps it testable with scripted input.
func RunSetup(in io.Reader, out io.Writer, path string) error {
	cur, err := Load(path)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(in)

	for {
		fmt.Fprint(out, "\ntrawl setup\n"+
			"  1) Edit global defaults\n"+
			"  2) Add a saved host\n"+
			"  3) Edit a saved host\n"+
			"  4) Remove a saved host\n"+
			"  5) Quit\n"+
			"> ")
		choice := readLine(sc)
		var (
			changed bool
			err     error
		)
		switch choice {
		case "1":
			changed, err = editDefaults(sc, out, &cur)
		case "2", "3":
			changed, err = editHost(sc, out, &cur)
		case "4":
			changed = removeHost(sc, out, &cur)
		case "5", "":
			return nil
		default:
			fmt.Fprintf(out, "unknown choice %q\n", choice)
			continue
		}
		if err != nil {
			return err
		}
		// Only persist (and report) when the action actually changed something,
		// so aborted entries and no-op choices don't write the file or claim to.
		if changed {
			if err := cur.Save(path); err != nil {
				return err
			}
			fmt.Fprintf(out, "Wrote %s\n", path)
		}
	}
}

// editDefaults prompts for the global default fields, prefilled from cur. It
// reports changed=true once the prompts complete (an invalid port returns the
// error instead).
func editDefaults(sc *bufio.Scanner, out io.Writer, cur *File) (changed bool, err error) {
	cur.DefaultUser = promptString(sc, out, "Default user", cur.DefaultUser)
	cur.KeyPath = promptString(sc, out, "Private key path (blank = use SSH agent)", cur.KeyPath)

	port, err := promptPort(sc, out, cur.DefaultPort)
	if err != nil {
		return false, err
	}
	cur.DefaultPort = port

	cur.PasswordFallback = promptBool(sc, out, "Allow password fallback", cur.PasswordFallback)
	return true, nil
}

// editHost prompts for a saved host and stores it in cur.Hosts. It handles both
// adding a new host and editing an existing one: the entry under the chosen name
// (if any) prefills the prompts. It reports changed=false when the entry is
// aborted (blank name or host) so the caller skips the save.
func editHost(sc *bufio.Scanner, out io.Writer, cur *File) (changed bool, err error) {
	name := promptString(sc, out, "Host name", "")
	if name == "" {
		fmt.Fprintln(out, "name is required")
		return false, nil
	}
	h := cur.Hosts[name] // zero value if new

	userHost := promptString(sc, out, "user@host", joinUserHost(h.User, h.Host))
	if userHost == "" {
		fmt.Fprintln(out, "host is required")
		return false, nil
	}
	h.User, h.Host = splitUserHost(userHost)
	if h.Host == "" {
		fmt.Fprintln(out, "host is required")
		return false, nil
	}

	port, err := promptPort(sc, out, h.Port)
	if err != nil {
		return false, err
	}
	h.Port = port
	h.KeyPath = promptString(sc, out, "Private key path (blank = inherit global)", h.KeyPath)
	h.RemoteDir = promptString(sc, out, "Remote directory", defaultOr(h.RemoteDir, "~"))
	h.LocalDir = promptString(sc, out, "Local directory", defaultOr(h.LocalDir, localCwd()))

	if cur.Hosts == nil {
		cur.Hosts = map[string]Host{}
	}
	cur.Hosts[name] = h
	fmt.Fprintf(out, "saved host %q\n", name)
	return true, nil
}

// removeHost lists the saved hosts and deletes the one the user names. It reports
// changed=true only when an entry was actually deleted.
func removeHost(sc *bufio.Scanner, out io.Writer, cur *File) (changed bool) {
	if len(cur.Hosts) == 0 {
		fmt.Fprintln(out, "no saved hosts")
		return false
	}
	fmt.Fprintln(out, "Saved hosts:")
	for name := range cur.Hosts {
		fmt.Fprintf(out, "  %s\n", name)
	}
	name := promptString(sc, out, "Remove which host", "")
	if name == "" {
		return false
	}
	if _, ok := cur.Hosts[name]; !ok {
		fmt.Fprintf(out, "no saved host %q\n", name)
		return false
	}
	delete(cur.Hosts, name)
	fmt.Fprintf(out, "removed host %q\n", name)
	return true
}

// splitUserHost splits "user@host" on the first @. With no @, the whole string
// is the host and the user is empty.
func splitUserHost(s string) (user, host string) {
	if i := strings.Index(s, "@"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// joinUserHost is the inverse of splitUserHost, used to prefill the prompt.
func joinUserHost(user, host string) string {
	if user == "" {
		return host
	}
	return user + "@" + host
}

// defaultOr returns cur if set, otherwise fallback. Used to seed a prompt's
// default when the stored value is empty.
func defaultOr(cur, fallback string) string {
	if cur != "" {
		return cur
	}
	return fallback
}

// localCwd returns the current working directory, or "" if it can't be
// determined, for use as a saved host's default local directory.
func localCwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

func promptString(sc *bufio.Scanner, out io.Writer, label, cur string) string {
	if cur != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, cur)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	if line := readLine(sc); line != "" {
		return line
	}
	return cur
}

func promptPort(sc *bufio.Scanner, out io.Writer, cur int) (int, error) {
	shown := cur
	if shown == 0 {
		shown = 22
	}
	fmt.Fprintf(out, "Default port [%d]: ", shown)
	line := readLine(sc)
	if line == "" {
		return cur, nil
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("invalid port %q", line)
	}
	return n, nil
}

func promptBool(sc *bufio.Scanner, out io.Writer, label string, cur bool) bool {
	def := "Y/n"
	if !cur {
		def = "y/N"
	}
	fmt.Fprintf(out, "%s [%s]: ", label, def)
	switch strings.ToLower(readLine(sc)) {
	case "":
		return cur
	case "y", "yes":
		return true
	default:
		return false
	}
}

func readLine(sc *bufio.Scanner) string {
	if !sc.Scan() {
		return ""
	}
	return strings.TrimSpace(sc.Text())
}
