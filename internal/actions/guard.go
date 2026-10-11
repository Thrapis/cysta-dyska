// Package actions carries out the cleanups the user confirms: running an
// owner's command, moving files to the Recycle Bin, deleting them (only if
// the user enabled it), and starting uninstallers. Every file action goes
// through Guard, which no rule can override.
package actions

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"cysta-dyska/internal/sources/locations"
)

// Guard decides whether a path may be moved to the Recycle Bin or deleted.
type Guard struct {
	protected []string // lower-case; these and everything above them are refused
	subtrees  []string // lower-case; these and everything below them are refused
	home      string   // lower-case user profile
	usersDir  string   // lower-case parent of the user profile, e.g. c:\users
}

// NewGuard builds a guard for this machine's folders.
func NewGuard(v locations.Vars) *Guard {
	low := func(p string) string { return strings.ToLower(filepath.Clean(p)) }
	g := &Guard{}
	add := func(list *[]string, p string) {
		if p != "" {
			*list = append(*list, low(p))
		}
	}
	// Whole areas owned by Windows and installed programs. Without the
	// environment, fall back to the usual locations rather than fail open.
	for _, k := range []string{"SYSTEMROOT", "PROGRAMFILES", "PROGRAMFILESX86"} {
		add(&g.subtrees, v[k])
	}
	for _, p := range []string{`C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`} {
		add(&g.subtrees, p)
	}

	// Folders that must never be removed as a whole (nor anything above them).
	for _, k := range []string{"USERPROFILE", "LOCALAPPDATA", "APPDATA", "PROGRAMDATA", "TEMP"} {
		add(&g.protected, v[k])
	}
	if home := v["USERPROFILE"]; home != "" {
		g.home = low(home)
		g.usersDir = filepath.Dir(g.home)
		for _, name := range []string{"AppData", "Documents", "Desktop", "Downloads", "Pictures",
			"Music", "Videos", "OneDrive", "Favorites", "Contacts", "Saved Games", "Links", "Searches"} {
			add(&g.protected, filepath.Join(home, name))
		}
	}
	return g
}

// Check returns why path may not be removed, or nil.
func (g *Guard) Check(path string) error {
	if path == "" || !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) {
		return errors.New("only full local paths can be removed")
	}
	if strings.Contains(path, `\..`) || strings.Contains(path, `\.\`) {
		return errors.New("path must not contain . or .. segments")
	}
	p := strings.ToLower(filepath.Clean(path))
	parts := strings.Split(strings.TrimSuffix(p, `\`), `\`)

	if len(parts) < 3 {
		return fmt.Errorf("%s is a drive or a top-level folder", path)
	}
	for _, seg := range parts {
		if seg == "$recycle.bin" || seg == "system volume information" {
			return fmt.Errorf("%s belongs to Windows", path)
		}
	}
	for _, s := range g.subtrees {
		if within(p, s) {
			return fmt.Errorf("%s is inside %s, which Windows or installed programs own", path, s)
		}
	}
	for _, s := range g.protected {
		if within(s, p) { // p is s or above it
			return fmt.Errorf("%s is or contains a main user or system folder", path)
		}
	}
	// Other people's profiles, and C:\Users itself.
	users := g.usersDir
	if users == "" {
		users = parts[0] + `\users`
	}
	if within(p, users) {
		if g.home == "" || !within(p, g.home) {
			return fmt.Errorf("%s is not in your user folder", path)
		}
	}
	return nil
}

// within reports whether p is base or below it (both lower-case, clean).
func within(p, base string) bool {
	return p == base || strings.HasPrefix(p, strings.TrimSuffix(base, `\`)+`\`)
}
