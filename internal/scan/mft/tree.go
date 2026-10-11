package mft

import (
	"errors"
	"sort"
	"strings"
	"time"

	"cysta-dyska/internal/scan"
)

// rootRecord is the root directory's record number. Records below
// firstUserRecord are NTFS metadata files ($MFT, $LogFile, $Extend, ...),
// which a directory walk does not see either.
const (
	rootRecord      = 5
	firstUserRecord = 16
)

// Tree collects parsed records and turns them into a flat scan tree.
// Records are stored by number: the MFT is dense, so a slice is much
// cheaper than a map for millions of files.
type Tree struct {
	files []file
	Bad   int // records that could not be read (torn or corrupt)

	// Measurements of ReadVolume, for tuning.
	Records     int
	Bytes       int64
	Open        time.Duration
	Read, Parse time.Duration // summed over the reading workers
	Workers     int
}

type file struct {
	seq         uint16
	inUse, dir  bool
	reparseTag  uint32
	names       []Name
	size, alloc int64
	wofAlloc    int64
}

// NewTree returns an empty tree.
func NewTree() *Tree { return &Tree{} }

// grow makes room for record numbers below n. Not safe for concurrent use;
// ReadVolume grows the tree once before its workers start.
func (t *Tree) grow(n uint64) {
	if n > uint64(len(t.files)) {
		t.files = append(t.files, make([]file, n-uint64(len(t.files)))...)
	}
}

func (t *Tree) at(num uint64) *file {
	if num >= uint64(len(t.files)) {
		return nil
	}
	return &t.files[num]
}

// Add merges one record. Extension records add their names and data to
// their base record. Adding base records concurrently is safe when the tree
// already has room for them, since each touches only its own slot;
// extension records touch their base's slot and must be added alone.
func (t *Tree) Add(r Record) {
	num := r.Num
	if r.Base != 0 {
		num = r.Base
	}
	t.grow(num + 1)
	f := &t.files[num]
	if r.Base == 0 {
		f.inUse, f.seq, f.dir = true, r.Seq, r.Dir
	}
	f.names = append(f.names, r.Names...)
	if r.HasData {
		f.size, f.alloc = r.Size, r.Alloc
	}
	if r.ReparseTag != 0 {
		f.reparseTag = r.ReparseTag
	}
	if r.WofAlloc != 0 {
		f.wofAlloc = r.WofAlloc
	}
}

// isNameSurrogate matches scan's rule: junctions and symlinks are not
// followed; other reparse points (e.g. OneDrive) are normal data.
func isNameSurrogate(tag uint32) bool { return tag&0x20000000 != 0 }

type edge struct {
	parent, child uint32
	name          string
}

// Flat returns the tree below the root directory, named rootName (e.g.
// `C:\`), in the format scan.Build reads. Files whose parent is missing,
// freed or reused (sequence mismatch) are left out, as are metadata files.
func (t *Tree) Flat(rootName string) ([]scan.Flat, error) {
	root := t.at(rootRecord)
	if root == nil || !root.inUse || !root.dir {
		return nil, errors.New("root directory record is missing")
	}

	// Collect parent -> child edges, then group them by parent with a
	// counting sort (one pass, no per-folder allocations).
	var edges []edge
	counts := make([]int32, len(t.files)+1)
	for num := firstUserRecord; num < len(t.files); num++ {
		f := &t.files[num]
		if !f.inUse {
			continue
		}
		first := len(edges)
	names:
		for _, n := range f.names {
			if n.Namespace == namespaceDOS {
				continue // an 8.3 alias of a long name listed separately
			}
			p := t.at(n.Parent)
			if p == nil || !p.inUse || !p.dir || p.seq != n.ParentSeq {
				continue
			}
			for _, e := range edges[first:] { // a file has only a few names
				if uint64(e.parent) == n.Parent && strings.EqualFold(e.name, n.Name) {
					continue names
				}
			}
			edges = append(edges, edge{uint32(n.Parent), uint32(num), n.Name})
			counts[n.Parent+1]++
		}
	}
	for i := 1; i < len(counts); i++ {
		counts[i] += counts[i-1] // counts[p] is now where p's children start
	}
	grouped := make([]edge, len(edges))
	next := append([]int32(nil), counts[:len(counts)-1]...)
	for _, e := range edges {
		grouped[next[e.parent]] = e
		next[e.parent]++
	}

	out := make([]scan.Flat, 1, len(edges)+1)
	out[0] = scan.Flat{Parent: -1, Name: rootName, IsDir: true}
	// Iterative depth-first walk: the tree can be very deep.
	type item struct {
		num   uint32
		index int32
	}
	stack := []item{{rootRecord, 0}}
	// A folder is listed once even if a damaged table names it twice or
	// inside itself; otherwise the walk could duplicate data or never end.
	expanded := make([]bool, len(t.files))
	expanded[rootRecord] = true
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		kids := grouped[counts[it.num]:counts[it.num+1]]
		sort.Slice(kids, func(i, j int) bool { return kids[i].name < kids[j].name })
		for _, c := range kids {
			f := &t.files[c.child]
			if f.dir && expanded[c.child] {
				continue
			}
			link := f.reparseTag != 0 && isNameSurrogate(f.reparseTag)
			e := scan.Flat{Parent: it.index, Name: c.name, IsDir: f.dir, Link: link}
			if !f.dir {
				e.Size, e.Alloc, e.FileID = f.size, f.alloc, uint64(c.child)+1
				if f.reparseTag == ioReparseTagWOF {
					e.Alloc += f.wofAlloc // the compressed data
				}
			}
			out = append(out, e)
			if f.dir && !link {
				expanded[c.child] = true
				stack = append(stack, item{c.child, int32(len(out) - 1)})
			}
		}
	}
	return out, nil
}
