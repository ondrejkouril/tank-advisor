//go:build !windows

package main

import "errors"

// The app is Windows-only (docs/spec-desktop.md section 1); this keeps the
// module building elsewhere.
func showInFolder(string) error { return errors.New("not supported on this system") }

func installModElevated(string, string) error {
	return errors.New("not supported on this system")
}
