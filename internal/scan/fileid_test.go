package scan

import "testing"

// Proves: only multiply-linked files get a dedup key, the key is stable for
// the same file and differs between devices that reuse an inode number.
func TestHardlinkID(t *testing.T) {
	if got := hardlinkID(1, 42, 1); got != 0 {
		t.Errorf("single link: got %d, want 0", got)
	}
	if got := hardlinkID(1, 0, 2); got != 0 {
		t.Errorf("inode 0: got %d, want 0", got)
	}
	a, b := hardlinkID(1, 42, 2), hardlinkID(1, 42, 3)
	if a == 0 || a != b {
		t.Errorf("same file must give the same non-zero key: %d vs %d", a, b)
	}
	if hardlinkID(1, 42, 2) == hardlinkID(2, 42, 2) {
		t.Error("same inode on different devices must not collide")
	}
}
