// Package store keeps scan history in SQLite so changes between scans can
// be shown. A snapshot stores a summary of the tree, not every file: the
// entries that are at least a minimum size, down to a maximum depth.
package store

import (
	"path/filepath"
	"strings"

	"cysta-dyska/internal/scan"
)

// Row is one stored entry of a snapshot.
type Row struct {
	Key   string // lower-case path relative to the scan root; "" is the root
	Path  string // the same path with its original case, for display
	Depth int    // 0 for the root
	IsDir bool
	Alloc int64
	Size  int64
	Files int64
}

// Select picks the entries of tree to store. A folder is never smaller than
// anything inside it, so the selected rows always form a connected tree.
func Select(tree *scan.Node, minAlloc int64, maxDepth int) []Row {
	rows := []Row{{Key: "", Path: "", IsDir: true, Alloc: tree.Alloc, Size: tree.Size, Files: tree.Files}}
	var walk func(n *scan.Node, rel string, depth int)
	walk = func(n *scan.Node, rel string, depth int) {
		if depth > maxDepth {
			return
		}
		for _, c := range n.Children { // sorted largest first
			if c.Alloc < minAlloc {
				break
			}
			p := c.Name
			if rel != "" {
				p = rel + `\` + c.Name
			}
			rows = append(rows, Row{
				Key: strings.ToLower(p), Path: p, Depth: depth, IsDir: c.IsDir,
				Alloc: c.Alloc, Size: c.Size, Files: c.Files,
			})
			if c.IsDir && !c.Link {
				walk(c, p, depth+1)
			}
		}
	}
	walk(tree, "", 1)
	return rows
}

// parentKey returns the key of the folder containing key.
func parentKey(key string) string {
	if i := strings.LastIndexByte(key, filepath.Separator); i >= 0 {
		return key[:i]
	}
	return ""
}
