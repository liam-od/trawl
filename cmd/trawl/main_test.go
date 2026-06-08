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

func TestBareWordIsTreatedAsSavedHost(t *testing.T) {
	// A bare word (no "@") is a saved host name, not a live target. With no
	// matching host it fails with a no-saved-host error rather than connecting.
	code, _, errOut := runArgs(t, "bogus")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "no saved host") {
		t.Errorf("expected a no-saved-host error, got:\n%s", errOut)
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

func TestExpandRemoteHome(t *testing.T) {
	const home = "/home/remote"
	cases := map[string]string{
		"~":        home,
		"~/logs":   home + "/logs",
		"~/a/b":    home + "/a/b",
		"/srv/www": "/srv/www",
		"relative": "relative",
		"~tricky":  "~tricky", // ~ not followed by / is left alone
		"":         "",
	}
	for in, want := range cases {
		if got := expandRemoteHome(in, home); got != want {
			t.Errorf("expandRemoteHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveArgLiveTarget(t *testing.T) {
	target, local, _, err := resolveArg("me@host:2200:/srv", config.File{})
	if err != nil {
		t.Fatalf("resolveArg: %v", err)
	}
	if target.User != "me" || target.Host != "host" || target.Port != "2200" || target.Path != "/srv" {
		t.Errorf("target = %+v, want me@host:2200:/srv", target)
	}
	if local != "" {
		t.Errorf("localStart = %q, want empty (defer to cwd)", local)
	}
}

func TestResolveArgSavedHost(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	fileCfg := config.File{
		KeyPath: "/cfg/key",
		Hosts: map[string]config.Host{
			"prod": {
				User: "admin", Host: "10.0.0.5", Port: 2222,
				KeyPath: "/host/key", RemoteDir: "/srv/www", LocalDir: "~/site",
			},
		},
	}
	target, local, cfg, err := resolveArg("prod", fileCfg)
	if err != nil {
		t.Fatalf("resolveArg: %v", err)
	}
	if target.User != "admin" || target.Host != "10.0.0.5" || target.Port != "2222" || target.Path != "/srv/www" {
		t.Errorf("target = %+v, want admin@10.0.0.5:2222:/srv/www", target)
	}
	if want := "/home/tester/site"; local != want {
		t.Errorf("localStart = %q, want %q", local, want)
	}
	if cfg.KeyPath != "/host/key" {
		t.Errorf("KeyPath = %q, want /host/key (per-host overlay)", cfg.KeyPath)
	}
}

func TestResolveArgUnknownHost(t *testing.T) {
	_, _, _, err := resolveArg("nope", config.File{})
	if err == nil || !strings.Contains(err.Error(), "no saved host") {
		t.Fatalf("err = %v, want a no-saved-host error", err)
	}
}

func TestUnknownAliasExitsOne(t *testing.T) {
	code, _, errOut := runArgs(t, "nope")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "no saved host") {
		t.Errorf("expected a no-saved-host error, got:\n%s", errOut)
	}
}

func TestTransferBadJSON(t *testing.T) {
	code, _, errOut := runArgs(t, "--transfer", "{not json}")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "transfer spec") {
		t.Errorf("expected a parse error, got:\n%s", errOut)
	}
}

func TestTransferUnknownHost(t *testing.T) {
	// A well-formed spec naming a host that isn't saved fails before any
	// connection attempt.
	code, _, errOut := runArgs(t, "--transfer", `{"name":"nope","type":"remote_to_local","object":"x"}`)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "no saved host") {
		t.Errorf("expected a no-saved-host error, got:\n%s", errOut)
	}
}

func TestHelpMentionsTransfer(t *testing.T) {
	_, _, errOut := runArgs(t, "--help")
	if !strings.Contains(errOut, "--transfer") {
		t.Errorf("help output missing --transfer:\n%s", errOut)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:                  "0 B",
		512:                "512 B",
		1024:               "1.0 KiB",
		1536:               "1.5 KiB",
		1024 * 1024:        "1.0 MiB",
		3 * 1024 * 1024:    "3.0 MiB",
		1024 * 1024 * 1024: "1.0 GiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSavedHostFlagOverride(t *testing.T) {
	// --port beats the saved host's port. resolveArg sets target.Port; mergeSettings
	// then lets the explicit flag win.
	fileCfg := config.File{Hosts: map[string]config.Host{
		"prod": {User: "admin", Host: "h", Port: 2222},
	}}
	target, _, cfg, err := resolveArg("prod", fileCfg)
	if err != nil {
		t.Fatalf("resolveArg: %v", err)
	}
	_, merged, _, err := mergeSettings(target, cfg, cliFlags{port: 22, set: map[string]bool{"port": true}})
	if err != nil {
		t.Fatalf("mergeSettings: %v", err)
	}
	if merged.Port != 22 {
		t.Errorf("merged port = %d, want 22 (--port over saved host)", merged.Port)
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
