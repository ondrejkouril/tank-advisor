package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// shellExecuteInfo is SHELLEXECUTEINFOW. x/sys/windows has ShellExecute but
// not the Ex form, which is the one that hands back the process to wait on.
type shellExecuteInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           windows.Handle
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       windows.Handle
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      windows.Handle
	dwHotKey       uint32
	hIconOrMonitor windows.Handle
	hProcess       windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	errorCancelled        = syscall.Errno(1223) // the player said no to the UAC prompt
)

var procShellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// installModElevated runs this executable again with administrator
// permission, which Windows asks the player for, to copy the mod into a game
// folder they cannot write to (docs/spec-desktop.md section 6.2). It waits
// for the copy and reports how it went.
func installModElevated(pkg, gameDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := strings.Join([]string{
		"--install-mod", syscall.EscapeArg(pkg),
		"--game-dir", syscall.EscapeArg(gameDir),
	}, " ")
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       windows.StringToUTF16Ptr("runas"),
		lpFile:       windows.StringToUTF16Ptr(exe),
		lpParameters: windows.StringToUTF16Ptr(args),
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if ok, _, callErr := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		if errors.Is(callErr, errorCancelled) {
			return errors.New("Windows did not get permission, so the mod was not copied")
		}
		return callErr
	}
	defer windows.CloseHandle(info.hProcess)
	if _, err := windows.WaitForSingleObject(info.hProcess, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("the copy failed (exit code %d); is the game running?", code)
	}
	return nil
}
