package tree_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liam-od/trawl/internal/fs"
	"github.com/liam-od/trawl/internal/tree"
)

// fixture lays out, under a fresh temp dir:
//
//	a/            (dir)
//	  deep/       (dir)
//	    leaf.txt  (3 bytes)
//	  b.txt       (2 bytes)
//	top.txt       (5 bytes)
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "a", "deep"))
	mustWrite(t, filepath.Join(root, "a", "deep", "leaf.txt"), "xyz")
	mustWrite(t, filepath.Join(root, "a", "b.txt"), "yy")
	mustWrite(t, filepath.Join(root, "top.txt"), "hello")
	return root
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, data string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildFullTree(t *testing.T) {
	nodes, err := tree.Build(fs.NewLocal(), fixture(t), 0)
	if err != nil {
		t.Fatal(err)
	}

	// Directories first ("a"), then files ("top.txt").
	if len(nodes) != 2 {
		t.Fatalf("top level = %d nodes, want 2: %+v", len(nodes), nodes)
	}
	if nodes[0].Name != "a" || !nodes[0].IsDir {
		t.Fatalf("nodes[0] = %+v, want dir \"a\" first", nodes[0])
	}
	if nodes[1].Name != "top.txt" || nodes[1].IsDir || nodes[1].Size != 5 {
		t.Fatalf("nodes[1] = %+v, want file \"top.txt\" size 5", nodes[1])
	}

	// "a" holds "deep" (dir) before "b.txt" (file).
	a := nodes[0].Children
	if len(a) != 2 || a[0].Name != "deep" || !a[0].IsDir || a[1].Name != "b.txt" || a[1].Size != 2 {
		t.Fatalf("a children = %+v, want [deep(dir), b.txt(2)]", a)
	}

	// Recursed all the way to leaf.txt.
	deep := a[0].Children
	if len(deep) != 1 || deep[0].Name != "leaf.txt" || deep[0].Size != 3 {
		t.Fatalf("deep children = %+v, want [leaf.txt(3)]", deep)
	}
}

func TestBuildDepthTruncates(t *testing.T) {
	nodes, err := tree.Build(fs.NewLocal(), fixture(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Name != "a" {
		t.Fatalf("top level = %+v, want [a, top.txt]", nodes)
	}
	// At depth 1 the dir "a" is listed but not descended.
	if nodes[0].Children != nil {
		t.Fatalf("a.Children = %+v, want nil (truncated at depth 1)", nodes[0].Children)
	}
}

func TestBuildMissingRoot(t *testing.T) {
	if _, err := tree.Build(fs.NewLocal(), filepath.Join(t.TempDir(), "nope"), 0); err == nil {
		t.Fatal("Build on a missing root: want error, got nil")
	}
}
