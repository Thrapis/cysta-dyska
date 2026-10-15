// Package locations resolves well-known folders (Windows shell folders and
// developer tool caches) to absolute paths. It reads environment variables
// and tool config files only; it never runs the tools themselves.
package locations

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Known lists every name Resolve can produce. Rules may reference only
// these, as {NAME}. Windows rules use the Windows folders and macOS rules
// the macOS ones; a name that does not resolve on this OS skips the rule.
var Known = []string{
	// Windows folders
	"USERPROFILE", "LOCALAPPDATA", "APPDATA", "PROGRAMDATA", "PROGRAMFILES",
	"PROGRAMFILESX86", "SYSTEMROOT", "SYSTEMDRIVE", "TEMP",
	// macOS folders
	"HOME", "LIBRARY", "CACHES", "APPSUPPORT", "APPLICATIONS", "TMPDIR",
	// Developer tools
	"GOPATH", "GOMODCACHE", "GOCACHE", "NPM_CACHE", "PNPM_STORE", "YARN_CACHE",
	"PIP_CACHE", "UV_CACHE", "CARGO_HOME", "RUSTUP_HOME", "GRADLE_HOME",
	"MAVEN_REPO", "NUGET_PKGS", "ANDROID_SDK", "ANDROID_AVD",
}

// Vars maps a known name to an absolute path. Unresolvable names are absent.
type Vars map[string]string

// Resolve computes all known locations for the running OS. getenv is
// os.Getenv in production.
func Resolve(getenv func(string) string) Vars { return resolve(getenv, runtime.GOOS) }

func resolve(getenv func(string) string, goos string) Vars {
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

	// Per-OS base folders and the default location of each tool's cache.
	var home, local, roaming string // local/roaming are the Windows AppData folders
	var goConfig, goBuild, npmCache, pnpmStore, yarnCache, pipCache, uvCache, androidSDK string
	if goos == "darwin" {
		home = getenv("HOME")
		library := under(home, "Library")
		caches := under(library, "Caches")
		support := under(library, "Application Support")
		set("HOME", home)
		set("LIBRARY", library)
		set("CACHES", caches)
		set("APPSUPPORT", support)
		set("APPLICATIONS", "/Applications")
		set("TMPDIR", getenv("TMPDIR"))
		goConfig = under(support, "go", "env")
		goBuild = under(caches, "go-build")
		npmCache = under(home, ".npm")
		pnpmStore = under(library, "pnpm", "store")
		yarnCache = under(caches, "Yarn")
		pipCache = under(caches, "pip")
		uvCache = under(home, ".cache", "uv")
		androidSDK = under(library, "Android", "sdk")
	} else {
		home = getenv("USERPROFILE")
		local = getenv("LOCALAPPDATA")
		roaming = getenv("APPDATA")
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
		goConfig = under(roaming, "go", "env")
		goBuild = under(local, "go-build")
		npmCache = under(local, "npm-cache")
		pnpmStore = under(local, "pnpm", "store")
		yarnCache = under(local, "Yarn", "Cache")
		pipCache = under(local, "pip", "Cache")
		uvCache = under(local, "uv", "cache")
		androidSDK = under(local, "Android", "Sdk")
	}

	// Go: environment, then the file written by `go env -w`, then defaults.
	goenv := readKeyValues(first(getenv("GOENV"), goConfig), "=")
	goVar := func(k string) string { return first(getenv(k), goenv[k]) }
	gopath := first(goVar("GOPATH"), under(home, "go"))
	if i := strings.IndexByte(gopath, filepath.ListSeparator); i >= 0 {
		gopath = gopath[:i]
	}
	set("GOPATH", gopath)
	set("GOMODCACHE", first(goVar("GOMODCACHE"), under(gopath, "pkg", "mod")))
	set("GOCACHE", first(goVar("GOCACHE"), goBuild))

	// JavaScript
	npmrc := readKeyValues(under(home, ".npmrc"), "=")
	set("NPM_CACHE", first(getenv("npm_config_cache"), npmrc["cache"], npmCache))
	set("PNPM_STORE", first(getenv("npm_config_store_dir"), pnpmStore))
	set("YARN_CACHE", first(getenv("YARN_CACHE_FOLDER"), yarnCache))

	// Python
	set("PIP_CACHE", first(getenv("PIP_CACHE_DIR"), pipCache))
	set("UV_CACHE", first(getenv("UV_CACHE_DIR"), uvCache))

	// Rust
	set("CARGO_HOME", first(getenv("CARGO_HOME"), under(home, ".cargo")))
	set("RUSTUP_HOME", first(getenv("RUSTUP_HOME"), under(home, ".rustup")))

	// JVM and .NET
	set("GRADLE_HOME", first(getenv("GRADLE_USER_HOME"), under(home, ".gradle")))
	set("MAVEN_REPO", under(home, ".m2", "repository"))
	set("NUGET_PKGS", first(getenv("NUGET_PACKAGES"), under(home, ".nuget", "packages")))

	// Android
	set("ANDROID_SDK", first(getenv("ANDROID_HOME"), getenv("ANDROID_SDK_ROOT"), androidSDK))
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
