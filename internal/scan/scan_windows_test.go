package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Proves: our Go mirror of FILE_ID_BOTH_DIR_INFO has the same layout as the
// C struct documented by Microsoft, so parsing the kernel buffer is safe.
func TestDirInfoLayout(t *testing.T) {
	var d fileIDBothDirInfo
	checks := []struct {
		name      string
		got, want uintptr
	}{
		{"EndOfFile", unsafe.Offsetof(d.EndOfFile), 40},
		{"AllocationSize", unsafe.Offsetof(d.AllocationSize), 48},
		{"FileAttributes", unsafe.Offsetof(d.FileAttributes), 56},
		{"FileNameLength", unsafe.Offsetof(d.FileNameLength), 60},
		{"EaSize", unsafe.Offsetof(d.EaSize), 64},
		{"ShortName", unsafe.Offsetof(d.ShortName), 70},
		{"FileId", unsafe.Offsetof(d.FileId), 96},
		{"FileName", unsafe.Offsetof(d.FileName), 104},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("offset of %s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// Proves: name-surrogate reparse points (junctions, symlinks) are not
// descended into, but other reparse points are. OneDrive folders are
// cloud reparse points and must still be scanned.
func TestDescendReparse(t *testing.T) {
	const (
		symlink    = 0xA000000C
		cloud      = 0x9000001A
		dedup      = 0x80000013
		appExecLnk = 0x8000001B
	)
	cases := []struct {
		tag       uint32
		surrogate bool // true = points elsewhere, must not be followed
	}{
		{windows.IO_REPARSE_TAG_MOUNT_POINT, true},
		{symlink, true},
		{cloud, false},
		{dedup, false},
		{appExecLnk, false},
	}
	for _, c := range cases {
		if got := isNameSurrogate(c.tag); got != c.surrogate {
			t.Errorf("isNameSurrogate(%#x) = %v, want %v", c.tag, got, c.surrogate)
		}
	}
}

// Proves: directory junctions (which need no privileges and are common on
// Windows, e.g. "Application Data") are shown but not followed or
// double-counted.
func TestScanDoesNotFollowJunctions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real", "data.bin"), 100_000)
	mklink(t, "/J", filepath.Join(root, "junction"), filepath.Join(root, "real"))

	tree := mustScan(t, root, Options{})
	if tree.Size != 100_000 {
		t.Errorf("root size = %d, want 100000 (junction must not be followed)", tree.Size)
	}
	j := tree.Find(filepath.Join(root, "junction"))
	if j == nil || !j.Link || j.Size != 0 || len(j.Children) != 0 {
		t.Errorf("junction node = %+v, want a Link with no size and no children", j)
	}
}

// Proves: symlinked directories are not followed either.
func TestScanDoesNotFollowDirSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real", "data.bin"), 1000)
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("cannot create symlink (needs Developer Mode or admin): %v", err)
	}
	tree := mustScan(t, root, Options{})
	if tree.Size != 1000 {
		t.Errorf("root size = %d, want 1000 (symlink must not be followed)", tree.Size)
	}
}

// Proves: a hardlinked file uses its disk space once. The first path in
// name order keeps the on-disk size, so results are stable between scans.
func TestScanCountsHardlinksOnce(t *testing.T) {
	root := t.TempDir()
	const n = 1 << 20
	writeFile(t, filepath.Join(root, "a", "orig.bin"), n)
	if err := os.MkdirAll(filepath.Join(root, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "a", "orig.bin"), filepath.Join(root, "b", "link.bin")); err != nil {
		t.Fatalf("hardlink: %v", err)
	}

	for i := 0; i < 3; i++ { // repeat: the winner must not depend on scheduling
		tree := mustScan(t, root, Options{Workers: 8})
		a, b := tree.Find(filepath.Join(root, "a")), tree.Find(filepath.Join(root, "b"))
		if a.Alloc < n || b.Alloc != 0 {
			t.Fatalf("alloc a=%d b=%d, want a>=%d and b=0", a.Alloc, b.Alloc, n)
		}
		if tree.Alloc != a.Alloc {
			t.Errorf("root alloc = %d, want %d (counted once)", tree.Alloc, a.Alloc)
		}
		if tree.Size != 2*n {
			t.Errorf("root logical size = %d, want %d (every path counts logically)", tree.Size, 2*n)
		}
		link := tree.Find(filepath.Join(root, "b", "link.bin"))
		if !link.Hardlink {
			t.Error("second path should be marked Hardlink")
		}
	}
}

// Proves: NTFS-compressed files report their smaller on-disk size.
func TestScanCompressedFileUsesAllocatedSize(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "zeros.bin")
	const n = 4 << 20
	writeFile(t, p, n)
	if out, err := exec.Command("compact", "/c", "/q", p).CombinedOutput(); err != nil {
		t.Skipf("compact unavailable: %v: %s", err, out)
	}

	tree := mustScan(t, root, Options{})
	f := tree.Find(p)
	if f.Size != n {
		t.Errorf("logical size = %d, want %d", f.Size, n)
	}
	if f.Alloc >= n/2 {
		t.Errorf("alloc = %d, want well below %d for compressed zeros", f.Alloc, n)
	}
}

// Proves: sparse files report their (near zero) on-disk size, not their
// logical length. Large sparse VHDX/database files are common on dev boxes.
func TestScanSparseFileUsesAllocatedSize(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "sparse.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	var ret uint32
	err = windows.DeviceIoControl(windows.Handle(f.Fd()), windows.FSCTL_SET_SPARSE, nil, 0, nil, 0, &ret, nil)
	if err != nil {
		f.Close()
		t.Skipf("FSCTL_SET_SPARSE unsupported here: %v", err)
	}
	const n = 256 << 20
	if err := f.Truncate(n); err != nil {
		t.Fatal(err)
	}
	f.Close()

	tree := mustScan(t, root, Options{})
	node := tree.Find(p)
	if node.Size != n {
		t.Errorf("logical size = %d, want %d", node.Size, n)
	}
	if node.Alloc > 1<<20 {
		t.Errorf("alloc = %d, want < 1 MiB for an empty sparse file", node.Alloc)
	}
}

// Proves: a folder we may not read is marked with an error and skipped,
// and the rest of the scan still completes.
func TestScanSkipsAccessDeniedFolder(t *testing.T) {
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	writeFile(t, filepath.Join(locked, "secret.bin"), 10_000)
	writeFile(t, filepath.Join(root, "open", "ok.bin"), 20_000)

	user := os.Getenv("USERNAME")
	if out, err := exec.Command("icacls", locked, "/deny", user+":(OI)(CI)(RX)").CombinedOutput(); err != nil {
		t.Skipf("icacls deny failed: %v: %s", err, out)
	}
	t.Cleanup(func() {
		// Restore access so t.TempDir can delete the folder.
		_ = exec.Command("icacls", locked, "/remove:d", user, "/t").Run()
	})

	var last Progress
	tree := mustScan(t, root, Options{OnProgress: func(p Progress) { last = p }})
	l := tree.Find(locked)
	if l == nil || l.Err == "" {
		t.Fatalf("locked node = %+v, want an Err", l)
	}
	if tree.Size != 20_000 {
		t.Errorf("root size = %d, want 20000 (only the readable file)", tree.Size)
	}
	if last.Errors != 1 {
		t.Errorf("progress errors = %d, want 1", last.Errors)
	}
}

// Proves: paths longer than MAX_PATH (260) are scanned, not silently lost.
func TestScanLongPaths(t *testing.T) {
	root := t.TempDir()
	deep := root
	for len(deep) < 400 {
		deep = filepath.Join(deep, strings.Repeat("d", 40))
	}
	writeFile(t, filepath.Join(deep, "far.bin"), 12_345)

	tree := mustScan(t, root, Options{})
	if tree.Size != 12_345 {
		t.Errorf("root size = %d, want 12345 (file beyond MAX_PATH)", tree.Size)
	}
	if tree.Find(deep) == nil {
		t.Error("deep folder missing from tree")
	}
}

func mklink(t *testing.T, flag, link, target string) {
	t.Helper()
	out, err := exec.Command("cmd", "/c", "mklink", flag, link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("mklink %s failed: %v: %s", flag, err, out)
	}
}
