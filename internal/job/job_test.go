package job

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Spec
		wantErr bool
	}{
		{
			name:  "minimal download",
			input: `{"name":"box","type":"remote_to_local","object":"film.mkv"}`,
			want:  Spec{Name: "box", Type: RemoteToLocal, Object: "film.mkv"},
		},
		{
			name:  "upload with overrides",
			input: `{"name":"box","type":"local_to_remote","object":"a.iso","remote_path":"/srv/in","local_path":"/data"}`,
			want:  Spec{Name: "box", Type: LocalToRemote, Object: "a.iso", RemotePath: "/srv/in", LocalPath: "/data"},
		},
		{
			name:  "object omitted is allowed",
			input: `{"name":"box","type":"remote_to_local"}`,
			want:  Spec{Name: "box", Type: RemoteToLocal},
		},
		{name: "not json", input: `not json`, wantErr: true},
		{name: "trailing data", input: `{"name":"box","type":"remote_to_local"} {}`, wantErr: true},
		{name: "unknown field", input: `{"name":"box","type":"remote_to_local","oops":1}`, wantErr: true},
		{name: "missing name", input: `{"type":"remote_to_local"}`, wantErr: true},
		{name: "blank name", input: `{"name":"","type":"remote_to_local"}`, wantErr: true},
		{name: "missing type", input: `{"name":"box"}`, wantErr: true},
		{name: "unknown type", input: `{"name":"box","type":"sideways"}`, wantErr: true},
		{name: "absolute object", input: `{"name":"box","type":"remote_to_local","object":"/etc/passwd"}`, wantErr: true},
		{name: "parent escape object", input: `{"name":"box","type":"remote_to_local","object":"../secret"}`, wantErr: true},
		{name: "nested parent escape", input: `{"name":"box","type":"remote_to_local","object":"a/../../b"}`, wantErr: true},
		{
			name:  "nested object is fine",
			input: `{"name":"box","type":"remote_to_local","object":"sub/dir/film.mkv"}`,
			want:  Spec{Name: "box", Type: RemoteToLocal, Object: "sub/dir/film.mkv"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %+v, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	const remoteBase = "/home/u/downloads/rtorrent"
	const localBase = "/home/liam/Inbox"

	tests := []struct {
		name       string
		spec       Spec
		wantRemote string
		wantLocal  string
		wantSrcRem bool
	}{
		{
			name:       "download named file",
			spec:       Spec{Type: RemoteToLocal, Object: "film.mkv"},
			wantRemote: "/home/u/downloads/rtorrent/film.mkv",
			wantLocal:  "/home/liam/Inbox/film.mkv",
			wantSrcRem: true,
		},
		{
			name:       "download nested object keeps basename at dest",
			spec:       Spec{Type: RemoteToLocal, Object: "sub/film.mkv"},
			wantRemote: "/home/u/downloads/rtorrent/sub/film.mkv",
			wantLocal:  "/home/liam/Inbox/film.mkv",
			wantSrcRem: true,
		},
		{
			name:       "download whole base dir",
			spec:       Spec{Type: RemoteToLocal},
			wantRemote: "/home/u/downloads/rtorrent",
			wantLocal:  "/home/liam/Inbox/rtorrent",
			wantSrcRem: true,
		},
		{
			name:       "upload named file",
			spec:       Spec{Type: LocalToRemote, Object: "a.iso"},
			wantRemote: "/home/u/downloads/rtorrent/a.iso",
			wantLocal:  "/home/liam/Inbox/a.iso",
			wantSrcRem: false,
		},
		{
			name:       "upload whole base dir",
			spec:       Spec{Type: LocalToRemote},
			wantRemote: "/home/u/downloads/rtorrent/Inbox",
			wantLocal:  "/home/liam/Inbox",
			wantSrcRem: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRemote, gotLocal, gotSrcRem := tt.spec.Resolve(remoteBase, localBase)
			if gotRemote != tt.wantRemote || gotLocal != tt.wantLocal || gotSrcRem != tt.wantSrcRem {
				t.Fatalf("Resolve() = (%q, %q, %v), want (%q, %q, %v)",
					gotRemote, gotLocal, gotSrcRem, tt.wantRemote, tt.wantLocal, tt.wantSrcRem)
			}
		})
	}
}
