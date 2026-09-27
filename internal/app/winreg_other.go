//go:build !windows

package app

// The app is Windows-only; these keep the module building elsewhere.

func AddToUserPath(string) error        { return nil }
func RemoveFromUserPath(string) error   { return nil }
func RecordInstallDir(string) error     { return nil }
func RemoveRegistration(string) []error { return nil }
