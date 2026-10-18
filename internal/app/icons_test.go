package app

import (
	"errors"
	"testing"

	"cysta-dyska/internal/sources/registry"
)

// Proves: AppIcons returns an icon for each owner that is an installed
// program (names match regardless of case), leaves out owners with no known
// program, no icon file or an icon that cannot be read, and always returns a
// non-nil map.
func TestAppIcons(t *testing.T) {
	a, _ := newHistoryApp(t)
	a.installedApps = func() ([]registry.App, error) {
		return []registry.App{
			{Name: "Foo Editor", IconPath: `C:\foo\foo.exe`, IconIndex: 2},
			{Name: "No Icon"},
			{Name: "Broken", IconPath: `C:\broken\b.exe`},
		}, nil
	}
	var asked []string
	a.appIcon = func(path string, index int) (string, error) {
		asked = append(asked, path)
		if path == `C:\broken\b.exe` {
			return "", errors.New("no icon")
		}
		return "data:image/png;base64,AAAA", nil
	}

	got := a.AppIcons([]string{"foo editor", "No Icon", "Broken", "Unknown", "Foo Editor"})
	if len(got) != 2 || got["foo editor"] == "" || got["Foo Editor"] == "" {
		t.Errorf("icons = %v, want only the Foo Editor owners", got)
	}

	a.installedApps = func() ([]registry.App, error) { return nil, errors.New("registry unreadable") }
	if got := a.AppIcons([]string{"x"}); got == nil || len(got) != 0 {
		t.Errorf("icons with unreadable registry = %#v, want an empty non-nil map", got)
	}
}
