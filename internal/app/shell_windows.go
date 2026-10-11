package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// RevealCommand returns the command that shows path selected in Explorer.
// path must be an existing absolute path.
func RevealCommand(path string) (*exec.Cmd, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("path must be absolute")
	}
	if _, err := os.Lstat(path); err != nil {
		return nil, err
	}
	path = filepath.Clean(path)
	cmd := exec.Command("explorer.exe")
	// Explorer only understands /select,"<path>"; Go's default quoting of
	// the whole argument would make it open Documents instead. Windows
	// paths cannot contain '"', so this cannot be escaped out of.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	return cmd, nil
}
