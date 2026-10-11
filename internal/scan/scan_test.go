package scan

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// Proves: directory sizes are the sum of their contents, file/dir counts are
// right, and children come back sorted largest-first.
func TestScanSumsSizesAndSortsChildren(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "small", "a.bin"), 100)
	writeFile(t, filepath.Join(root, "big", "b.bin"), 3000)
	writeFile(t, filepath.Join(root, "big", "nested", "c.bin"), 2000)
	writeFile(t, filepath.Join(root, "top.bin"), 500)

	tree, err := Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}

	if tree.Size != 5600 {
		t.Errorf("root size = %d, want 5600", tree.Size)
	}
	if tree.Files != 4 {
		t.Errorf("root files = %d, want 4", tree.Files)
	}
	if tree.Dirs != 3 { // small, big, big/nested
		t.Errorf("root dirs = %d, want 3", tree.Dirs)
	}

	names := []string{}
	for _, c := range tree.Children {
		names = append(names, c.Name)
	}
	want := []string{"big", "top.bin", "small"}
	if len(names) != len(want) {
		t.Fatalf("children = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("children = %v, want %v (sorted by size desc)", names, want)
		}
	}

	big := tree.Children[0]
	if big.Size != 5000 || !big.IsDir {
		t.Errorf("big = %+v, want dir of 5000 bytes", big)
	}
}

// Proves: a cancelled context stops the scan and reports context.Canceled
// instead of returning a partial tree that looks complete.
func TestScanHonoursCancellation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(root, "d", string(rune('a'+i%26))+string(rune('a'+i/26)), "f"), 1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Scan(ctx, root, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// Proves: progress callback is invoked and its final totals match the tree.
func TestScanReportsProgress(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x", "1.bin"), 10)
	writeFile(t, filepath.Join(root, "x", "2.bin"), 20)

	var last Progress
	calls := 0
	tree, err := Scan(context.Background(), root, func(p Progress) {
		calls++
		last = p
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("progress callback never called")
	}
	if last.Files != tree.Files || last.Bytes != tree.Size {
		t.Errorf("final progress = %+v, tree = files %d size %d", last, tree.Files, tree.Size)
	}
}

// Proves: symlinked directories are not followed, so their target is not
// counted twice (a classic source of inflated totals on Windows).
func TestScanDoesNotFollowDirSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real", "data.bin"), 1000)
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("cannot create symlink (needs Developer Mode or admin): %v", err)
	}

	tree, err := Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Size != 1000 {
		t.Errorf("root size = %d, want 1000 (symlink must not be followed)", tree.Size)
	}
}

// Proves: directory junctions (which need no privileges and are common on
// Windows, e.g. "Application Data") are not followed or double-counted.
func TestScanDoesNotFollowJunctions(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions are Windows-only")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real", "data.bin"), 1000)
	out, err := exec.Command("cmd", "/c", "mklink", "/J",
		filepath.Join(root, "junction"), filepath.Join(root, "real")).CombinedOutput()
	if err != nil {
		t.Fatalf("mklink /J failed: %v: %s", err, out)
	}

	tree, err := Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Size != 1000 {
		t.Errorf("root size = %d, want 1000 (junction must not be followed)", tree.Size)
	}
}

// Proves: scanning a path that does not exist is an error, not an empty tree.
func TestScanMissingRoot(t *testing.T) {
	_, err := Scan(context.Background(), filepath.Join(t.TempDir(), "nope"), nil)
	if err == nil {
		t.Fatal("want error for missing root")
	}
}

// Proves: Find resolves a path inside the scanned tree to its node.
func TestFind(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "b", "c.bin"), 7)

	tree, err := Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	n := tree.Find(filepath.Join(root, "a", "b"))
	if n == nil || n.Name != "b" || n.Size != 7 {
		t.Fatalf("Find(a/b) = %+v", n)
	}
	if tree.Find(root) != tree {
		t.Error("Find(root) should return root")
	}
	if tree.Find(filepath.Join(root, "missing")) != nil {
		t.Error("Find(missing) should be nil")
	}
}
