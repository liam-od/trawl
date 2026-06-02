package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load(missing) error: %v", err)
	}
	want := File{PasswordFallback: true}
	if got != want {
		t.Errorf("Load(missing) = %+v, want %+v", got, want)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	in := File{
		KeyPath:          "/home/me/.ssh/id_ed25519",
		DefaultUser:      "me",
		DefaultPort:      2222,
		PasswordFallback: false,
	}
	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perms = %o, want 600", perm)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != in {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}
}

func TestLoadOmittedPasswordKeyDefaultsTrue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// A file that sets only the user, omitting password_fallback entirely.
	if err := os.WriteFile(path, []byte(`{"default_user":"me"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.PasswordFallback {
		t.Error("omitted password_fallback should default to true")
	}
	if got.DefaultUser != "me" {
		t.Errorf("DefaultUser = %q, want me", got.DefaultUser)
	}
}

func TestLoadExplicitPasswordFalseIsHonored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"password_fallback":false}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.PasswordFallback {
		t.Error("explicit password_fallback=false should be honored")
	}
}

func TestRunSetupScriptedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// user, key, port, password? (n)
	in := strings.NewReader("alice\n/home/alice/.ssh/id\n2222\nn\n")
	var out bytes.Buffer
	if err := RunSetup(in, &out, path); err != nil {
		t.Fatalf("RunSetup: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after setup: %v", err)
	}
	want := File{
		KeyPath:          "/home/alice/.ssh/id",
		DefaultUser:      "alice",
		DefaultPort:      2222,
		PasswordFallback: false,
	}
	if got != want {
		t.Errorf("setup wrote %+v, want %+v", got, want)
	}
	if !strings.Contains(out.String(), "Wrote "+path) {
		t.Errorf("setup output missing confirmation:\n%s", out.String())
	}
}

func TestRunSetupBlankInputKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	in := strings.NewReader("\n\n\n\n") // accept every default
	var out bytes.Buffer
	if err := RunSetup(in, &out, path); err != nil {
		t.Fatalf("RunSetup: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := File{PasswordFallback: true} // defaults: nothing set, password on
	if got != want {
		t.Errorf("setup wrote %+v, want defaults %+v", got, want)
	}
}

func TestRunSetupInvalidPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	in := strings.NewReader("\n\nnotaport\n")
	var out bytes.Buffer
	if err := RunSetup(in, &out, path); err == nil {
		t.Fatal("expected error on invalid port, got nil")
	}
}

func TestDefaultPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if want := "/custom/xdg/trawl/config.json"; got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}

func TestDefaultPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/tester")
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if want := "/home/tester/.config/trawl/config.json"; got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}
