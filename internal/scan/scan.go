// Package scan walks a directory tree and builds an in-memory size tree.
//
// M1 scanner: a straightforward sequential walk using logical file sizes.
// It never follows symlinks or junctions. M2 replaces the walk with a
// parallel FindFirstFileEx walker that reports allocated size and
// deduplicates hardlinks.
package scan

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Node is one file or directory in the scanned tree.
type Node struct {
	Name     string
	Path     string
	IsDir    bool
	Size     int64 // logical bytes, including all descendants
	Files    int64 // number of files below (or 1 for a file)
	Dirs     int64 // number of directories below, not counting itself
	Err      string
	Children []*Node // sorted by Size, largest first
}

// Progress is a running total reported while a scan is in progress.
type Progress struct {
	Files  int64
	Dirs   int64
	Bytes  int64
	Errors int64
	Path   string // directory currently being read
}

// progressEvery is how many entries are processed between progress reports.
const progressEvery = 2000

type walker struct {
	ctx      context.Context
	onProg   func(Progress)
	prog     Progress
	sinceRep int
}

// Scan walks root and returns its size tree. onProgress may be nil; when set
// it is called periodically and once more with the final totals.
// Unreadable directories are recorded in Node.Err and do not stop the scan.
func Scan(ctx context.Context, root string, onProgress func(Progress)) (*Node, error) {
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	w := &walker{ctx: ctx, onProg: onProgress}
	node := &Node{Name: root, Path: root, IsDir: true}
	if err := w.walkDir(node); err != nil {
		return nil, err
	}
	w.report()
	return node, nil
}

func (w *walker) walkDir(n *Node) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	w.prog.Path = n.Path

	entries, err := os.ReadDir(n.Path)
	if err != nil {
		n.Err = err.Error()
		w.prog.Errors++
		// ReadDir may still return the entries it read before failing.
	}

	for _, e := range entries {
		// Symlinks and junctions (reported as irregular) are skipped so a
		// target is never counted twice or walked in a loop.
		if e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			continue
		}
		child := &Node{Name: e.Name(), Path: filepath.Join(n.Path, e.Name())}

		if e.IsDir() {
			child.IsDir = true
			w.prog.Dirs++
			if err := w.walkDir(child); err != nil {
				return err
			}
			n.Dirs += child.Dirs + 1
		} else {
			info, err := e.Info()
			if err != nil {
				child.Err = err.Error()
				w.prog.Errors++
			} else {
				child.Size = info.Size()
			}
			child.Files = 1
			w.prog.Files++
			w.prog.Bytes += child.Size
		}

		n.Size += child.Size
		n.Files += child.Files
		n.Children = append(n.Children, child)

		w.sinceRep++
		if w.sinceRep >= progressEvery {
			w.report()
		}
	}

	sort.Slice(n.Children, func(i, j int) bool {
		if n.Children[i].Size != n.Children[j].Size {
			return n.Children[i].Size > n.Children[j].Size
		}
		return n.Children[i].Name < n.Children[j].Name
	})
	return nil
}

func (w *walker) report() {
	w.sinceRep = 0
	if w.onProg != nil {
		w.onProg(w.prog)
	}
}

// Find returns the node at path inside this tree, or nil if absent.
// Matching is case-insensitive, as paths are on Windows.
func (n *Node) Find(path string) *Node {
	rel, err := filepath.Rel(n.Path, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	if rel == "." {
		return n
	}
	cur := n
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		var next *Node
		for _, c := range cur.Children {
			if strings.EqualFold(c.Name, part) {
				next = c
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}
