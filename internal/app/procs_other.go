//go:build !windows

package app

// listWotctx finds nothing outside Windows, where the app does not run.
func listWotctx() ([]Proc, error) { return nil, nil }
