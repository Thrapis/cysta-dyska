package locations

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func baseEnv(home string) map[string]string {
	return map[string]string{
		"USERPROFILE":       home,
		"LOCALAPPDATA":      filepath.Join(home, "AppData", "Local"),
		"APPDATA":           filepath.Join(home, "AppData", "Roaming"),
		"ProgramData":       `C:\ProgramData`,
		"ProgramFiles":      `C:\Program Files`,
		"ProgramFiles(x86)": `C:\Program Files (x86)`,
		"SystemRoot":        `C:\Windows`,
		"SystemDrive":       `C:`,
		"TEMP":              filepath.Join(home, "AppData", "Local", "Temp"),
	}
}

// Proves: with no overrides, every tool location falls back to its
// documented default, and SystemDrive becomes a usable root ("C:\").
func TestResolveDefaults(t *testing.T) {
	home := t.TempDir()
	v := Resolve(fakeEnv(baseEnv(home)))
	local := filepath.Join(home, "AppData", "Local")

	want := map[string]string{
		"SYSTEMDRIVE": `C:\`,
		"GOPATH":      filepath.Join(home, "go"),
		"GOMODCACHE":  filepath.Join(home, "go", "pkg", "mod"),
		"GOCACHE":     filepath.Join(local, "go-build"),
		"NPM_CACHE":   filepath.Join(local, "npm-cache"),
		"PIP_CACHE":   filepath.Join(local, "pip", "Cache"),
		"CARGO_HOME":  filepath.Join(home, ".cargo"),
		"GRADLE_HOME": filepath.Join(home, ".gradle"),
		"MAVEN_REPO":  filepath.Join(home, ".m2", "repository"),
		"NUGET_PKGS":  filepath.Join(home, ".nuget", "packages"),
	}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %q, want %q", k, v[k], w)
		}
	}
}

// Proves: environment overrides win over defaults, and only the first
// GOPATH entry is used.
func TestResolveEnvOverrides(t *testing.T) {
	home := t.TempDir()
	env := baseEnv(home)
	env["GOPATH"] = `D:\gopath;E:\other`
	env["GOCACHE"] = `D:\gocache`
	env["npm_config_cache"] = `D:\npm`
	env["PIP_CACHE_DIR"] = `D:\pip`
	env["CARGO_HOME"] = `D:\cargo`

	v := Resolve(fakeEnv(env))
	for k, w := range map[string]string{
		"GOPATH": `D:\gopath`, "GOMODCACHE": `D:\gopath\pkg\mod`, "GOCACHE": `D:\gocache`,
		"NPM_CACHE": `D:\npm`, "PIP_CACHE": `D:\pip`, "CARGO_HOME": `D:\cargo`,
	} {
		if v[k] != w {
			t.Errorf("%s = %q, want %q", k, v[k], w)
		}
	}
}

// Proves: settings saved with `go env -w` (the GOENV file) and in .npmrc
// are honoured, so moved caches are still found without running the tools.
func TestResolveReadsToolConfigFiles(t *testing.T) {
	home := t.TempDir()
	env := baseEnv(home)
	goenv := filepath.Join(env["APPDATA"], "go", "env")
	write(t, goenv, "GOMODCACHE=D:\\mods\nGOCACHE=D:\\build\n")
	write(t, filepath.Join(home, ".npmrc"), "registry=https://example.invalid/\ncache = D:\\npmcache\n")

	v := Resolve(fakeEnv(env))
	for k, w := range map[string]string{
		"GOMODCACHE": `D:\mods`, "GOCACHE": `D:\build`, "NPM_CACHE": `D:\npmcache`,
	} {
		if v[k] != w {
			t.Errorf("%s = %q, want %q", k, v[k], w)
		}
	}
}

// Proves: a missing base variable leaves dependent locations unset rather
// than producing relative paths that could match the wrong folder.
func TestResolveMissingBaseLeavesUnset(t *testing.T) {
	v := Resolve(fakeEnv(map[string]string{}))
	for _, k := range []string{"USERPROFILE", "GOPATH", "GOCACHE", "NPM_CACHE", "SYSTEMDRIVE"} {
		if v[k] != "" {
			t.Errorf("%s = %q, want empty", k, v[k])
		}
	}
}

// Proves: Resolve only ever produces names listed in Known, so the rule
// validator can check rules against Known.
func TestResolveOnlyKnownNames(t *testing.T) {
	home := t.TempDir()
	known := map[string]bool{}
	for _, k := range Known {
		known[k] = true
	}
	for k := range Resolve(fakeEnv(baseEnv(home))) {
		if !known[k] {
			t.Errorf("Resolve produced %q, which is not in Known", k)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
