package app

import (
	"context"
	"testing"

	"cysta-dyska/internal/actions"
	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/testutil"
)

func fakeFastTree(t *testing.T) *scan.Node {
	t.Helper()
	root, err := scan.Build([]scan.Flat{
		{Parent: -1, Name: `C:\`, IsDir: true},
		{Parent: 0, Name: "big.bin", Size: 5000, Alloc: 8192, FileID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// Proves: a fast scan accepts "C:" or `C:\`, passes the bare drive to the
// helper, and its tree goes through the same path as a normal scan
// (attribution, then the done event with the root summary).
func TestFastScan(t *testing.T) {
	testutil.RequireWindows(t)
	for _, in := range []string{"C:", `C:\`, "c:"} {
		em := newFakeEmitter()
		a := New(em, nil)
		var got string
		a.fastScan = func(_ context.Context, drive string) (*scan.Node, error) {
			got = drive
			return fakeFastTree(t), nil
		}
		if err := a.StartFastScan(in); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		em.wait(t)
		name, data := em.last()
		if got != "C:" || name != EventDone || data.(NodeDTO).Alloc != 8192 {
			t.Errorf("%q: helper got %q, last event %s %+v", in, got, name, data)
		}
		if c, err := a.Culprits(); err != nil || c.Items == nil {
			t.Errorf("%q: culprits after fast scan: %+v, %v", in, c, err)
		}
	}
}

// Proves: only whole drives can be fast-scanned; folders are refused before
// any UAC prompt.
func TestFastScanRejectsFolders(t *testing.T) {
	a := New(newFakeEmitter(), nil)
	a.fastScan = func(context.Context, string) (*scan.Node, error) {
		t.Fatal("helper started for a folder")
		return nil, nil
	}
	for _, in := range []string{`C:\Users`, `\server\share`, "", "C"} {
		if err := a.StartFastScan(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

// Proves: declining the UAC prompt is reported as its own event, not as an
// error, and the previous scan stays in place.
func TestFastScanDeclined(t *testing.T) {
	testutil.RequireWindows(t)
	em := newFakeEmitter()
	a := New(em, nil)
	a.fastScan = func(context.Context, string) (*scan.Node, error) {
		return fakeFastTree(t), nil
	}
	a.StartFastScan("C:")
	em.wait(t)
	before := a.tree

	a.fastScan = func(context.Context, string) (*scan.Node, error) { return nil, actions.ErrDeclined }
	if err := a.StartFastScan("C:"); err != nil {
		t.Fatal(err)
	}
	em.wait(t)
	if name, _ := em.last(); name != EventDeclined {
		t.Errorf("last event = %s, want %s", name, EventDeclined)
	}
	if a.tree != before {
		t.Error("previous scan was dropped")
	}
}
