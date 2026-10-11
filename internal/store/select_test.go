package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cysta-dyska/internal/scan"
)

func scanFixture(t *testing.T, files map[string]int) *scan.Node {
	t.Helper()
	root := t.TempDir()
	for name, n := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tree, err := scan.Scan(context.Background(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// Proves: only entries at least minAlloc big and at most maxDepth deep are
// kept; the root is always kept; keys are case-insensitive relative paths.
func TestSelect(t *testing.T) {
	tree := scanFixture(t, map[string]int{
		"Big/Deeper/Deepest/huge.bin": 300_000,
		"Big/small.bin":               1_000,
		"Tiny/a.bin":                  1_000,
	})
	rows := Select(tree, 100_000, 2)

	got := map[string]Row{}
	for _, r := range rows {
		got[r.Key] = r
	}
	for _, want := range []string{"", `big`, `big\deeper`} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing key %q; got %v", want, keys(got))
		}
	}
	for _, notWant := range []string{`big\deeper\deepest`, `tiny`, `big\small.bin`} {
		if _, ok := got[notWant]; ok {
			t.Errorf("unexpected key %q", notWant)
		}
	}
	if r := got[`big\deeper`]; r.Path != `Big\Deeper` || r.Depth != 2 || !r.IsDir || r.Alloc < 300_000 {
		t.Errorf("big/deeper row = %+v", r)
	}
	if got[""].Alloc != tree.Alloc {
		t.Errorf("root alloc = %d, want %d", got[""].Alloc, tree.Alloc)
	}
}

// Proves: every selected row's parent is selected too, so stored snapshots
// are a connected tree that can be browsed level by level.
func TestSelectIsConnected(t *testing.T) {
	tree := scanFixture(t, map[string]int{
		"a/b/c/d/e.bin": 200_000,
		"a/x.bin":       150_000,
		"f/g.bin":       120_000,
	})
	rows := Select(tree, 100_000, 10)
	have := map[string]bool{}
	for _, r := range rows {
		have[r.Key] = true
	}
	for _, r := range rows {
		if r.Key == "" {
			continue
		}
		parent := filepath.Dir(r.Key)
		if parent == "." {
			parent = ""
		}
		if !have[parent] {
			t.Errorf("row %q has no parent row %q", r.Key, parent)
		}
	}
}

func keys(m map[string]Row) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
