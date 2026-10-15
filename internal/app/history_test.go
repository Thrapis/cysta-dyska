package app

import (
	"context"
	"path/filepath"
	"testing"

	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/store"
	"cysta-dyska/internal/testutil"
)

func newHistoryApp(t *testing.T) (*App, *fakeEmitter) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	em := newFakeEmitter()
	a := New(em, s)
	a.historyOpts = store.Options{MinAlloc: 50_000, MaxDepth: 8, Keep: 10}
	a.hotspotMinDelta = 100_000
	return a, em
}

func scanAndWait(t *testing.T, a *App, em *fakeEmitter, root string) {
	t.Helper()
	if err := a.StartScan(root); err != nil {
		t.Fatal(err)
	}
	em.wait(t)
	if name, data := em.last(); name != EventDone {
		t.Fatalf("scan ended with %s: %v", name, data)
	}
}

// Proves: every finished scan is saved; History lists the snapshots of the
// current folder newest first, and comparing two of them shows what grew,
// both level by level and as hotspots.
func TestHistoryRecordsScansAndCompares(t *testing.T) {
	a, em := newHistoryApp(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]int{"cache/blob.bin": 100_000, "docs/a.txt": 100_000})
	scanAndWait(t, a, em, root)
	writeFiles(t, root, map[string]int{"cache/blob.bin": 700_000})
	scanAndWait(t, a, em, root)

	h, err := a.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Snapshots) != 2 || h.CurrentID != h.Snapshots[0].ID || h.Warning != "" {
		t.Fatalf("history = %+v", h)
	}
	before, after := h.Snapshots[1].ID, h.Snapshots[0].ID

	rows, err := a.CompareChildren(before, after, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].Key != "cache" || rows[0].Delta < 590_000 {
		t.Errorf("compare = %+v, want cache first with about +600k", rows)
	}

	hot, err := a.Hotspots(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(hot) == 0 || (hot[0].Key != "cache" && hot[0].Key != `cache\blob.bin`) {
		t.Errorf("hotspots = %+v", hot)
	}
}

// Proves: without a history database the app still scans, and History
// says why there is no history instead of failing.
func TestHistoryUnavailable(t *testing.T) {
	em := newFakeEmitter()
	a := New(em, nil)
	root := t.TempDir()
	writeFiles(t, root, map[string]int{"f.bin": 10_000})
	scanAndWait(t, a, em, root)

	h, err := a.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Snapshots) != 0 || h.Warning == "" {
		t.Errorf("history = %+v, want no snapshots and a warning", h)
	}
}

// Proves: Treemap lays out one folder level inside the requested size,
// largest tile first, and folds the long tail into one "rest" tile.
func TestTreemap(t *testing.T) {
	a, em := newHistoryApp(t)
	root := t.TempDir()
	files := map[string]int{"big/x.bin": 400_000, "mid/y.bin": 200_000}
	for i := 0; i < maxTiles+3; i++ {
		files[filepath.Join("many", "f"+itoa(i))] = 5_000
	}
	writeFiles(t, root, files)
	scanAndWait(t, a, em, root)

	tiles, err := a.Treemap(root, 800, 600)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiles) != 3 {
		t.Fatalf("root tiles = %+v, want many, big and mid", tiles)
	}
	for i, tl := range tiles {
		if !tl.IsDir || !tl.HasChildren {
			t.Errorf("tile %s should be a folder with children", tl.Name)
		}
		if i > 0 && tl.Alloc > tiles[i-1].Alloc {
			t.Errorf("tiles not largest first: %s after %s", tl.Name, tiles[i-1].Name)
		}
	}
	for _, tl := range tiles {
		if tl.X < 0 || tl.Y < 0 || tl.X+tl.W > 800.001 || tl.Y+tl.H > 600.001 {
			t.Errorf("tile %+v outside 800x600", tl)
		}
	}

	many, err := a.Treemap(filepath.Join(root, "many"), 400, 300)
	if err != nil {
		t.Fatal(err)
	}
	if len(many) != maxTiles+1 || !many[len(many)-1].IsRest {
		t.Errorf("got %d tiles, want %d plus a rest tile", len(many), maxTiles)
	}
	if _, err := a.Treemap(filepath.Join(root, "missing"), 100, 100); err == nil {
		t.Error("treemap of a missing folder should fail")
	}
}

// Proves: a full scan is saved as such and History shows only snapshots of
// the same method as the scan on screen, so switching between full and
// normal scans never shows fake growth.
func TestHistoryKeepsScanMethodsApart(t *testing.T) {
	testutil.RequireWindows(t)
	a, em := newHistoryApp(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]int{"a/blob.bin": 100_000})
	scanAndWait(t, a, em, root)

	// The fake full scan returns a tree of the same folder, standing in for
	// the drive the helper would read.
	a.fastScan = func(ctx context.Context, _ string) (*scan.Node, error) {
		return scan.Scan(ctx, root, scan.Options{})
	}
	if err := a.StartFastScan("C:"); err != nil {
		t.Fatal(err)
	}
	em.wait(t)
	h, err := a.History()
	if err != nil {
		t.Fatal(err)
	}
	if h.Method != store.MethodMFT || len(h.Snapshots) != 1 || h.Snapshots[0].ID != h.CurrentID {
		t.Fatalf("after a full scan: %+v", h)
	}

	scanAndWait(t, a, em, root)
	h, _ = a.History()
	if h.Method != store.MethodWalk || len(h.Snapshots) != 2 {
		t.Fatalf("after a normal scan: %+v, want the 2 normal scans only", h)
	}
}
