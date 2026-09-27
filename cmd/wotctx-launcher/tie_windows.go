package main

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// startTied starts cmd inside a job object that ends it when the launcher
// ends. Claude Desktop stops an extension by ending the launcher; without the
// job, wotctx would live on, holding the cache open and its own executable
// locked against the next update.
func startTied(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return cmd.Start() // still usable, just not tied
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return cmd.Start()
	}
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return err
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		windows.AssignProcessToJobObject(job, proc)
		windows.CloseHandle(proc)
	}
	// The job handle stays open for the launcher's lifetime; the OS closes it
	// when the launcher exits, which ends wotctx.
	return nil
}

// registryInstallDir reads where the installer put Tank Advisor.
func registryInstallDir() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Tank Advisor`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	dir, _, err := k.GetStringValue("InstallDir")
	if err != nil {
		return ""
	}
	return dir
}
