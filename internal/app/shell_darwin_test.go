//go:build darwin

package app

import (
	"reflect"
	"testing"
)

// Proves: Reveal on macOS runs exactly `open -R <path>` for an existing
// absolute path and refuses relative or missing paths.
func TestRevealCommandDarwin(t *testing.T) {
	dir := t.TempDir()
	cmd, err := RevealCommand(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"open", "-R", dir}; !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("args = %v, want %v", cmd.Args, want)
	}
	if _, err := RevealCommand("relative/path"); err == nil {
		t.Error("relative path must be refused")
	}
	if _, err := RevealCommand(dir + "/missing"); err == nil {
		t.Error("missing path must be refused")
	}
}
