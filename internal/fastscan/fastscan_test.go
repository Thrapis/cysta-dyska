package fastscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"cysta-dyska/internal/actions"
	"cysta-dyska/internal/scan"
)

func testDir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "fastscan")
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

// Proves: the elevated helper only writes where the app expects it: a new
// file named mft-<16 hex>.bin directly in the result folder. Other folders,
// traversal, other names and existing files are refused, so the helper can
// never be used to create or overwrite files elsewhere as administrator.
func TestResultPathValidation(t *testing.T) {
	dir := testDir(t)
	ok := filepath.Join(dir, "mft-0123456789abcdef.bin")
	if err := checkResultPath(dir, ok); err != nil {
		t.Errorf("valid path refused: %v", err)
	}
	for _, p := range []string{
		filepath.Join(filepath.Dir(dir), "mft-0123456789abcdef.bin"),
		filepath.Join(dir, "..", "mft-0123456789abcdef.bin"),
		filepath.Join(dir, "sub", "mft-0123456789abcdef.bin"),
		filepath.Join(dir, "mft-0123456789abcdef.exe"),
		filepath.Join(dir, "history.db"),
		`C:\Windows\System32\mft-0123456789abcdef.bin`,
		"mft-0123456789abcdef.bin",
	} {
		if err := checkResultPath(dir, p); err == nil {
			t.Errorf("accepted %s", p)
		}
	}

	if err := os.WriteFile(ok, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeResult(ok, result{Err: "x"}); err == nil {
		t.Error("an existing file was overwritten")
	}
	if b, _ := os.ReadFile(ok); string(b) != "keep" {
		t.Errorf("existing file changed to %q", b)
	}
}

// Proves: the helper refuses bad arguments without reading any volume or
// writing any file, and reports a usage error.
func TestHelperRefusesBadArgs(t *testing.T) {
	dir := testDir(t)
	read := func(context.Context, string) ([]scan.Flat, int, []Step, error) {
		t.Fatal("volume read with bad arguments")
		return nil, 0, nil, nil
	}
	for _, args := range [][]string{
		nil,
		{"C:"},
		{`C:\`, filepath.Join(dir, "mft-0123456789abcdef.bin")},
		{`\\.\PhysicalDrive0`, filepath.Join(dir, "mft-0123456789abcdef.bin")},
		{"C:", filepath.Join(dir, "evil.bin")},
		{"C:", filepath.Join(dir, "mft-0123456789abcdef.bin"), "extra"},
	} {
		if code := helper(dir, args, read); code != exitUsage {
			t.Errorf("%q: exit %d, want %d", args, code, exitUsage)
		}
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("files written: %v", left)
	}
}

// fakeLauncher plays the elevated helper in-process.
func fakeLauncher(dir string, read readFunc) launcher {
	return func(args []string) (int, error) {
		if len(args) < 1 || args[0] != HelperFlag {
			return 0, errors.New("not a helper call")
		}
		return helper(dir, args[1:], read), nil
	}
}

// Proves: a fast scan round trip works: the helper's flat tree comes back
// as a scan tree with the same totals, and the result file is deleted.
func TestRunRoundTrip(t *testing.T) {
	dir := testDir(t)
	flat := []scan.Flat{
		{Parent: -1, Name: `C:\`, IsDir: true},
		{Parent: 0, Name: "Users", IsDir: true},
		{Parent: 1, Name: "a.bin", Size: 1000, Alloc: 4096, FileID: 7},
	}
	read := func(_ context.Context, drive string) ([]scan.Flat, int, []Step, error) {
		if drive != "C:" {
			t.Errorf("drive = %q", drive)
		}
		return flat, 0, nil, nil
	}
	root, err := run(context.Background(), dir, "C:", fakeLauncher(dir, read))
	if err != nil {
		t.Fatal(err)
	}
	if root.Path() != `C:\` || root.Alloc != 4096 || root.Files != 1 {
		t.Errorf("root = %s %d %d", root.Path(), root.Alloc, root.Files)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("result file left behind: %v", left)
	}
}

// Proves: failures come back as errors the app can show: the helper's own
// error (e.g. not NTFS), a declined UAC prompt (actions.ErrDeclined, so the
// app can fall back quietly), and a helper that exits without a result.
func TestRunFailures(t *testing.T) {
	dir := testDir(t)
	failing := func(context.Context, string) ([]scan.Flat, int, []Step, error) {
		return nil, 0, nil, errors.New("D: is not an NTFS volume")
	}
	if _, err := run(context.Background(), dir, "D:", fakeLauncher(dir, failing)); err == nil || err.Error() != "D: is not an NTFS volume" {
		t.Errorf("helper error = %v", err)
	}
	declined := func([]string) (int, error) { return 0, actions.ErrDeclined }
	if _, err := run(context.Background(), dir, "C:", declined); !errors.Is(err, actions.ErrDeclined) {
		t.Errorf("declined = %v", err)
	}
	silent := func([]string) (int, error) { return 0, nil }
	if _, err := run(context.Background(), dir, "C:", silent); err == nil {
		t.Error("missing result accepted")
	}
	if _, err := run(context.Background(), dir, `C:\Users`, silent); err == nil {
		t.Error("a folder was accepted as a drive")
	}
}

// Proves: result files left by an earlier fast scan (the app was closed
// while the helper ran) are removed before a new one, and nothing else in
// the folder is touched.
func TestRunCleansStaleResults(t *testing.T) {
	dir := testDir(t)
	stale := filepath.Join(dir, "mft-00000000000000aa.bin")
	other := filepath.Join(dir, "notes.txt")
	os.WriteFile(stale, []byte("old"), 0o600)
	os.WriteFile(other, []byte("mine"), 0o600)
	read := func(context.Context, string) ([]scan.Flat, int, []Step, error) {
		return []scan.Flat{{Parent: -1, Name: `C:\`, IsDir: true}}, 0, nil, nil
	}
	if _, err := run(context.Background(), dir, "C:", fakeLauncher(dir, read)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale result kept")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("unrelated file removed")
	}
}
