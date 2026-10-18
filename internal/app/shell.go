package app

import (
	"errors"
	"os/exec"
	"regexp"
	"runtime"
)

// The UI may only open these kinds of things. Both actions are read-only:
// they show a folder or a Settings page and never change anything.
// RevealCommand lives in shell_windows.go.

var settingsURI = regexp.MustCompile(`^ms-settings:[a-z0-9-]+$`)

// SettingsCommand returns the command that opens a Windows Settings page.
// Only ms-settings: URIs are allowed, so a rule can never launch anything
// else.
func SettingsCommand(uri string) (*exec.Cmd, error) {
	if runtime.GOOS != "windows" {
		return nil, errors.New("Settings pages are only available on Windows")
	}
	if !settingsURI.MatchString(uri) {
		return nil, errors.New("not an ms-settings: URI")
	}
	return exec.Command("explorer.exe", uri), nil
}
