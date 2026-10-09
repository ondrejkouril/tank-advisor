package app

import (
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// listWotctx lists the running wotctx processes with their image paths and
// start times. A process it may not open is left out: one from another user
// cannot be this user's Claude.
func listWotctx() ([]Proc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)

	var procs []Proc
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		if !strings.HasPrefix(name, "wotctx") || strings.HasPrefix(name, "wotctx-launcher") {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if err != nil {
			continue
		}
		var buf [windows.MAX_LONG_PATH]uint16
		size := uint32(len(buf))
		pathErr := windows.QueryFullProcessImageName(h, 0, &buf[0], &size)
		var created, exited, kernel, user windows.Filetime
		timesErr := windows.GetProcessTimes(h, &created, &exited, &kernel, &user)
		windows.CloseHandle(h)
		if pathErr != nil || timesErr != nil {
			continue
		}
		procs = append(procs, Proc{Path: windows.UTF16ToString(buf[:size]), Started: time.Unix(0, created.Nanoseconds())})
	}
	return procs, nil
}
