package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load(missing) error: %v", err)
	}
	want := File{PasswordFallback: true}
	if !reflect.DeepEqual(got, want) {
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
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}
}

func TestSaveLoadRoundTripWithHosts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	in := File{
		DefaultUser:      "me",
		PasswordFallback: true,
		Hosts: map[string]Host{
			"prod": {
				User:      "admin",
				Host:      "10.0.0.5",
				Port:      2222,
				KeyPath:   "/home/me/.ssh/prod",
				RemoteDir: "/srv/www",
				LocalDir:  "/home/me/site",
			},
		},
	}
	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}

	h, ok := got.Host("prod")
	if !ok {
		t.Fatal(`Host("prod") not found`)
	}
	if h.Host != "10.0.0.5" {
		t.Errorf("Host(prod).Host = %q, want 10.0.0.5", h.Host)
	}
	if _, ok := got.Host("nope"); ok {
		t.Error(`Host("nope") = found, want missing`)
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

func TestRunSetupEditDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// 1) edit defaults: user, key, port, password? (n); then 5) quit.
	in := strings.NewReader("1\nalice\n/home/alice/.ssh/id\n2222\nn\n5\n")
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
	if !reflect.DeepEqual(got, want) {
		t.Errorf("setup wrote %+v, want %+v", got, want)
	}
	if !strings.Contains(out.String(), "Wrote "+path) {
		t.Errorf("setup output missing confirmation:\n%s", out.String())
	}
}

func TestRunSetupBlankInputKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// 1) edit defaults, accept every default, then 5) quit.
	in := strings.NewReader("1\n\n\n\n\n5\n")
	var out bytes.Buffer
	if err := RunSetup(in, &out, path); err != nil {
		t.Fatalf("RunSetup: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := File{PasswordFallback: true} // defaults: nothing set, password on
	if !reflect.DeepEqual(got, want) {
		t.Errorf("setup wrote %+v, want defaults %+v", got, want)
	}
}

func TestRunSetupQuitImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	in := strings.NewReader("5\n")
	var out bytes.Buffer
	if err := RunSetup(in, &out, path); err != nil {
		t.Fatalf("RunSetup: %v", err)
	}
	// Quitting without a change writes nothing.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("quit-immediately wrote a file (err=%v), want none", err)
	}
}

func TestRunSetupAddHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// 2) add host: name, user@host, port, key(blank), remote dir, local dir; 5) quit.
	in := strings.NewReader("2\nprod\nme@example.com\n2222\n\n/srv\n/home/me/dl\n5\n")
	var out bytes.Buffer
	if err := RunSetup(in, &out, path); err != nil {
		t.Fatalf("RunSetup: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after setup: %v", err)
	}
	want := Host{
		User:      "me",
		Host:      "example.com",
		Port:      2222,
		RemoteDir: "/srv",
		LocalDir:  "/home/me/dl",
	}
	h, ok := got.Host("prod")
	if !ok {
		t.Fatalf(`host "prod" not saved; file = %+v`, got)
	}
	if !reflect.DeepEqual(h, want) {
		t.Errorf("saved host = %+v, want %+v", h, want)
	}
}

func TestRunSetupNoOpDoesNotWrite(t *testing.T) {
	cases := map[string]string{
		"remove from empty": "4\n5\n",   // remove with no saved hosts
		"abort blank name":  "2\n\n5\n", // add host, blank name aborts
		"abort blank host":  "2\nname\n\n5\n",
	}
	for label, script := range cases {
		t.Run(label, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			var out bytes.Buffer
			if err := RunSetup(strings.NewReader(script), &out, path); err != nil {
				t.Fatalf("RunSetup: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("no-op action wrote a file (err=%v), want none", err)
			}
			if strings.Contains(out.String(), "Wrote ") {
				t.Errorf("no-op action printed a Wrote confirmation:\n%s", out.String())
			}
		})
	}
}

func TestRunSetupInvalidPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// 1) edit defaults: user, key, then an invalid port.
	in := strings.NewReader("1\n\n\nnotaport\n")
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
