package locations

import (
	"path/filepath"
	"testing"
)

// Proves: with GOOS=darwin the macOS folders and tool defaults come from HOME
// (Library/Caches etc.), no Windows variable is produced, and Windows
// variables are not produced from a macOS environment.
func TestResolveDarwin(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{"HOME": home, "LOCALAPPDATA": filepath.Join(home, "ignored")}
	v := resolve(func(k string) string { return env[k] }, "darwin")

	want := map[string]string{
		"HOME":       home,
		"LIBRARY":    filepath.Join(home, "Library"),
		"CACHES":     filepath.Join(home, "Library", "Caches"),
		"APPSUPPORT": filepath.Join(home, "Library", "Application Support"),
		"GOCACHE":    filepath.Join(home, "Library", "Caches", "go-build"),
		"NPM_CACHE":  filepath.Join(home, ".npm"),
		"PIP_CACHE":  filepath.Join(home, "Library", "Caches", "pip"),
		"GOPATH":     filepath.Join(home, "go"),
	}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %q, want %q", k, v[k], w)
		}
	}
	for _, k := range []string{"USERPROFILE", "LOCALAPPDATA", "SYSTEMROOT"} {
		if _, ok := v[k]; ok {
			t.Errorf("Windows variable %s must not resolve on darwin", k)
		}
	}
}
