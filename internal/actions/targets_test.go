package actions

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cysta-dyska/internal/attribute"
	"cysta-dyska/internal/testutil"
)

func write(t *testing.T, root string, files map[string]int) {
	t.Helper()
	for name, n := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Proves: "folder" targets the folder itself, "contents" targets what is
// inside it (so the folder stays), and a file is always itself.
func TestTargets(t *testing.T) {
	testutil.RequireWindows(t)
	root := t.TempDir()
	write(t, root, map[string]int{"cache/a.bin": 1, "cache/sub/b.bin": 1, "one.bin": 1})
	cache := filepath.Join(root, "cache")

	got, err := Targets(cache, attribute.FileActionFolder)
	if err != nil || len(got) != 1 || got[0] != cache {
		t.Errorf("folder targets = %v, %v", got, err)
	}
	got, err = Targets(cache, attribute.FileActionContents)
	if err != nil || len(got) != 2 || !strings.HasPrefix(got[0], cache+`\`) {
		t.Errorf("contents targets = %v, %v", got, err)
	}
	file := filepath.Join(root, "one.bin")
	got, err = Targets(file, attribute.FileActionContents)
	if err != nil || len(got) != 1 || got[0] != file {
		t.Errorf("file targets = %v, %v", got, err)
	}
	if _, err := Targets(cache, attribute.FileActionNone); err == nil {
		t.Error("no file action should give an error")
	}
	if _, err := Targets(filepath.Join(root, "missing"), attribute.FileActionFolder); err == nil {
		t.Error("missing path should give an error")
	}
}

// Proves: measuring counts bytes and files, and reports junctions inside a
// target so the action can be refused.
func TestMeasure(t *testing.T) {
	testutil.RequireWindows(t)
	root := t.TempDir()
	write(t, root, map[string]int{"t/a.bin": 100_000, "t/s/b.bin": 50_000, "real/x.bin": 10})
	target := filepath.Join(root, "t")

	m, err := Measure(context.Background(), []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if m.Files != 2 || m.Bytes < 150_000 || len(m.Links) != 0 || m.Blocked() != "" {
		t.Errorf("measure = %+v", m)
	}

	out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(target, "j"), filepath.Join(root, "real")).CombinedOutput()
	if err != nil {
		t.Fatalf("mklink: %v %s", err, out)
	}
	m, err = Measure(context.Background(), []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Links) != 1 || m.Blocked() == "" {
		t.Errorf("junction not reported: %+v", m)
	}

	// A target that is itself a junction is refused too.
	m, err = Measure(context.Background(), []string{filepath.Join(target, "j")})
	if err != nil {
		t.Fatal(err)
	}
	if m.Blocked() == "" {
		t.Error("a junction as target should be blocked")
	}
}

// Proves: paths too long for the Recycle Bin API are reported.
func TestMeasureLongPaths(t *testing.T) {
	root := t.TempDir()
	deep := root
	for len(deep) < 300 {
		deep = filepath.Join(deep, strings.Repeat("d", 40))
	}
	write(t, deep, map[string]int{"f.bin": 1})
	m, err := Measure(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if m.LongPath == "" {
		t.Error("long path not reported")
	}
}
