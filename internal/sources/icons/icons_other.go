//go:build !windows

package icons

import "errors"

func extractPNG(string, int) ([]byte, error) {
	return nil, errors.New("program icons are only read on Windows")
}
