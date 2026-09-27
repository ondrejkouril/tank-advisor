package app

import (
	"os/exec"
	"strings"
	"testing"
)

// TestWotctxDoesNotLinkTheWindowToolkit keeps Wails in cmd/tankadvisor: the
// CLI and MCP server keep their own short dependency list
// (docs/spec-desktop.md section 3). internal/app itself must stay free of it
// too, so its logic can be tested without a window.
func TestWotctxDoesNotLinkTheWindowToolkit(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	for _, pkg := range []string{"./cmd/wotctx", "./cmd/wotctx-launcher", "./internal/app"} {
		cmd := exec.Command(gobin, "list", "-deps", pkg)
		cmd.Dir = "../.."
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for _, dep := range strings.Fields(string(out)) {
			if strings.HasPrefix(dep, "github.com/wailsapp/") {
				t.Errorf("%s depends on %s", pkg, dep)
			}
		}
	}
}
