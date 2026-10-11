package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cysta-dyska/internal/scan"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	// A space in the path checks the DSN handles real Windows folder names.
	s, err := Open(filepath.Join(t.TempDir(), "with space", "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { clock = clock.Add(time.Hour); return clock }
	return s
}

func testOpts() Options { return Options{MinAlloc: 50_000, MaxDepth: 5, Keep: 30} }

// Proves: a saved snapshot is listed for its root (newest first, case-
// insensitive root match) with its totals, and the database survives reopening.
func TestSaveAndList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tree := scanFixture(t, map[string]int{"a/big.bin": 200_000})
	id1, err := s.Save(tree, time.Second, MethodWalk, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.Save(tree, time.Second, MethodWalk, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, err := s.Snapshots(upper(tree.Path()), MethodWalk)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != id2 || list[1].ID != id1 {
		t.Fatalf("snapshots = %+v, want [%d %d]", list, id2, id1)
	}
	if list[0].Alloc != tree.Alloc || list[0].Files != tree.Files || list[0].Root != tree.Path() {
		t.Errorf("snapshot = %+v, tree alloc %d files %d", list[0], tree.Alloc, tree.Files)
	}
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// Proves: Compare lists one folder level from both snapshots, with grown,
// shrunk, new and removed entries, and marks entries that have children.
func TestCompareChildren(t *testing.T) {
	s := openTest(t)
	root := t.TempDir()
	write := func(name string, n int) {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("grows/sub/f.bin", 100_000)
	write("shrinks/f.bin", 300_000)
	write("goes/f.bin", 100_000)
	before := saveScan(t, s, root)

	write("grows/sub/g.bin", 400_000)
	write("shrinks/f.bin", 60_000)
	os.RemoveAll(filepath.Join(root, "goes"))
	write("new/f.bin", 200_000)
	after := saveScan(t, s, root)

	rows, err := s.Compare(before, after, "")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]DiffRow{}
	for _, r := range rows {
		by[r.Key] = r
	}
	g := by["grows"]
	if g.After-g.Before < 400_000 || !g.HasChildren || !g.BeforeKnown || !g.AfterKnown {
		t.Errorf("grows = %+v", g)
	}
	if sh := by["shrinks"]; sh.After >= sh.Before {
		t.Errorf("shrinks = %+v", sh)
	}
	if n := by["new"]; n.BeforeKnown || !n.AfterKnown || n.Before != 0 {
		t.Errorf("new = %+v", n)
	}
	if gone := by["goes"]; !gone.BeforeKnown || gone.AfterKnown || gone.After != 0 {
		t.Errorf("goes = %+v", gone)
	}
	if rows[0].Key != "grows" {
		t.Errorf("first row = %q, want largest change first", rows[0].Key)
	}

	sub, err := s.Compare(before, after, "grows")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 1 || sub[0].Key != `grows\sub` || sub[0].Path != `grows\sub` {
		t.Errorf("children of grows = %+v", sub)
	}
}

// Proves: Hotspots names where growth actually happened (the deepest
// folder that holds most of it), not every parent above it.
func TestHotspots(t *testing.T) {
	s := openTest(t)
	root := t.TempDir()
	p := filepath.Join(root, "users", "me", "cache", "blob.bin")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, make([]byte, 100_000), 0o644)
	other := filepath.Join(root, "spread")
	for _, n := range []string{"a", "b", "c", "d"} {
		os.MkdirAll(filepath.Join(other, n), 0o755)
		os.WriteFile(filepath.Join(other, n, "f"), make([]byte, 60_000), 0o644)
	}
	before := saveScan(t, s, root)

	os.WriteFile(p, make([]byte, 900_000), 0o644)
	for _, n := range []string{"a", "b", "c", "d"} {
		os.WriteFile(filepath.Join(other, n, "f"), make([]byte, 200_000), 0o644)
	}
	after := saveScan(t, s, root)

	hs, err := s.Hotspots(before, after, 100_000, 10)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, h := range hs {
		keys[h.Key] = true
	}
	if !keys[`users\me\cache`] && !keys[`users\me\cache\blob.bin`] {
		t.Errorf("hotspots %v miss the cache that grew", keys)
	}
	for _, parent := range []string{"users", `users\me`, ""} {
		if keys[parent] {
			t.Errorf("hotspots %v include %q, whose growth is all in one child", keys, parent)
		}
	}
	if !keys["spread"] {
		t.Errorf("hotspots %v miss 'spread', where growth is split over many children", keys)
	}
}

// Proves: snapshots of different roots cannot be compared.
func TestCompareDifferentRoots(t *testing.T) {
	s := openTest(t)
	a := saveScan(t, s, scanFixture(t, map[string]int{"x": 100_000}).Path())
	b := saveScan(t, s, scanFixture(t, map[string]int{"y": 100_000}).Path())
	if _, err := s.Compare(a, b, ""); err == nil {
		t.Error("compared snapshots of different folders")
	}
	if _, err := s.Hotspots(a, b, 1, 10); err == nil {
		t.Error("hotspots across different folders")
	}
}

// Proves: only the newest Keep snapshots per root are kept, and their
// entries are deleted with them.
func TestPruneKeepsNewest(t *testing.T) {
	s := openTest(t)
	tree := scanFixture(t, map[string]int{"a/b.bin": 100_000})
	opts := testOpts()
	opts.Keep = 3
	var ids []int64
	for i := 0; i < 5; i++ {
		id, err := s.Save(tree, time.Second, MethodWalk, opts)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	list, _ := s.Snapshots(tree.Path(), MethodWalk)
	if len(list) != 3 || list[2].ID != ids[2] {
		t.Fatalf("kept %+v, want the last 3 of %v", list, ids)
	}
	var orphans int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM entries WHERE snapshot_id NOT IN (SELECT id FROM snapshots)`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d entries left from pruned snapshots", orphans)
	}
}

func saveScan(t *testing.T, s *Store, root string) int64 {
	t.Helper()
	tree := mustScan(t, root)
	id, err := s.Save(tree, time.Second, MethodWalk, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustScan(t *testing.T, root string) *scan.Node {
	t.Helper()
	tree, err := scan.Scan(t.Context(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// Proves: snapshots made by the normal scan and by the full (MFT) scan of
// the same folder are kept apart: each list shows one method only, they
// cannot be compared with each other (the full scan sees folders the normal
// scan cannot, which would look like growth), and pruning counts each
// method separately so full scans never push out normal ones.
func TestSnapshotMethodsAreSeparate(t *testing.T) {
	s := openTest(t)
	tree := scanFixture(t, map[string]int{"a/b.bin": 100_000})
	opts := testOpts()
	opts.Keep = 2
	walk, err := s.Save(tree, time.Second, MethodWalk, opts)
	if err != nil {
		t.Fatal(err)
	}
	var full []int64
	for range 3 {
		id, err := s.Save(tree, time.Second, MethodMFT, opts)
		if err != nil {
			t.Fatal(err)
		}
		full = append(full, id)
	}

	w, _ := s.Snapshots(tree.Path(), MethodWalk)
	if len(w) != 1 || w[0].ID != walk || w[0].Method != MethodWalk {
		t.Errorf("normal scans = %+v, want only %d", w, walk)
	}
	f, _ := s.Snapshots(tree.Path(), MethodMFT)
	if len(f) != 2 || f[0].ID != full[2] || f[1].ID != full[1] || f[0].Method != MethodMFT {
		t.Errorf("full scans = %+v, want the newest 2 of %v", f, full)
	}
	if _, err := s.Compare(walk, full[2], ""); err == nil {
		t.Error("compared a normal scan with a full scan")
	}
	if _, err := s.Hotspots(walk, full[2], 1, 10); err == nil {
		t.Error("hotspots across scan methods")
	}
	if _, err := s.Compare(full[1], full[2], ""); err != nil {
		t.Errorf("two full scans: %v", err)
	}
	if _, err := s.Save(tree, time.Second, "guess", opts); err == nil {
		t.Error("unknown method accepted")
	}
}
