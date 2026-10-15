package attribute

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/sources/locations"
)

func macPack(t *testing.T) *Pack {
	t.Helper()
	p, err := loadEmbeddedFor("darwin")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Proves: the macOS pack loads and validates (EN+BE texts, known variables),
// and the Windows pack is still what every other OS gets.
func TestMacOSPackIsValid(t *testing.T) {
	if n := len(macPack(t).Rules); n < 15 {
		t.Errorf("only %d macOS rules loaded", n)
	}
	w, err := loadEmbeddedFor("windows")
	if err != nil {
		t.Fatal(err)
	}
	if w.Rule("chrome-cache") == nil || w.Rule("mac-trash") != nil {
		t.Error("Windows pack must not contain macOS rules")
	}
}

// Proves: the macOS pack is explain-only. Nothing in it may opt in to file
// actions or runnable commands, because the app never changes a Mac in this
// version and macOS behaviour is untested.
func TestMacOSPackIsExplainOnly(t *testing.T) {
	for _, r := range macPack(t).Rules {
		if r.FileAction != FileActionNone {
			t.Errorf("%s: fileAction %q on macOS", r.ID, r.FileAction)
		}
		for i, s := range r.Suggest {
			if s.Run != nil || s.Settings != "" {
				t.Errorf("%s: suggest[%d] has run/settings on macOS", r.ID, i)
			}
		}
	}
}

// Proves: macOS rules written with "/" match a scanned tree through the macOS
// variables, case-insensitively, and name rules skip ~/Library and dot-folders.
func TestMatchMacOSRules(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, size int) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("home/Library/Caches/Homebrew/dl/x.tar.gz", 40_000)
	write("home/Library/Developer/Xcode/DerivedData/App-abc/Build/o", 30_000)
	write("home/.Trash/old.zip", 20_000)
	write("home/code/web/package.json", 100)
	write("home/code/web/node_modules/a/i.js", 10_000)
	write("home/Library/Foo/package.json", 100)
	write("home/Library/Foo/node_modules/a/i.js", 10_000)
	write("home/.cache/tool/package.json", 100)
	write("home/.cache/tool/node_modules/a/i.js", 10_000)

	home := filepath.Join(root, "home")
	vars := locations.Vars{
		"HOME":       home,
		"LIBRARY":    filepath.Join(home, "Library"),
		"CACHES":     filepath.Join(home, "Library", "Caches"),
		"APPSUPPORT": filepath.Join(home, "Library", "Application Support"),
	}
	tree, err := scan.Scan(context.Background(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := byRule(Match(tree, macPack(t), vars, nil))

	if len(got["mac-xcode-derived-data"]) != 1 {
		t.Errorf("DerivedData findings = %d, want 1", len(got["mac-xcode-derived-data"]))
	}
	if len(got["mac-homebrew-cache"]) != 1 {
		t.Errorf("Homebrew findings = %d, want 1", len(got["mac-homebrew-cache"]))
	}
	if len(got["mac-trash"]) != 1 {
		t.Errorf("Trash findings = %d, want 1", len(got["mac-trash"]))
	}
	// Homebrew sits inside Caches: the outer finding must not count it twice.
	if f := got["mac-user-caches"]; len(f) == 1 && f[0].Alloc >= 40_000 {
		t.Errorf("user caches alloc %d still includes the nested Homebrew finding", f[0].Alloc)
	}
	nm := got["node-modules"]
	if len(nm) != 1 || !filepath.IsAbs(nm[0].Node.Path()) ||
		filepath.Base(filepath.Dir(nm[0].Node.Path())) != "web" {
		t.Errorf("node-modules = %+v, want only home/code/web/node_modules", nm)
	}
}
