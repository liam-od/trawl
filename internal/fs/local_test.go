package fs

import "testing"

func TestLocalFS(t *testing.T) {
	fsMatrix(t, NewLocal(), t.TempDir())
}

func TestLocalFS_Join(t *testing.T) {
	got := NewLocal().Join("/a", "b", "c")
	want := "/a/b/c" // OS-native; on the test platform (linux) the separator is '/'.
	if got != want {
		t.Errorf("Join: got %q, want %q", got, want)
	}
}
