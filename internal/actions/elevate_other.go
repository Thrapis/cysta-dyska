//go:build !windows

package actions

import (
	"errors"
	"os/exec"
)

// ErrDeclined means the user declined the UAC prompt.
var ErrDeclined = errors.New("administrator permission was not given")

var errUnsupported = errors.New("only supported on Windows")

func hideWindow(*exec.Cmd) {}

// Recycle is only supported on Windows.
func Recycle([]string) error { return errUnsupported }

// RunElevated is only supported on Windows.
func RunElevated(string, []string) (int, error) { return 0, errUnsupported }

// LaunchUninstaller is only supported on Windows.
func LaunchUninstaller(string) error { return errUnsupported }
