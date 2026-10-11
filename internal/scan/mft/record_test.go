package mft

import (
	"encoding/binary"
	"testing"
	"time"
	"unicode/utf16"

	"cysta-dyska/internal/scan"
)

const recSize = 1024

// rec builds a 1 KiB file record segment the way NTFS stores it, including
// the update sequence protection at the end of each 512-byte block.
type rec struct {
	seq   uint16
	dir   bool
	free  bool
	base  uint64
	attrs [][]byte
}

func (r rec) bytes() []byte {
	b := make([]byte, recSize)
	copy(b, "FILE")
	binary.LittleEndian.PutUint16(b[4:], 0x30) // update sequence array offset
	binary.LittleEndian.PutUint16(b[6:], 3)    // USN + one entry per 512 bytes
	binary.LittleEndian.PutUint16(b[0x10:], r.seq)
	binary.LittleEndian.PutUint16(b[0x14:], 0x38) // first attribute
	var flags uint16
	if !r.free {
		flags |= recordInUse
	}
	if r.dir {
		flags |= recordDirectory
	}
	binary.LittleEndian.PutUint16(b[0x16:], flags)
	binary.LittleEndian.PutUint64(b[0x20:], r.base)
	off := 0x38
	for _, a := range r.attrs {
		copy(b[off:], a)
		off += len(a)
	}
	binary.LittleEndian.PutUint32(b[off:], attrEnd)
	binary.LittleEndian.PutUint32(b[0x18:], uint32(off+8))
	// Protect the blocks: the real last two bytes go to the array, and the
	// sequence number takes their place.
	const usn = 0x0007
	binary.LittleEndian.PutUint16(b[0x30:], usn)
	for i := 1; i <= 2; i++ {
		end := i*512 - 2
		copy(b[0x30+2*i:], b[end:end+2])
		binary.LittleEndian.PutUint16(b[end:], usn)
	}
	return b
}

func pad8(n int) int { return (n + 7) &^ 7 }

func resident(typ uint32, value []byte, nameLen byte) []byte {
	a := make([]byte, pad8(0x18+len(value)))
	binary.LittleEndian.PutUint32(a, typ)
	binary.LittleEndian.PutUint32(a[4:], uint32(len(a)))
	a[9] = nameLen
	binary.LittleEndian.PutUint32(a[0x10:], uint32(len(value)))
	binary.LittleEndian.PutUint16(a[0x14:], 0x18)
	copy(a[0x18:], value)
	return a
}

func fileName(parent uint64, parentSeq uint16, name string, ns byte) []byte {
	u := utf16.Encode([]rune(name))
	v := make([]byte, 0x42+2*len(u))
	binary.LittleEndian.PutUint64(v, parent|uint64(parentSeq)<<48)
	v[0x40] = byte(len(u))
	v[0x41] = ns
	for i, c := range u {
		binary.LittleEndian.PutUint16(v[0x42+2*i:], c)
	}
	return resident(attrFileName, v, 0)
}

// data builds a nonresident unnamed $DATA attribute. total >= 0 adds the
// TotalAllocated field used by compressed and sparse files.
func data(lowestVCN, alloc, size, total int64, flags uint16, runs []byte) []byte {
	hdr := 0x40
	if total >= 0 {
		hdr = 0x48
	}
	a := make([]byte, pad8(hdr+len(runs)+1))
	binary.LittleEndian.PutUint32(a, attrData)
	binary.LittleEndian.PutUint32(a[4:], uint32(len(a)))
	a[8] = 1 // nonresident
	binary.LittleEndian.PutUint16(a[0x0C:], flags)
	binary.LittleEndian.PutUint64(a[0x10:], uint64(lowestVCN))
	binary.LittleEndian.PutUint16(a[0x20:], uint16(hdr))
	binary.LittleEndian.PutUint64(a[0x28:], uint64(alloc))
	binary.LittleEndian.PutUint64(a[0x30:], uint64(size))
	if total >= 0 {
		binary.LittleEndian.PutUint64(a[0x40:], uint64(total))
	}
	copy(a[hdr:], runs)
	return a
}

// named sets an attribute's name (stored after the header, as NTFS does).
func named(a []byte, name string) []byte {
	u := utf16.Encode([]rune(name))
	hdr := int(binary.LittleEndian.Uint16(a[0x20:])) // mapping pairs offset (nonresident)
	out := make([]byte, pad8(len(a)+2*len(u)))
	copy(out, a[:hdr])
	for i, c := range u {
		binary.LittleEndian.PutUint16(out[hdr+2*i:], c)
	}
	copy(out[hdr+2*len(u):], a[hdr:])
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)))
	out[9] = byte(len(u))
	binary.LittleEndian.PutUint16(out[0x0A:], uint16(hdr))
	binary.LittleEndian.PutUint16(out[0x20:], uint16(hdr+2*len(u)))
	return out
}

func reparse(tag uint32) []byte {
	v := make([]byte, 8)
	binary.LittleEndian.PutUint32(v, tag)
	return resident(attrReparsePoint, v, 0)
}

func parse(t *testing.T, r rec, num uint64) Record {
	t.Helper()
	got, err := Parse(r.bytes(), num, true)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return got
}

// Proves: a plain file record yields its name, parent, sizes and run list,
// and the update sequence fixups restore bytes that sit at block ends.
func TestParseFile(t *testing.T) {
	// One run: 0x10 clusters at LCN 0x2000 (header byte 0x21: 1 length byte, 2 LCN bytes).
	r := parse(t, rec{seq: 3, attrs: [][]byte{
		fileName(rootRecord, 5, "report.pdf", 1),
		data(0, 65536, 60000, -1, 0, []byte{0x21, 0x10, 0x00, 0x20}),
	}}, 40)
	if r.Seq != 3 || r.Dir || r.Base != 0 {
		t.Errorf("header = %+v", r)
	}
	if len(r.Names) != 1 || r.Names[0] != (Name{Parent: rootRecord, ParentSeq: 5, Name: "report.pdf", Namespace: 1}) {
		t.Errorf("names = %+v", r.Names)
	}
	if !r.HasData || r.Size != 60000 || r.Alloc != 65536 {
		t.Errorf("sizes = %d/%d (has %v)", r.Size, r.Alloc, r.HasData)
	}
	if len(r.Runs) != 1 || r.Runs[0] != (Run{VCN: 0, LCN: 0x2000, Length: 0x10}) {
		t.Errorf("runs = %+v", r.Runs)
	}
}

// Proves: compressed and sparse files report the clusters really used
// (TotalAllocated), like the directory walker's AllocationSize does.
func TestParseCompressedAndSparse(t *testing.T) {
	for name, flags := range map[string]uint16{"compressed": 0x0001, "sparse": attrFlagSparse} {
		r := parse(t, rec{attrs: [][]byte{data(0, 1<<20, 1<<20, 4096, flags, []byte{0x01, 0x10})}}, 41)
		if r.Alloc != 4096 {
			t.Errorf("%s: alloc = %d, want 4096", name, r.Alloc)
		}
	}
}

// Proves: resident data counts its size rounded to 8 bytes; named streams
// (alternate data streams) and $DATA continuations (LowestVcn > 0) do not
// set the file's size.
func TestParseDataVariants(t *testing.T) {
	r := parse(t, rec{attrs: [][]byte{resident(attrData, make([]byte, 13), 0)}}, 42)
	if !r.HasData || r.Size != 13 || r.Alloc != 16 {
		t.Errorf("resident: %d/%d", r.Size, r.Alloc)
	}
	named := resident(attrData, make([]byte, 100), 4)
	r = parse(t, rec{attrs: [][]byte{named}}, 43)
	if r.HasData {
		t.Error("a named stream set the file size")
	}
	r = parse(t, rec{base: 44, attrs: [][]byte{data(16, 999, 999, -1, 0, []byte{0x11, 0x10, 0x05})}}, 45)
	if r.HasData || r.Base != 44 {
		t.Errorf("continuation: %+v", r)
	}
}

// Proves: files compressed by Windows (WOF, e.g. CompactOS and the driver
// store) count the disk space of their WofCompressedData stream, where the
// data really is; their main stream is sparse and empty. Other named streams
// still do not count.
func TestWOFCompressedFile(t *testing.T) {
	tr := NewTree()
	tr.Add(parse(t, rec{seq: 5, dir: true}, rootRecord))
	tr.Add(parse(t, rec{seq: 1, attrs: [][]byte{
		fileName(rootRecord, 5, "driver.dll", 3),
		data(0, 1<<20, 1<<20, 0, attrFlagSparse, []byte{0x02, 0x00, 0x01}), // 256 sparse clusters
		reparse(ioReparseTagWOF),
		named(data(0, 12288, 12000, -1, 0, []byte{0x21, 0x03, 0x00, 0x10}), "WofCompressedData"),
		named(data(0, 8192, 8000, -1, 0, []byte{0x21, 0x02, 0x00, 0x20}), "Zone.Identifier"),
	}}, 60))
	flat, err := tr.Flat(`C:\`)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := scan.Build(flat)
	f := root.Find(`C:\driver.dll`)
	if f == nil || f.Size != 1<<20 || f.Alloc != 12288 {
		t.Errorf("WOF file = %+v, want size %d, alloc 12288", f, 1<<20)
	}
}

// Proves: damaged records are rejected instead of yielding garbage: a torn
// write (block end does not match the sequence number), a bad attribute
// length, and a missing signature; a free slot is reported as not in use.
func TestParseRejectsDamage(t *testing.T) {
	good := rec{attrs: [][]byte{fileName(rootRecord, 5, "a", 1)}}

	torn := good.bytes()
	torn[510] ^= 0xFF
	if _, err := Parse(torn, 50, false); err == nil {
		t.Error("torn record accepted")
	}
	badLen := good.bytes()
	binary.LittleEndian.PutUint32(badLen[0x38+4:], 4000)
	if _, err := Parse(badLen, 50, false); err == nil {
		t.Error("attribute running past the record accepted")
	}
	noSig := good.bytes()
	copy(noSig, "BAAD")
	if _, err := Parse(noSig, 50, false); err == nil {
		t.Error("record without FILE signature accepted")
	}
	free := rec{free: true}.bytes()
	if _, err := Parse(free, 50, false); err != ErrNotInUse {
		t.Errorf("free record: %v", err)
	}
	if _, err := Parse(make([]byte, recSize), 50, false); err != ErrNotInUse {
		t.Errorf("zeroed slot: %v", err)
	}
}

// Proves: mapping pairs decode with signed, relative LCNs and sparse runs
// (no LCN bytes), per the ATTRIBUTE_RECORD_HEADER remarks.
func TestDecodeRuns(t *testing.T) {
	runs := []byte{
		0x21, 0x08, 0x00, 0x01, // 8 clusters at LCN 0x100
		0x01, 0x04, // 4 sparse clusters
		0x11, 0x02, 0xF0, // 2 clusters at 0x100 - 0x10 = 0xF0
	}
	r := parse(t, rec{attrs: [][]byte{data(0, 14*4096, 14*4096, -1, 0, runs)}}, 46)
	want := []Run{{0, 0x100, 8}, {8, -1, 4}, {12, 0xF0, 2}}
	if len(r.Runs) != len(want) {
		t.Fatalf("runs = %+v", r.Runs)
	}
	for i := range want {
		if r.Runs[i] != want[i] {
			t.Errorf("run %d = %+v, want %+v", i, r.Runs[i], want[i])
		}
	}
}

// Proves: the tree matches what the directory walker would build: DOS 8.3
// aliases are dropped, hardlinks keep the disk space once (first path in name
// order), junctions are shown but not followed, extension records add their
// data to the base file, metadata files and orphans (parent freed or reused)
// are left out.
func TestTreeFlat(t *testing.T) {
	tr := NewTree()
	add := func(num uint64, r rec) { tr.Add(parse(t, r, num)) }

	add(rootRecord, rec{seq: 5, dir: true, attrs: [][]byte{fileName(rootRecord, 5, ".", 3)}})
	add(0, rec{seq: 1, attrs: [][]byte{fileName(rootRecord, 5, "$MFT", 3), data(0, 1<<30, 1<<30, -1, 0, []byte{0x01, 0x01})}})
	add(20, rec{seq: 1, dir: true, attrs: [][]byte{fileName(rootRecord, 5, "Users", 3)}})
	add(21, rec{seq: 2, attrs: [][]byte{
		fileName(20, 1, "LONGFI~1.TXT", namespaceDOS),
		fileName(20, 1, "long file name.txt", 1),
		resident(attrData, make([]byte, 100), 0),
	}})
	// Hardlink: one file, two names in different folders.
	add(22, rec{seq: 1, dir: true, attrs: [][]byte{fileName(rootRecord, 5, "b", 3)}})
	add(23, rec{seq: 1, attrs: [][]byte{
		fileName(22, 1, "copy.dll", 3),
		fileName(20, 1, "orig.dll", 3),
		data(0, 8192, 8000, -1, 0, []byte{0x11, 0x02, 0x10}),
	}})
	// Junction with something "inside" it that must not be listed.
	add(24, rec{seq: 1, dir: true, attrs: [][]byte{fileName(rootRecord, 5, "link", 3), reparse(0xA0000003)}})
	// A large file whose $DATA lives in an extension record.
	add(25, rec{seq: 1, attrs: [][]byte{fileName(20, 1, "big.vhdx", 3), resident(attrAttributeList, make([]byte, 8), 0)}})
	add(26, rec{seq: 1, base: 25, attrs: [][]byte{data(0, 1<<20, 1<<20, -1, 0, []byte{0x11, 0x10, 0x20})}})
	// Orphan: its parent's sequence number no longer matches.
	add(27, rec{seq: 1, attrs: [][]byte{fileName(20, 9, "stale.txt", 3), resident(attrData, make([]byte, 50), 0)}})

	flat, err := tr.Flat(`C:\`)
	if err != nil {
		t.Fatal(err)
	}
	root, err := scan.Build(flat)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]*scan.Node{}
	var walk func(n *scan.Node)
	walk = func(n *scan.Node) {
		paths[n.Path()] = n
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)

	for _, p := range []string{`C:\$MFT`, `C:\Users\LONGFI~1.TXT`, `C:\Users\stale.txt`} {
		if paths[p] != nil {
			t.Errorf("%s should not be listed", p)
		}
	}
	if n := paths[`C:\Users\long file name.txt`]; n == nil || n.Alloc != 104 {
		t.Errorf("long name missing or wrong size: %+v", n)
	}
	if n := paths[`C:\Users\big.vhdx`]; n == nil || n.Alloc != 1<<20 {
		t.Errorf("extension record data not merged: %+v", n)
	}
	orig, cp := paths[`C:\Users\orig.dll`], paths[`C:\b\copy.dll`]
	if orig == nil || cp == nil {
		t.Fatalf("hardlink names missing: %v %v", orig, cp)
	}
	// Name order: C:\Users < C:\b ('U' < 'b'), so C:\Users\orig.dll keeps the space.
	if orig.Alloc != 8192 || cp.Alloc != 0 || !cp.Hardlink {
		t.Errorf("hardlink: orig %d, copy %d (hardlink %v)", orig.Alloc, cp.Alloc, cp.Hardlink)
	}
	if l := paths[`C:\link`]; l == nil || !l.Link || len(l.Children) != 0 {
		t.Errorf("junction: %+v", l)
	}
	if want := int64(104 + 8192 + 1<<20); root.Alloc != want {
		t.Errorf("root alloc = %d, want %d", root.Alloc, want)
	}
}

// Proves: a damaged table that lists a folder inside its own subfolder does
// not make the walk loop forever or count the folder twice.
func TestTreeCycle(t *testing.T) {
	tr := NewTree()
	add := func(num uint64, r rec) { tr.Add(parse(t, r, num)) }
	add(rootRecord, rec{seq: 5, dir: true})
	add(30, rec{seq: 1, dir: true, attrs: [][]byte{fileName(rootRecord, 5, "a", 3), fileName(31, 1, "again", 3)}})
	add(31, rec{seq: 1, dir: true, attrs: [][]byte{fileName(30, 1, "b", 3)}})
	add(32, rec{seq: 1, attrs: [][]byte{fileName(31, 1, "f", 3), resident(attrData, make([]byte, 8), 0)}})

	var flat []scan.Flat
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		flat, err = tr.Flat(`C:\`)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Flat did not finish: the cycle is walked forever")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(flat) != 4 { // root, a, b, f
		t.Errorf("flat = %+v", flat)
	}
	root, err := scan.Build(flat)
	if err != nil || root.Alloc != 8 {
		t.Errorf("root = %+v, %v", root, err)
	}
}
