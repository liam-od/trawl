// Package tree builds a recursive, JSON-serialisable view of a directory and
// its subdirectories over an fs.FS, for the headless --list command. It is kept
// separate from package fs so the JSON shape stays out of the minimal
// filesystem abstraction.
package tree

import (
	"fmt"

	"github.com/liam-od/trawl/internal/fs"
)

// Node is one entry in the tree. Children is non-nil only for directories that
// were walked; a directory truncated by the depth limit carries no Children, so
// a consumer can tell "empty dir" (empty slice) from "not descended" (absent).
type Node struct {
	Name     string `json:"name"`
	Size     int64  `json:"size,omitempty"`
	IsDir    bool   `json:"is_dir"`
	Children []Node `json:"children,omitempty"`
}

// Build walks root on fsys and returns the nodes directly under it, recursing
// into subdirectories. maxDepth caps how many levels below root are descended
// (root's direct entries are level 1); maxDepth <= 0 means no limit. Entries
// arrive already sorted directories-first then alphabetically from fs.ReadDir,
// so node order needs no further work.
func Build(fsys fs.FS, root string, maxDepth int) ([]Node, error) {
	return build(fsys, root, maxDepth, 1)
}

// build returns the nodes for the entries directly under dir, recursing into
// subdirectories until depth would exceed maxDepth.
func build(fsys fs.FS, dir string, maxDepth, depth int) ([]Node, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	nodes := make([]Node, 0, len(entries))
	for _, e := range entries {
		n := Node{Name: e.Name, IsDir: e.IsDir}
		if e.IsDir {
			if maxDepth <= 0 || depth < maxDepth {
				kids, err := build(fsys, fsys.Join(dir, e.Name), maxDepth, depth+1)
				if err != nil {
					return nil, err
				}
				n.Children = kids
			}
		} else {
			n.Size = e.Size
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}
