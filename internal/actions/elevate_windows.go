package actions

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellExecuteInfo mirrors SHELLEXECUTEINFOW (shellapi.h, natural alignment
// on 64-bit). Layout is checked by TestShellExecuteInfoLayout.
type shellExecuteInfo struct {
	Size       uint32
	Mask       uint32
	Hwnd       uintptr
	Verb       *uint16
	File       *uint16
	Parameters *uint16
	Directory  *uint16
	Show       int32
	InstApp    uintptr
	IDList     uintptr
	Class      *uint16
	KeyClass   uintptr
	HotKey     uint32
	Icon       uintptr // union with hMonitor
	Process    windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400
	swShowNormal          = 1
	errorCancelled        = 1223 // the user said no to the UAC prompt
)

// ErrDeclined means the user declined the UAC prompt.
var ErrDeclined = errors.New("administrator permission was not given")

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// shellExecute starts file with the given verb. If wait is set it waits for
// the process and returns its exit code.
func shellExecute(verb, file, params string, wait bool) (int, error) {
	v, err := windows.UTF16PtrFromString(verb)
	if err != nil {
		return 0, err
	}
	f, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return 0, err
	}
	p, err := windows.UTF16PtrFromString(params)
	if err != nil {
		return 0, err
	}
	info := shellExecuteInfo{
		Mask: seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
		Verb: v, File: f, Parameters: p, Show: swShowNormal,
	}
	info.Size = uint32(unsafe.Sizeof(info))

	runtime.LockOSThread()
	ok, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	runtime.UnlockOSThread()
	if ok == 0 {
		if errors.Is(callErr, syscall.Errno(errorCancelled)) {
			return 0, ErrDeclined
		}
		return 0, fmt.Errorf("could not start %s: %w", file, callErr)
	}
	if info.Process == 0 {
		return 0, nil // started without a process handle we can wait on
	}
	defer windows.CloseHandle(info.Process)
	if !wait {
		return 0, nil
	}
	if _, err := windows.WaitForSingleObject(info.Process, windows.INFINITE); err != nil {
		return 0, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.Process, &code); err != nil {
		return 0, err
	}
	return int(code), nil
}

// RunElevated runs program (already resolved by ResolveProgram with admin
// set) as administrator after the UAC prompt, waits, and returns its exit
// code. Its output appears in its own window and is not captured.
func RunElevated(program string, args []string) (int, error) {
	return shellExecute("runas", program, windows.ComposeCommandLine(args), true)
}

// LaunchUninstaller starts an UninstallString as Windows does from
// Settings > Apps. Windows shows a UAC prompt if the uninstaller needs one.
// It does not wait: many uninstallers hand over to a copy of themselves.
func LaunchUninstaller(uninstallString string) error {
	file, params, err := SplitCommandLine(uninstallString)
	if err != nil {
		return err
	}
	_, err = shellExecute("open", file, params, false)
	return err
}

// RunSelfElevated runs this program again as administrator with args, after
// the UAC prompt, waits, and returns its exit code. Used for the fast scan
// helper, which only reads.
func RunSelfElevated(args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	return shellExecute("runas", exe, windows.ComposeCommandLine(args), true)
}
