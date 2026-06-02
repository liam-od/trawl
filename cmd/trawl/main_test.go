package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/liam-od/trawl/internal/config"
	"github.com/liam-od/trawl/internal/sshx"
)

// runArgs invokes run with captured output and returns (code, stdout, stderr).
// It points XDG_CONFIG_HOME at a fresh temp dir so no real ~/.config/trawl
// config file leaks into the test's merge behavior.
func runArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestVersionFlag(t *testing.T) {
	code, out, _ := runArgs(t, "--version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := "trawl " + version + "\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestHelpFlagExitsZero(t *testing.T) {
	code, _, errOut := runArgs(t, "--help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(errOut, "usage: trawl") {
		t.Errorf("help output missing usage line:\n%s", errOut)
	}
}

func TestNoArgsIsUsageError(t *testing.T) {
	code, _, errOut := runArgs(t)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "usage: trawl") {
		t.Errorf("expected usage text on stderr, got:\n%s", errOut)
	}
}

func TestBareHostWithoutUserIsRejected(t *testing.T) {
	// "bogus" parses as a bare host but has no user, which trawl requires.
	code, _, errOut := runArgs(t, "bogus")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "user@host") {
		t.Errorf("expected an error mentioning the expected format, got:\n%s", errOut)
	}
}

func TestMalformedTargetIsRejected(t *testing.T) {
	code, _, errOut := runArgs(t, "me@host:notaport:/path")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "usage: trawl") {
		t.Errorf("expected usage text on stderr, got:\n%s", errOut)
	}
}

func TestTooManyArgs(t *testing.T) {
	code, _, errOut := runArgs(t, "me@a", "me@b")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "exactly one target") {
		t.Errorf("expected too-many-args error, got:\n%s", errOut)
	}
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	code, _, _ := runArgs(t, "--bogus-flag")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestExpandHome(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cases := map[string]string{
		"~/.ssh/key":      "/home/tester/.ssh/key",
		"~":               "/home/tester",
		"/abs/path":       "/abs/path",
		"relative/path":   "relative/path",
		"~tricky/notpath": "~tricky/notpath", // ~ not followed by / is left alone
		"":                "",
	}
	for in, want := range cases {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeUserPrecedence(t *testing.T) {
	fileCfg := config.File{DefaultUser: "cfguser", PasswordFallback: true}

	// target user@ beats config default.
	got, _, _, err := mergeSettings(sshx.Target{User: "targetuser", Host: "h"}, fileCfg, cliFlags{set: map[string]bool{}})
	if err != nil || got.User != "targetuser" {
		t.Errorf("target user: got %q err %v, want targetuser", got.User, err)
	}

	// --user beats both.
	got, _, _, err = mergeSettings(sshx.Target{User: "targetuser", Host: "h"}, fileCfg,
		cliFlags{user: "flaguser", set: map[string]bool{"user": true}})
	if err != nil || got.User != "flaguser" {
		t.Errorf("flag user: got %q err %v, want flaguser", got.User, err)
	}

	// No target user, no flag → config default fills in.
	got, _, _, err = mergeSettings(sshx.Target{Host: "h"}, fileCfg, cliFlags{set: map[string]bool{}})
	if err != nil || got.User != "cfguser" {
		t.Errorf("config user: got %q err %v, want cfguser", got.User, err)
	}

	// Nothing supplies a user → error.
	if _, _, _, err := mergeSettings(sshx.Target{Host: "h"}, config.File{}, cliFlags{set: map[string]bool{}}); err == nil {
		t.Error("expected error when no user is available")
	}
}

func TestMergePortKeyPassword(t *testing.T) {
	fileCfg := config.File{DefaultPort: 2222, KeyPath: "/cfg/key", PasswordFallback: false}
	base := sshx.Target{User: "me", Host: "h"}

	// Config values apply when no flags are set.
	_, cfg, pwd, err := mergeSettings(base, fileCfg, cliFlags{set: map[string]bool{}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if cfg.Port != 2222 || cfg.KeyPath != "/cfg/key" || pwd {
		t.Errorf("config layer: port=%d key=%q pwd=%v, want 2222 /cfg/key false", cfg.Port, cfg.KeyPath, pwd)
	}

	// Explicit flags override config.
	_, cfg, pwd, err = mergeSettings(base, fileCfg, cliFlags{
		port: 22, key: "/flag/key", password: true,
		set: map[string]bool{"port": true, "key": true, "password": true},
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if cfg.Port != 22 || cfg.KeyPath != "/flag/key" || !pwd {
		t.Errorf("flag layer: port=%d key=%q pwd=%v, want 22 /flag/key true", cfg.Port, cfg.KeyPath, pwd)
	}

	// An explicit port in the target beats config's default_port...
	_, cfg, _, err = mergeSettings(sshx.Target{User: "me", Host: "h", Port: "2200"}, fileCfg, cliFlags{set: map[string]bool{}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if cfg.Port != 0 {
		t.Errorf("target :port present: cfg.Port=%d, want 0 (defer to target via sshx)", cfg.Port)
	}
	// ...but --port still wins over the target's port.
	_, cfg, _, err = mergeSettings(sshx.Target{User: "me", Host: "h", Port: "2200"}, fileCfg,
		cliFlags{port: 22, set: map[string]bool{"port": true}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if cfg.Port != 22 {
		t.Errorf("--port with target :port: cfg.Port=%d, want 22", cfg.Port)
	}

	// --no-password disables even when config enabled it.
	enabled := config.File{PasswordFallback: true}
	_, _, pwd, _ = mergeSettings(base, enabled, cliFlags{
		password: true, noPassword: true, set: map[string]bool{"no-password": true},
	})
	if pwd {
		t.Error("--no-password should disable password auth")
	}
}
