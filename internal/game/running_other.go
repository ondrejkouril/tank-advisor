//go:build !windows

package game

import "context"

// runningClients finds nothing where the game does not run.
func runningClients() []string { return nil }

// RunningClients finds nothing where the game does not run.
func RunningClients() []Process { return nil }

// WaitForExit has nothing to wait for where the game does not run.
func WaitForExit(context.Context, uint32) error { return nil }
