//go:build !windows && !darwin

package app

import (
	"errors"
	"os/exec"
)

// RevealCommand is only supported on Windows.
func RevealCommand(path string) (*exec.Cmd, error) {
	return nil, errors.New("not supported on this platform")
}
