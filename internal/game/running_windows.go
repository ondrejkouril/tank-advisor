package game

import (
	"context"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// runningClients lists the executables of running WorldOfTanks.exe
// processes.
func runningClients() []string {
	var exes []string
	for _, p := range RunningClients() {
		exes = append(exes, p.Exe)
	}
	return exes
}

// RunningClients lists the running WorldOfTanks.exe processes. It needs no
// privileges: the limited query right is enough for another process of the
// same user.
func RunningClients() []Process {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var found []Process
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "WorldOfTanks.exe") {
			continue
		}
		if exe := imagePath(entry.ProcessID); exe != "" {
			found = append(found, Process{PID: entry.ProcessID, Exe: exe})
		}
	}
	return found
}

func imagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

// WaitForExit blocks until the process has exited, or ctx ends. It waits on
// the process handle, so it costs nothing while it waits. A process that
// cannot be opened (it has already gone) counts as exited.
func WaitForExit(ctx context.Context, pid uint32) error {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(h)
	// The wait runs in slices so a cancelled ctx is noticed.
	for {
		event, err := windows.WaitForSingleObject(h, 5000)
		switch {
		case err != nil:
			return err
		case event == windows.WAIT_OBJECT_0:
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}
