// Command wotctx-launcher is what the Claude Desktop extension runs. It starts
// the wotctx that Tank Advisor installed, as `wotctx mcp`, on the launcher's
// own stdin and stdout, and exits when it does (docs/spec-desktop.md section
// 7.1).
//
// The extension therefore never carries a wotctx of its own: an app update
// reaches Claude Desktop at its next start with no extension reinstall, and
// only one wotctx binary ever opens the cache.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is set at build time.
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	path, tried := locate()
	if path == "" {
		// Serve a single tool that says what is wrong, rather than failing
		// silently: Claude Desktop shows a crashed server only in its logs.
		if err := serveMissing(context.Background(), &mcp.StdioTransport{}, tried); err != nil {
			fmt.Fprintln(os.Stderr, "wotctx-launcher:", err)
			return 1
		}
		return 0
	}

	cmd := exec.Command(path, append([]string{"mcp"}, os.Args[1:]...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := startTied(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "wotctx-launcher: starting %s: %v\n", path, err)
		return 1
	}
	err := cmd.Wait()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	default:
		fmt.Fprintf(os.Stderr, "wotctx-launcher: %v\n", err)
		return 1
	}
}

// Where to look, in order. Each is replaced in tests.
var (
	// installDir is where the Tank Advisor installer recorded itself, from
	// HKCU\Software\Tank Advisor, value InstallDir.
	installDir = registryInstallDir
	// defaultDir is the installer's default: %LOCALAPPDATA%\Programs\Tank Advisor.
	defaultDir = func() string {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "Programs", "Tank Advisor")
		}
		return ""
	}
	lookPath = exec.LookPath
)

// locate finds wotctx: WOTCTX_EXE if set, then the installed app, then the
// installer's default folder, then the PATH (a developer's `make install`).
// It returns the places tried, for the message when none has it.
func locate() (string, []string) {
	var tried []string
	candidate := func(p string) bool {
		if p == "" {
			return false
		}
		tried = append(tried, p)
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	}
	exe := "wotctx.exe"
	if env := os.Getenv("WOTCTX_EXE"); candidate(env) {
		return env, tried
	}
	for _, dir := range []string{installDir(), defaultDir()} {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, exe); candidate(p) {
			return p, tried
		}
	}
	if p, err := lookPath("wotctx"); err == nil && candidate(p) {
		return p, tried
	}
	tried = append(tried, "wotctx on the PATH")
	return "", tried
}
