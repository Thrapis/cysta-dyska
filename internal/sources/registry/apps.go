// Package registry reads information about installed programs from the
// Windows registry. It is strictly read-only: keys are opened with
// registry.READ and nothing is ever written (see TestRegistryIsReadOnly in
// internal/guard).
package registry

import (
	"path/filepath"
	"strings"
)

// App is one installed program as listed in Settings > Apps.
type App struct {
	Name            string
	Publisher       string
	Version         string
	InstallDir      string // "" when unknown or too broad to attribute
	EstimatedSize   int64  // bytes, as reported by the installer
	UninstallString string // shown to the user; never executed in v1
}

// installDir picks the program's folder from InstallLocation, falling back
// to the folder of DisplayIcon. It returns "" for paths that are not a
// program's own folder, so a misconfigured app cannot claim e.g. all of
// Program Files.
func installDir(location, icon string) string {
	dir := cleanPath(location)
	if dir == "" && icon != "" {
		// DisplayIcon looks like `"C:\path\app.exe",0`.
		p := icon
		if i := strings.LastIndexByte(p, ','); i > strings.LastIndexByte(p, '\\') {
			p = p[:i]
		}
		if p = cleanPath(p); p != "" {
			dir = filepath.Dir(p)
		}
	}
	if dir == "" || !filepath.IsAbs(dir) || tooBroad(dir) {
		return ""
	}
	return dir
}

func cleanPath(p string) string {
	p = strings.Trim(strings.TrimSpace(p), `"`)
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// tooBroad reports folders that hold many programs or system files and so
// must never be attributed to a single app.
func tooBroad(dir string) bool {
	d := strings.ToLower(dir)
	parts := strings.Split(strings.TrimSuffix(d, `\`), `\`)
	if len(parts) <= 1 { // drive root
		return true
	}
	under := func(prefix string) bool {
		return d == prefix || strings.HasPrefix(d, prefix+`\`)
	}
	if under(parts[0] + `\windows`) {
		return true
	}
	switch {
	case len(parts) == 2:
		// C:\Program Files, C:\ProgramData, C:\Users, ...
		return true
	case parts[1] == "users" && len(parts) <= 3:
		return true // C:\Users\me
	case parts[1] == "users" && len(parts) >= 4 && parts[3] == "appdata":
		// C:\Users\me\AppData, ...\Local, ...\Roaming, ...\Local\Programs
		if len(parts) <= 5 {
			return true
		}
		return len(parts) == 6 && parts[4] == "local" && parts[5] == "programs"
	case parts[1] == "programdata" && len(parts) >= 3 && parts[2] == "package cache":
		return true
	}
	return false
}
