// Package scan walks a directory tree and builds an in-memory size tree.
//
// Directories are read in parallel by a pool of workers. On Windows each
// directory is read in large batches with GetFileInformationByHandleEx,
// which returns both the logical and the on-disk (allocated) size of every
// entry. Junctions and symlinks are never followed, and hardlinked files use
// their disk space only once.
package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Node is one file or directory in the scanned tree.
type Node struct {
	Name     string
	parent   *Node
	IsDir    bool
	Size     int64 // logical bytes, including all descendants
	Alloc    int64 // bytes used on disk, including all descendants
	Files    int64 // number of files below (or 1 for a file)
	Dirs     int64 // number of directories below, not counting itself
	Link     bool  // junction or symlink: shown, never followed
	Hardlink bool  // another path to a file already counted on disk
	Err      string
	Children []*Node // sorted by Alloc, largest first

	fileID uint64 // volume file ID, used to count hardlinks once
}

// Path returns the full path of the node.
func (n *Node) Path() string {
	if n.parent == nil {
		return n.Name
	}
	return filepath.Join(n.parent.Path(), n.Name)
}

// Progress is a running total reported while a scan is in progress.
type Progress struct {
	Files  int64
	Dirs   int64
	Bytes  int64 // on-disk bytes seen so far
	Errors int64
	Path   string // a directory currently being read
}

// Options tune a scan. The zero value is ready to use.
type Options struct {
	// OnProgress, if set, is called periodically from a single goroutine
	// and once more with the final totals before Scan returns.
	OnProgress func(Progress)
	// Workers is the number of directories read in parallel.
	// Zero means DefaultWorkers().
	Workers int
}

// progressInterval is how often OnProgress is called during a scan.
const progressInterval = 100 * time.Millisecond

// DefaultWorkers is the worker count used when Options.Workers is zero.
func DefaultWorkers() int {
	return min(max(runtime.NumCPU()*2, 4), 32)
}

// entry is one item returned by readDir.
type entry struct {
	name       string
	isDir      bool
	size       int64
	alloc      int64
	fileID     uint64
	reparseTag uint32 // non-zero for reparse points
}

// Scan walks root and returns its size tree. Unreadable directories are
// recorded in Node.Err and do not stop the scan.
func Scan(ctx context.Context, root string, opts Options) (*Node, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = DefaultWorkers()
	}

	s := &scanner{ctx: ctx}
	s.cond = sync.NewCond(&s.mu)
	stop := context.AfterFunc(ctx, s.abort)
	defer stop()

	rootNode := &Node{Name: root, IsDir: true}
	s.push(rootNode)

	stopProgress := s.startProgress(opts.OnProgress)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.work()
		}()
	}
	wg.Wait()
	stopProgress()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	finalize(rootNode)
	if opts.OnProgress != nil {
		opts.OnProgress(Progress{
			Files: rootNode.Files, Dirs: rootNode.Dirs, Bytes: rootNode.Alloc,
			Errors: s.errors.Load(), Path: root,
		})
	}
	return rootNode, nil
}

// scanner is the shared state of one scan: a LIFO work queue of directories
// still to read, plus progress counters.
type scanner struct {
	ctx context.Context

	mu      sync.Mutex
	cond    *sync.Cond
	queue   []*Node
	pending int // queued + being read; the scan is done when it reaches 0
	aborted bool

	files, dirs, bytes, errors atomic.Int64
	current                    atomic.Pointer[Node]
}

func (s *scanner) push(n *Node) {
	s.mu.Lock()
	s.queue = append(s.queue, n)
	s.pending++
	s.mu.Unlock()
	s.cond.Signal()
}

// pop returns the next directory to read, or nil when the scan is over.
func (s *scanner) pop() *Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.queue) == 0 && s.pending > 0 && !s.aborted {
		s.cond.Wait()
	}
	if s.aborted || len(s.queue) == 0 {
		return nil
	}
	n := s.queue[len(s.queue)-1]
	s.queue = s.queue[:len(s.queue)-1]
	return n
}

func (s *scanner) done() {
	s.mu.Lock()
	s.pending--
	last := s.pending == 0
	s.mu.Unlock()
	if last {
		s.cond.Broadcast()
	}
}

func (s *scanner) abort() {
	s.mu.Lock()
	s.aborted = true
	s.mu.Unlock()
	s.cond.Broadcast()
}

func (s *scanner) work() {
	r := newDirReader()
	for {
		n := s.pop()
		if n == nil {
			return
		}
		s.readInto(r, n)
		s.done()
	}
}

// readInto reads directory n and attaches its entries. Subdirectories are
// queued for other workers. Only the worker holding n writes to it.
func (s *scanner) readInto(r *dirReader, n *Node) {
	s.current.Store(n)
	entries, err := r.readDir(n.Path())
	if err != nil {
		n.Err = err.Error()
		s.errors.Add(1)
		// Entries read before the error are still used.
	}

	n.Children = make([]*Node, 0, len(entries))
	for _, e := range entries {
		c := &Node{Name: e.name, parent: n, IsDir: e.isDir}
		n.Children = append(n.Children, c)

		switch {
		case e.reparseTag != 0 && isNameSurrogate(e.reparseTag):
			// Junction or symlink: it points at data counted elsewhere.
			c.Link = true
			if !e.isDir {
				c.Files = 1
				s.files.Add(1)
			}
		case e.isDir:
			s.dirs.Add(1)
			s.push(c)
		default:
			c.Size, c.Alloc, c.Files, c.fileID = e.size, e.alloc, 1, e.fileID
			s.files.Add(1)
			s.bytes.Add(e.alloc)
		}
	}
}

// isNameSurrogate reports whether a reparse tag makes the entry an alias for
// another location (junction, symlink, ...). Such entries are not followed.
// Other reparse points, such as OneDrive cloud folders, are normal data.
func isNameSurrogate(tag uint32) bool {
	return tag&0x20000000 != 0
}

func (s *scanner) startProgress(onProgress func(Progress)) (stop func()) {
	if onProgress == nil {
		return func() {}
	}
	quit := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(progressInterval)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-t.C:
				p := Progress{
					Files: s.files.Load(), Dirs: s.dirs.Load(),
					Bytes: s.bytes.Load(), Errors: s.errors.Load(),
				}
				if cur := s.current.Load(); cur != nil {
					p.Path = cur.Path()
				}
				onProgress(p)
			}
		}
	}()
	return func() {
		close(quit)
		<-finished
	}
}

// finalize runs single-threaded after all workers are done. It visits the
// tree in name order so the first path of a hardlinked file always keeps
// its on-disk size, then totals and sorts every directory.
func finalize(root *Node) {
	seen := make(map[uint64]struct{})
	var walk func(n *Node)
	walk = func(n *Node) {
		sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].Name < n.Children[j].Name })
		for _, c := range n.Children {
			if c.IsDir && !c.Link {
				walk(c)
				n.Dirs += c.Dirs + 1
			} else if c.IsDir {
				n.Dirs++
			} else if c.fileID != 0 && c.Alloc > 0 {
				if _, dup := seen[c.fileID]; dup {
					c.Alloc = 0
					c.Hardlink = true
				} else {
					seen[c.fileID] = struct{}{}
				}
			}
			n.Size += c.Size
			n.Alloc += c.Alloc
			n.Files += c.Files
		}
		sort.SliceStable(n.Children, func(i, j int) bool {
			a, b := n.Children[i], n.Children[j]
			if a.Alloc != b.Alloc {
				return a.Alloc > b.Alloc
			}
			return a.Size > b.Size
		})
	}
	walk(root)
}

// Find returns the node at path inside this tree, or nil if absent.
// Matching is case-insensitive, as paths are on Windows.
func (n *Node) Find(path string) *Node {
	rel, err := filepath.Rel(n.Path(), filepath.Clean(path))
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
