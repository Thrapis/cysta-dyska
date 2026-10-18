//go:build darwin

package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// RevealCommand returns the command that shows path selected in Finder.
// path must be an existing absolute path; being absolute it starts with "/",
// so open cannot read it as an option. No shell is involved.
func RevealCommand(path string) (*exec.Cmd, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("path must be absolute")
	}
	if _, err := os.Lstat(path); err != nil {
		return nil, err
	}
	return exec.Command("open", "-R", filepath.Clean(path)), nil
}
