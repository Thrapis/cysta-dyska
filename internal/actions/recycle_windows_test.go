package actions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

// Proves: our SHFILEOPSTRUCTW mirror has the 64-bit layout of shellapi.h.
func TestShFileOpLayout(t *testing.T) {
	var s shFileOpStruct
	if unsafe.Sizeof(s) != 56 {
		t.Errorf("size = %d, want 56", unsafe.Sizeof(s))
	}
	for name, got := range map[string]uintptr{
		"Func": unsafe.Offsetof(s.Func), "From": unsafe.Offsetof(s.From), "Flags": unsafe.Offsetof(s.Flags),
		"AnyOperationsAborted": unsafe.Offsetof(s.AnyOperationsAborted), "ProgressTitle": unsafe.Offsetof(s.ProgressTitle),
	} {
		want := map[string]uintptr{"Func": 8, "From": 16, "Flags": 32, "AnyOperationsAborted": 36, "ProgressTitle": 48}[name]
		if got != want {
			t.Errorf("offset of %s = %d, want %d", name, got, want)
		}
	}
}

// Proves: the path list is double-null terminated and holds exactly the
// given paths. A missing terminator makes the API read past the buffer and
// can delete unrelated files.
func TestDoubleNullList(t *testing.T) {
	buf, err := doubleNullList([]string{`C:\a`, `C:\bb`})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint16{'C', ':', '\\', 'a', 0, 'C', ':', '\\', 'b', 'b', 0, 0}
	if len(buf) != len(want) {
		t.Fatalf("buf = %v, want %v", buf, want)
	}
	for i := range want {
		if buf[i] != want[i] {
			t.Fatalf("buf = %v, want %v", buf, want)
		}
	}
	for _, bad := range [][]string{nil, {`C:\a` + "\x00" + `C:\Windows`}, {`relative`}, {`\\?\C:\x`}, {`C:\` + strings.Repeat("x", 300)}} {
		if _, err := doubleNullList(bad); err == nil {
			t.Errorf("doubleNullList(%q) accepted", bad)
		}
	}
}

// Proves, against the real Recycle Bin: files and folders are moved there
// (not deleted, and not left behind). Opt-in because it adds two tiny test
// items to the user's Recycle Bin: set CYSTA_RECYCLE_TEST=1.
func TestRecycleReal(t *testing.T) {
	if os.Getenv("CYSTA_RECYCLE_TEST") != "1" {
		t.Skip("set CYSTA_RECYCLE_TEST=1 to test with the real Recycle Bin")
	}
	root := t.TempDir()
	file := filepath.Join(root, "cysta-dyska-recycle-test.txt")
	dir := filepath.Join(root, "cysta-dyska-recycle-test-dir")
	write(t, root, map[string]int{filepath.Base(file): 10, filepath.Base(dir) + "/inner.txt": 10})

	if err := Recycle([]string{file, dir}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{file, dir} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists after recycling", p)
		}
	}
}

// Proves: our SHELLEXECUTEINFOW mirror has the 64-bit layout of shellapi.h.
func TestShellExecuteInfoLayout(t *testing.T) {
	var s shellExecuteInfo
	if unsafe.Sizeof(s) != 112 {
		t.Errorf("size = %d, want 112", unsafe.Sizeof(s))
	}
	if off := unsafe.Offsetof(s.Show); off != 48 {
		t.Errorf("offset of nShow = %d, want 48", off)
	}
	if off := unsafe.Offsetof(s.HotKey); off != 88 {
		t.Errorf("offset of dwHotKey = %d, want 88", off)
	}
	if off := unsafe.Offsetof(s.Process); off != 104 {
		t.Errorf("offset of hProcess = %d, want 104", off)
	}
}
