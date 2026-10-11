//go:build !windows

package mft

import (
	"context"
	"errors"
)

// ErrFragmentedMFT means the $MFT's own run list does not fit in its first record.
var ErrFragmentedMFT = errors.New("the master file table is too fragmented for the fast scan")

// ReadVolume is only supported on Windows.
func ReadVolume(context.Context, string) (*Tree, error) {
	return nil, errors.New("only supported on Windows")
}
