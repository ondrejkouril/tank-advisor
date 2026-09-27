package main

import (
	"os"
	"os/exec"
	"syscall"
)

// showInFolder opens Explorer at a folder, or at the folder holding a file
// with the file selected. The command line is written out by hand because
// Explorer wants /select,"path" with the quotes after the comma, which Go's
// own argument quoting does not produce.
func showInFolder(path string) error {
	cmdLine := `explorer.exe "` + path + `"`
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		cmdLine = `explorer.exe /select,"` + path + `"`
	}
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdLine}
	// Explorer exits with 1 even when it succeeds, so only starting counts.
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
