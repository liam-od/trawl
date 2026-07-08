package fs

import "strings"

// winIllegalChars are the characters Windows rejects in file names, in addition
// to the control range 0x00–0x1f.
const winIllegalChars = `<>:"/\|?*`

// winReserved holds the device names Windows refuses as a file's stem (the part
// before the first dot), compared case-insensitively: "CON" and "con.txt" are
// both rejected.
var winReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// cleanWindowsName rewrites name into one Windows accepts: illegal characters
// become '_', trailing dots and spaces are trimmed (Windows strips them
// silently, so a name keeping them could never be re-opened by the name it was
// written under), and a reserved device stem gets '_' appended. It lives in an
// unconstrained file, rather than with its //go:build windows caller, so it can
// be unit-tested on every platform.
func cleanWindowsName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r < 0x20 || strings.ContainsRune(winIllegalChars, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(b.String(), ". ")
	if out == "" {
		return "_"
	}
	stem := out
	if i := strings.IndexByte(out, '.'); i >= 0 {
		stem = out[:i]
	}
	// A stem with a trailing space before the extension ("COM1 .log") is still
	// canonicalised to the device name by Windows, so match on the trimmed stem.
	if winReserved[strings.ToUpper(strings.TrimRight(stem, " "))] {
		out = stem + "_" + out[len(stem):]
	}
	return out
}
