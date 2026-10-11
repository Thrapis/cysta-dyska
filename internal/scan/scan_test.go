package scan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile creates a file of exactly n bytes, creating parent dirs.
func writeFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustScan(t *testing.T, root string, opts Options) *Node {
	t.Helper()
	tree, err := Scan(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// Proves: directory sizes are the sum of their contents, file/dir counts are
// right, children come back sorted by on-disk size largest-first, and on-disk
// size is at least the logical size for ordinary files.
func TestScanSumsSizesAndSortsChildren(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "small", "a.bin"), 10_000)
	writeFile(t, filepath.Join(root, "big", "b.bin"), 300_000)
	writeFile(t, filepath.Join(root, "big", "nested", "c.bin"), 200_000)
	writeFile(t, filepath.Join(root, "top.bin"), 50_000)

	tree := mustScan(t, root, Options{})

	if tree.Size != 560_000 {
		t.Errorf("root size = %d, want 560000", tree.Size)
	}
	if tree.Files != 4 {
		t.Errorf("root files = %d, want 4", tree.Files)
	}
	if tree.Dirs != 3 { // small, big, big/nested
		t.Errorf("root dirs = %d, want 3", tree.Dirs)
	}
	if tree.Alloc < tree.Size || tree.Alloc > tree.Size+4*65536 {
		t.Errorf("root alloc = %d, want between size %d and size + 4 clusters", tree.Alloc, tree.Size)
	}

	var names []string
	var sum int64
	for _, c := range tree.Children {
		names = append(names, c.Name)
		sum += c.Alloc
	}
	if got, want := strings.Join(names, ","), "big,top.bin,small"; got != want {
		t.Fatalf("children = %s, want %s (sorted by on-disk size desc)", got, want)
	}
	if sum != tree.Alloc {
		t.Errorf("sum of child alloc = %d, root alloc = %d", sum, tree.Alloc)
	}

	big := tree.Children[0]
	if big.Size != 500_000 || !big.IsDir {
		t.Errorf("big = size %d dir %v, want dir of 500000 bytes", big.Size, big.IsDir)
	}
	if got := big.Path(); got != filepath.Join(root, "big") {
		t.Errorf("big.Path() = %q", got)
	}
}

// Proves: the result does not depend on how many workers ran the scan.
func TestScanSameResultForAnyWorkerCount(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		writeFile(t, filepath.Join(root, fmt.Sprintf("d%02d", i%7), fmt.Sprintf("s%02d", i%3), fmt.Sprintf("f%02d", i)), 1000*(i+1))
	}

	want := dump(mustScan(t, root, Options{Workers: 1}))
	for _, w := range []int{2, 8, 32} {
		if got := dump(mustScan(t, root, Options{Workers: w})); got != want {
			t.Fatalf("workers=%d gave a different tree:\n%s\nwant:\n%s", w, got, want)
		}
	}
}

func dump(n *Node) string {
	var b strings.Builder
	var walk func(*Node, int)
	walk = func(n *Node, depth int) {
		fmt.Fprintf(&b, "%s%s size=%d alloc=%d files=%d dirs=%d\n", strings.Repeat(" ", depth), n.Name, n.Size, n.Alloc, n.Files, n.Dirs)
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	walk(n, 0)
	return b.String()
}

// Proves: a cancelled context stops the scan and reports context.Canceled
// instead of returning a partial tree that looks complete.
func TestScanHonoursCancellation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(root, "d", fmt.Sprintf("x%02d", i), "f"), 1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Scan(ctx, root, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// Proves: progress callback is invoked and its final totals match the tree.
func TestScanReportsProgress(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x", "1.bin"), 10_000)
	writeFile(t, filepath.Join(root, "x", "2.bin"), 20_000)

	var last Progress
	calls := 0
	tree := mustScan(t, root, Options{OnProgress: func(p Progress) {
		calls++
		last = p
	}})
	if calls == 0 {
		t.Fatal("progress callback never called")
	}
	if last.Files != tree.Files || last.Bytes != tree.Alloc || last.Dirs != tree.Dirs {
		t.Errorf("final progress = %+v, tree = files %d alloc %d dirs %d", last, tree.Files, tree.Alloc, tree.Dirs)
	}
}

// Proves: scanning a path that does not exist is an error, not an empty tree.
func TestScanMissingRoot(t *testing.T) {
	_, err := Scan(context.Background(), filepath.Join(t.TempDir(), "nope"), Options{})
	if err == nil {
		t.Fatal("want error for missing root")
	}
}

// Proves: scanning a file instead of a folder is an error.
func TestScanFileRoot(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.bin")
	writeFile(t, p, 1)
	if _, err := Scan(context.Background(), p, Options{}); err == nil {
		t.Fatal("want error when root is a file")
	}
}

// Proves: Find resolves a path inside the scanned tree to its node,
// case-insensitively as on Windows.
func TestFind(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "b", "c.bin"), 7)

	tree := mustScan(t, root, Options{})
	n := tree.Find(filepath.Join(root, "A", "b"))
	if n == nil || n.Name != "b" || n.Size != 7 {
		t.Fatalf("Find(A/b) = %+v", n)
	}
	if tree.Find(root) != tree {
		t.Error("Find(root) should return root")
	}
	if tree.Find(filepath.Join(root, "missing")) != nil {
		t.Error("Find(missing) should be nil")
	}
	if tree.Find(filepath.Dir(root)) != nil {
		t.Error("Find(parent of root) should be nil")
	}
}
