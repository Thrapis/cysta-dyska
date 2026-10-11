package actions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procSHFileOperationW = windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW")

// shFileOpStruct mirrors SHFILEOPSTRUCTW (shellapi.h, natural alignment on
// 64-bit). Layout is checked by TestShFileOpLayout.
type shFileOpStruct struct {
	Hwnd                 uintptr
	Func                 uint32
	From                 *uint16
	To                   *uint16
	Flags                uint16
	AnyOperationsAborted int32
	NameMappings         uintptr
	ProgressTitle        *uint16
}

const (
	foDelete           = 0x0003
	fofSilent          = 0x0004 // no progress dialog
	fofNoConfirmation  = 0x0010 // our own dialog already asked
	fofAllowUndo       = 0x0040 // Recycle Bin instead of delete
	fofNoErrorUI       = 0x0400
	fofWantNukeWarning = 0x4000 // still ask before deleting anything too big for the bin
)

// Recycle moves paths to the Recycle Bin. If something cannot be recycled
// (for example it is bigger than the bin), Windows asks the user before
// deleting it; if they decline, the operation is aborted and reported.
func Recycle(paths []string) error {
	from, err := doubleNullList(paths)
	if err != nil {
		return err
	}
	op := shFileOpStruct{
		Func:  foDelete,
		From:  &from[0],
		Flags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI | fofWantNukeWarning,
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ret, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	runtime.KeepAlive(from)

	if ret != 0 {
		return fmt.Errorf("the Recycle Bin operation failed (code %#x)", ret)
	}
	if op.AnyOperationsAborted != 0 {
		return errors.New("the operation was cancelled before it finished")
	}
	var left []string
	for _, p := range paths {
		if _, err := os.Lstat(p); err == nil {
			left = append(left, p)
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("%d item(s) are still there, for example %s (in use?)", len(left), left[0])
	}
	return nil
}

// doubleNullList encodes paths as the API wants: each path ends with a NUL
// and the list ends with one more NUL.
func doubleNullList(paths []string) ([]uint16, error) {
	if len(paths) == 0 {
		return nil, errors.New("nothing to recycle")
	}
	var buf []uint16
	for _, p := range paths {
		if !filepath.IsAbs(p) || strings.HasPrefix(p, `\\`) {
			return nil, fmt.Errorf("not a full local path: %q", p)
		}
		if len(p) > maxShellPath {
			return nil, fmt.Errorf("path too long for the Recycle Bin: %s", p)
		}
		u, err := windows.UTF16FromString(p) // fails on embedded NUL
		if err != nil {
			return nil, err
		}
		buf = append(buf, u...) // u ends with its own NUL
	}
	return append(buf, 0), nil
}
