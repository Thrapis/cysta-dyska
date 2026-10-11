//go:build !windows

package registry

// Installed returns nothing off Windows.
func Installed() ([]App, error) { return nil, nil }
