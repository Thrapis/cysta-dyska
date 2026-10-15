package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cysta-dyska/internal/sources/registry"
	"cysta-dyska/internal/testutil"
)

// newTestApp returns an App whose environment and installed-app list point
// into root, so attribution does not depend on the machine running tests.
func newTestApp(root string, apps []registry.App) (*App, *fakeEmitter) {
	em := newFakeEmitter()
	a := New(em, nil)
	env := map[string]string{
		"USERPROFILE":  filepath.Join(root, "home"),
		"LOCALAPPDATA": filepath.Join(root, "home", "AppData", "Local"),
		"APPDATA":      filepath.Join(root, "home", "AppData", "Roaming"),
	}
	a.getenv = func(k string) string { return env[k] }
	a.installedApps = func() ([]registry.App, error) { return apps, nil }
	return a, em
}

func writeFiles(t *testing.T, root string, files map[string]int) {
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

// Proves: after a scan, Culprits lists rule findings with both locales,
// suggestions, and totals; {owner} in template texts is filled in.
func TestCulpritsAfterScan(t *testing.T) {
	testutil.RequireWindows(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]int{
		"home/AppData/Local/npm-cache/_cacache/blob": 200_000,
		"home/AppData/Roaming/Widgetry/state.db":     100_000,
		"other/file.bin":                             50_000,
	})
	a, em := newTestApp(root, []registry.App{{Name: "Widgetry"}})

	if err := a.StartScan(root); err != nil {
		t.Fatal(err)
	}
	em.wait(t)

	res, err := a.Culprits()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("items = %+v, want npm cache and Widgetry data", res.Items)
	}
	npm := res.Items[0]
	if npm.RuleID != "npm-cache" || npm.Risk != "safe" || npm.Alloc < 200_000 {
		t.Errorf("first item = %+v, want the npm cache (largest)", npm)
	}
	if npm.Explain["en"] == "" || npm.Explain["be"] == "" || len(npm.Suggest) == 0 || npm.Suggest[0].Command == "" {
		t.Errorf("npm item lacks texts or command: %+v", npm)
	}

	data := res.Items[1]
	if data.RuleID != "app-data" || data.Owner != "Widgetry" {
		t.Errorf("second item = %+v", data)
	}
	for _, l := range []string{"en", "be"} {
		if strings.Contains(data.Explain[l], "{owner}") || !strings.Contains(data.Explain[l], "Widgetry") {
			t.Errorf("%s explain not filled in: %q", l, data.Explain[l])
		}
	}

	if res.Explained != npm.Alloc+data.Alloc {
		t.Errorf("explained = %d, want sum of items %d", res.Explained, npm.Alloc+data.Alloc)
	}
	if res.ScanAlloc <= res.Explained {
		t.Errorf("scan alloc %d should exceed explained %d (other/file.bin is unexplained)", res.ScanAlloc, res.Explained)
	}
}

// Proves: Culprits before any scan is a clear error.
func TestCulpritsBeforeScan(t *testing.T) {
	a, _ := newTestApp(t.TempDir(), nil)
	if _, err := a.Culprits(); err == nil {
		t.Fatal("want error before any scan")
	}
}

// Proves: a registry failure does not lose the rule-based findings.
func TestCulpritsSurviveRegistryError(t *testing.T) {
	testutil.RequireWindows(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]int{"home/AppData/Local/npm-cache/x": 100_000})
	a, em := newTestApp(root, nil)
	a.installedApps = func() ([]registry.App, error) { return nil, os.ErrPermission }

	if err := a.StartScan(root); err != nil {
		t.Fatal(err)
	}
	em.wait(t)
	res, err := a.Culprits()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Warning == "" {
		t.Errorf("items=%d warning=%q, want 1 item and a warning", len(res.Items), res.Warning)
	}
}
