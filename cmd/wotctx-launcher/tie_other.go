//go:build !windows

package main

import "os/exec"

// startTied starts cmd. Outside Windows the extension is not built; this
// keeps the package compiling for `go vet ./...` on any platform.
func startTied(cmd *exec.Cmd) error { return cmd.Start() }

func registryInstallDir() string { return "" }
