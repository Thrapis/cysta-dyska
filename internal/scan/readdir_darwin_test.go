//go:build darwin

package scan

import (
	"os"
	"path/filepath"
	"testing"
)

// Proves: on macOS a hardlinked file is counted once, a symlink is not
// followed, and a sparse file reports its allocation rather than its length.
func TestDarwinReader(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	if err := os.WriteFile(a, make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, filepath.Join(dir, "b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(a, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	sp := filepath.Join(dir, "sparse")
	f, err := os.Create(sp)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	f.Close()

	entries, err := newDirReader().readDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]entry{}
	for _, e := range entries {
		by[e.name] = e
	}
	if by["a"].fileID == 0 || by["a"].fileID != by["b"].fileID {
		t.Errorf("hardlinks must share a non-zero id: %d vs %d", by["a"].fileID, by["b"].fileID)
	}
	if by["link"].reparseTag == 0 {
		t.Error("symlink must be flagged so it is not followed")
	}
	if by["sparse"].alloc >= by["sparse"].size {
		t.Errorf("sparse alloc %d should be below size %d", by["sparse"].alloc, by["sparse"].size)
	}
}
