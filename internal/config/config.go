// Package config persists the connection defaults that cmd/trawl would
// otherwise demand on every invocation (key path, default user/port, whether to
// offer password auth). It reads and writes a small JSON file and provides an
// interactive wizard to create it. Multiple saved hosts and theming are out of
// scope for v1.
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
}

// defaults returns the File used when no config file exists yet, and the seed
// for unmarshalling an existing one. PasswordFallback is true so that an absent
// file — or one that omits the key — leaves password auth enabled.
func defaults() File {
	return File{PasswordFallback: true}
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

// RunSetup prompts for each field (prefilled from any existing file at path),
// writes the result with 0600 perms, and reports where it wrote. Reading from
// in and writing prompts to out keeps it testable with scripted input.
func RunSetup(in io.Reader, out io.Writer, path string) error {
	cur, err := Load(path)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(in)

	cur.DefaultUser = promptString(sc, out, "Default user", cur.DefaultUser)
	cur.KeyPath = promptString(sc, out, "Private key path (blank = use SSH agent)", cur.KeyPath)

	port, err := promptPort(sc, out, cur.DefaultPort)
	if err != nil {
		return err
	}
	cur.DefaultPort = port

	cur.PasswordFallback = promptBool(sc, out, "Allow password fallback", cur.PasswordFallback)

	if err := cur.Save(path); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nWrote %s\n", path)
	return nil
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
