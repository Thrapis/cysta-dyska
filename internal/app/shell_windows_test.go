package app

import (
	"path/filepath"
	"testing"
)

// Proves: only plain ms-settings: page URIs are accepted, so nothing else
// (URLs, programs, arguments) can be launched through this door.
func TestSettingsCommandAllowsOnlySettingsPages(t *testing.T) {
	for _, ok := range []string{"ms-settings:storagesense", "ms-settings:appsfeatures"} {
		if _, err := SettingsCommand(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "https://example.com", "ms-settings:", "calc.exe", "ms-settings:x & calc",
		"ms-settings:storagesense\" /c calc", `C:\Windows\System32\cmd.exe`, "MS-SETTINGS:../x",
	} {
		if _, err := SettingsCommand(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// Proves: reveal only works for existing absolute paths and builds the
// explorer /select argument correctly.
func TestRevealCommand(t *testing.T) {
	dir := t.TempDir()
	cmd, err := RevealCommand(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := cmd.SysProcAttr.CmdLine; got != `explorer.exe /select,"`+dir+`"` {
		t.Errorf("command line = %q", got)
	}
	if _, err := RevealCommand("relative"); err == nil {
		t.Error("relative path accepted")
	}
	if _, err := RevealCommand(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing path accepted")
	}
}
