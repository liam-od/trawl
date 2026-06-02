package sshx

import "testing"

func TestParseTarget_HappyPaths(t *testing.T) {
	cases := []struct {
		in   string
		want Target
	}{
		{"u@h", Target{User: "u", Host: "h"}},
		{"u@h:22", Target{User: "u", Host: "h", Port: "22"}},
		{"u@h:/p", Target{User: "u", Host: "h", Path: "/p"}},
		{"u@h:22:/p", Target{User: "u", Host: "h", Port: "22", Path: "/p"}},
		{"h", Target{Host: "h"}},
		{"u@h:22:/path/with:colon", Target{User: "u", Host: "h", Port: "22", Path: "/path/with:colon"}},
		{"alice@example.com:2222:/srv/data", Target{User: "alice", Host: "example.com", Port: "2222", Path: "/srv/data"}},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseTarget(c.in)
			if err != nil {
				t.Fatalf("ParseTarget(%q) returned error: %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("ParseTarget(%q) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

func TestParseTarget_Malformed(t *testing.T) {
	cases := []string{
		"",
		"@h",
		"u@",
		"u@h:abc",
		"u@h:99999999",
		"u@h::",
		"u@h:22:nopath",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if _, err := ParseTarget(c); err == nil {
				t.Fatalf("ParseTarget(%q) returned no error; want error", c)
			}
		})
	}
}
