package job

import "testing"

func TestParseList(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ListSpec
		wantErr bool
	}{
		{
			name:  "minimal remote",
			input: `{"name":"box","side":"remote"}`,
			want:  ListSpec{Name: "box", Side: SideRemote},
		},
		{
			name:  "local with override and depth",
			input: `{"name":"box","side":"local","path":"/data/in","depth":2}`,
			want:  ListSpec{Name: "box", Side: SideLocal, Path: "/data/in", Depth: 2},
		},
		{
			name:  "tilde override allowed",
			input: `{"name":"box","side":"remote","path":"~/media"}`,
			want:  ListSpec{Name: "box", Side: SideRemote, Path: "~/media"},
		},
		{name: "not json", input: `not json`, wantErr: true},
		{name: "trailing data", input: `{"name":"box","side":"remote"} {}`, wantErr: true},
		{name: "unknown field", input: `{"name":"box","side":"remote","oops":1}`, wantErr: true},
		{name: "missing name", input: `{"side":"remote"}`, wantErr: true},
		{name: "blank name", input: `{"name":"","side":"remote"}`, wantErr: true},
		{name: "missing side", input: `{"name":"box"}`, wantErr: true},
		{name: "unknown side", input: `{"name":"box","side":"sideways"}`, wantErr: true},
		{name: "negative depth", input: `{"name":"box","side":"remote","depth":-1}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseList(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseList(%q) = %+v, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseList(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseList(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}
