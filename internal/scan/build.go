package scan

import (
	"errors"
	"fmt"
)

// Flat is one entry of a tree stored as a list, used to pass a scan made
// elsewhere (the MFT fast scan in an elevated helper) back to the app.
// Entry 0 is the root; every other entry names an earlier entry as parent.
type Flat struct {
	Parent int32
	Name   string
	IsDir  bool
	Link   bool
	Size   int64
	Alloc  int64
	FileID uint64 // same value for every name of a hardlinked file
}

// Build turns a flat list into a tree and totals it exactly as Scan does:
// hardlinked files use disk space once (the first path in name order), and
// children are sorted by on-disk size.
func Build(list []Flat) (*Node, error) {
	if len(list) == 0 || list[0].Parent != -1 || !list[0].IsDir {
		return nil, errors.New("flat tree has no root folder")
	}
	nodes := make([]*Node, len(list))
	for i, f := range list {
		n := &Node{Name: f.Name, IsDir: f.IsDir, Link: f.Link}
		switch {
		case f.Link && !f.IsDir:
			n.Files = 1 // a symlink file counts as a file but uses no space, as in Scan
		case !f.IsDir:
			n.Size, n.Alloc, n.Files, n.fileID = f.Size, f.Alloc, 1, f.FileID
		}
		nodes[i] = n
		if i == 0 {
			continue
		}
		p := int(f.Parent)
		if p < 0 || p >= i || !list[p].IsDir || list[p].Link {
			return nil, fmt.Errorf("entry %d (%s) has a bad parent %d", i, f.Name, p)
		}
		n.parent = nodes[p]
		nodes[p].Children = append(nodes[p].Children, n)
	}
	finalize(nodes[0])
	return nodes[0], nil
}
