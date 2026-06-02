package main

import (
	"bytes"
	"strings"
	"testing"
)

// runArgs invokes run with captured output and returns (code, stdout, stderr).
func runArgs(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestVersionFlag(t *testing.T) {
	code, out, _ := runArgs("--version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := "trawl " + version + "\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestHelpFlagExitsZero(t *testing.T) {
	code, _, errOut := runArgs("--help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(errOut, "usage: trawl") {
		t.Errorf("help output missing usage line:\n%s", errOut)
	}
}

func TestNoArgsIsUsageError(t *testing.T) {
	code, _, errOut := runArgs()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "usage: trawl") {
		t.Errorf("expected usage text on stderr, got:\n%s", errOut)
	}
}

func TestBareHostWithoutUserIsRejected(t *testing.T) {
	// "bogus" parses as a bare host but has no user, which trawl requires.
	code, _, errOut := runArgs("bogus")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "user@host") {
		t.Errorf("expected an error mentioning the expected format, got:\n%s", errOut)
	}
}

func TestMalformedTargetIsRejected(t *testing.T) {
	code, _, errOut := runArgs("me@host:notaport:/path")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "usage: trawl") {
		t.Errorf("expected usage text on stderr, got:\n%s", errOut)
	}
}

func TestTooManyArgs(t *testing.T) {
	code, _, errOut := runArgs("me@a", "me@b")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "exactly one target") {
		t.Errorf("expected too-many-args error, got:\n%s", errOut)
	}
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	code, _, _ := runArgs("--bogus-flag")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}
