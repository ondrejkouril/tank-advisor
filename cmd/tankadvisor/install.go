package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/app"
)

// payload is what a release build carries (cmd/tankadvisor/payload, filled by
// make installer): wotctx.exe, the launcher, the Desktop bundle, the mod.
//
//go:embed payload
var payload embed.FS

// payloadsCarried reports whether this build carries payloads: a release
// build does, a development build carries only the folder's README.
func payloadsCarried() (bool, fs.FS) {
	sub, err := fs.Sub(payload, "payload")
	if err != nil {
		return false, nil
	}
	_, err = fs.Stat(sub, "wotctx.exe")
	return err == nil, sub
}

// setUpPayloads writes out the payloads beside the executable, clears copies
// an earlier update renamed aside, and records where the app is for the
// Claude Desktop launcher. Failures are not fatal: the app still runs.
func setUpPayloads() error {
	ok, sub := payloadsCarried()
	if !ok {
		return nil
	}
	dir := payloadDir()
	app.CleanOldPayloads(dir)
	if _, err := app.InstallPayloads(sub, dir); err != nil {
		return err
	}
	return app.RecordInstallDir(dir)
}

// runInstallPayloads is the installer's step: the payloads, and the PATH.
func runInstallPayloads() int {
	if err := setUpPayloads(); err != nil {
		fmt.Fprintln(os.Stderr, "tankadvisor:", err)
		return 1
	}
	if err := app.AddToUserPath(payloadDir()); err != nil {
		fmt.Fprintln(os.Stderr, "tankadvisor:", err)
		return 1
	}
	return 0
}

// runUninstall is the uninstaller's step: everything the app put outside its
// folder. The installer then removes the folder and its own entries. What
// went wrong is written to uninstall.log in the temp folder, for support.
func runUninstall(deleteData bool) int {
	svc := app.New(app.Options{Version: version, PayloadDir: payloadDir()})
	report := svc.Uninstall(context.Background(), deleteData, os.Getenv("LOCALAPPDATA"))
	for _, err := range app.RemoveRegistration(payloadDir()) {
		report.Problems = append(report.Problems, err.Error())
	}
	var b strings.Builder
	for _, r := range report.Removed {
		fmt.Fprintln(&b, "removed", r)
	}
	for _, p := range report.Problems {
		fmt.Fprintln(&b, "problem:", p)
	}
	os.WriteFile(filepath.Join(os.TempDir(), "tankadvisor-uninstall.log"), []byte(b.String()), 0o644)
	if len(report.Problems) > 0 {
		return 1
	}
	return 0
}
