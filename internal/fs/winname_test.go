package fs

import "testing"

func TestCleanWindowsName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain.txt", "plain.txt"},
		{"no change needed (1.0.2)", "no change needed (1.0.2)"},
		{
			"Assassin's Creed: Black Flag Resynced - Deluxe Edition (1.0.2)",
			"Assassin's Creed_ Black Flag Resynced - Deluxe Edition (1.0.2)",
		},
		{`a<b>c:d"e/f\g|h?i*j`, "a_b_c_d_e_f_g_h_i_j"},
		{"tab\tand\x01ctl", "tab_and_ctl"},
		{"trailing dot.", "trailing dot"},
		{"trailing space ", "trailing space"},
		{"...", "_"},
		{"CON", "CON_"},
		{"con.txt", "con_.txt"},
		{"lpt9.log", "lpt9_.log"},
		{"Console", "Console"}, // reserved stem must match whole, not prefix
		{"NULl.txt", "NULl.txt"},
		{"COM1 .log", "COM1 _.log"}, // trailing space before ext still canonicalises to device
		{"CON .txt", "CON _.txt"},
	}
	for _, c := range cases {
		if got := cleanWindowsName(c.in); got != c.want {
			t.Errorf("cleanWindowsName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
