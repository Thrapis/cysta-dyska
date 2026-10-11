// Package locations resolves well-known folders (Windows shell folders and
// developer tool caches) to absolute paths. It reads environment variables
// and tool config files only; it never runs the tools themselves.
package locations

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Known lists every name Resolve can produce. Rules may reference only
// these, as {NAME}.
var Known = []string{
	// Windows folders
	"USERPROFILE", "LOCALAPPDATA", "APPDATA", "PROGRAMDATA", "PROGRAMFILES",
	"PROGRAMFILESX86", "SYSTEMROOT", "SYSTEMDRIVE", "TEMP",
	// Developer tools
	"GOPATH", "GOMODCACHE", "GOCACHE", "NPM_CACHE", "PNPM_STORE", "YARN_CACHE",
	"PIP_CACHE", "UV_CACHE", "CARGO_HOME", "RUSTUP_HOME", "GRADLE_HOME",
	"MAVEN_REPO", "NUGET_PKGS", "ANDROID_SDK", "ANDROID_AVD",
}

// Vars maps a known name to an absolute path. Unresolvable names are absent.
type Vars map[string]string

// Resolve computes all known locations. getenv is os.Getenv in production.
func Resolve(getenv func(string) string) Vars {
	v := Vars{}
	set := func(name, path string) {
		if path != "" && filepath.IsAbs(path) {
			v[name] = filepath.Clean(path)
		}
	}
	first := func(vals ...string) string {
		for _, s := range vals {
			if s != "" {
				return s
			}
		}
		return ""
	}
	under := func(base string, elem ...string) string {
		if base == "" {
			return ""
		}
		return filepath.Join(append([]string{base}, elem...)...)
	}

	home := getenv("USERPROFILE")
	local := getenv("LOCALAPPDATA")
	roaming := getenv("APPDATA")
	set("USERPROFILE", home)
	set("LOCALAPPDATA", local)
	set("APPDATA", roaming)
	set("PROGRAMDATA", getenv("ProgramData"))
	set("PROGRAMFILES", getenv("ProgramFiles"))
	set("PROGRAMFILESX86", getenv("ProgramFiles(x86)"))
	set("SYSTEMROOT", getenv("SystemRoot"))
	set("TEMP", getenv("TEMP"))
	if d := getenv("SystemDrive"); d != "" {
		set("SYSTEMDRIVE", d+`\`) // "C:" alone would mean "current dir on C:"
	}

	// Go: environment, then the file written by `go env -w`, then defaults.
	goenv := readKeyValues(first(getenv("GOENV"), under(roaming, "go", "env")), "=")
	goVar := func(k string) string { return first(getenv(k), goenv[k]) }
	gopath := first(goVar("GOPATH"), under(home, "go"))
	if i := strings.IndexByte(gopath, filepath.ListSeparator); i >= 0 {
		gopath = gopath[:i]
	}
	set("GOPATH", gopath)
	set("GOMODCACHE", first(goVar("GOMODCACHE"), under(gopath, "pkg", "mod")))
	set("GOCACHE", first(goVar("GOCACHE"), under(local, "go-build")))

	// JavaScript
	npmrc := readKeyValues(under(home, ".npmrc"), "=")
	set("NPM_CACHE", first(getenv("npm_config_cache"), npmrc["cache"], under(local, "npm-cache")))
	set("PNPM_STORE", first(getenv("npm_config_store_dir"), under(local, "pnpm", "store")))
	set("YARN_CACHE", first(getenv("YARN_CACHE_FOLDER"), under(local, "Yarn", "Cache")))

	// Python
	set("PIP_CACHE", first(getenv("PIP_CACHE_DIR"), under(local, "pip", "Cache")))
	set("UV_CACHE", first(getenv("UV_CACHE_DIR"), under(local, "uv", "cache")))

	// Rust
	set("CARGO_HOME", first(getenv("CARGO_HOME"), under(home, ".cargo")))
	set("RUSTUP_HOME", first(getenv("RUSTUP_HOME"), under(home, ".rustup")))

	// JVM and .NET
	set("GRADLE_HOME", first(getenv("GRADLE_USER_HOME"), under(home, ".gradle")))
	set("MAVEN_REPO", under(home, ".m2", "repository"))
	set("NUGET_PKGS", first(getenv("NUGET_PACKAGES"), under(home, ".nuget", "packages")))

	// Android
	set("ANDROID_SDK", first(getenv("ANDROID_HOME"), getenv("ANDROID_SDK_ROOT"), under(local, "Android", "Sdk")))
	set("ANDROID_AVD", first(getenv("ANDROID_AVD_HOME"), under(home, ".android", "avd")))

	return v
}

// readKeyValues reads simple "key<sep>value" lines. A missing or unreadable
// file yields an empty map.
func readKeyValues(path, sep string) map[string]string {
	out := map[string]string{}
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		k, val, ok := strings.Cut(line, sep)
		if ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(val)
		}
	}
	return out
}
