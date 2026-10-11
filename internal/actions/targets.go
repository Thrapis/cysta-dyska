package actions

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"cysta-dyska/internal/attribute"
	"cysta-dyska/internal/scan"
)

// maxShellPath is the longest path the Recycle Bin API (SHFileOperation)
// accepts; it does not support the \\?\ long-path prefix.
const maxShellPath = 259

// Targets lists the paths a file action removes.
func Targets(path string, mode attribute.FileAction) ([]string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	switch {
	case mode == attribute.FileActionNone:
		return nil, errors.New("this finding has no file action")
	case mode == attribute.FileActionFolder || !info.IsDir():
		return []string{filepath.Clean(path)}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, filepath.Join(path, e.Name()))
	}
	return out, nil
}

// Measurement is a fresh look at targets right before acting on them.
type Measurement struct {
	Bytes      int64 // on disk
	Files      int64
	Links      []string // junctions and symlinks inside (or as) the targets
	Unreadable int      // folders that could not be read
	LongPath   string   // first path too long for the Recycle Bin, if any
}

// Blocked returns why no file action may run on these targets, or "".
func (m Measurement) Blocked() string {
	if len(m.Links) > 0 {
		return fmt.Sprintf("contains a junction or symbolic link (%s); remove those by hand first", m.Links[0])
	}
	return ""
}

// Measure scans the targets again. It never follows links.
func Measure(ctx context.Context, targets []string) (Measurement, error) {
	var m Measurement
	for _, t := range targets {
		info, err := os.Lstat(t)
		if err != nil {
			return m, err
		}
		if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			m.Links = append(m.Links, t)
			continue
		}
		if !info.IsDir() {
			m.Files++
			m.Bytes += info.Size()
			if len(t) > maxShellPath && m.LongPath == "" {
				m.LongPath = t
			}
			continue
		}
		tree, err := scan.Scan(ctx, t, scan.Options{})
		if err != nil {
			return m, err
		}
		m.Bytes += tree.Alloc
		m.Files += tree.Files
		var walk func(n *scan.Node, path string)
		walk = func(n *scan.Node, path string) {
			if len(path) > maxShellPath && m.LongPath == "" {
				m.LongPath = path
			}
			if n.Link {
				m.Links = append(m.Links, path)
			}
			if n.Err != "" {
				m.Unreadable++
			}
			for _, c := range n.Children {
				walk(c, path+`\`+c.Name)
			}
		}
		walk(tree, tree.Path())
	}
	return m, nil
}
