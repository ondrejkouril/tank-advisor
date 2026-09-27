package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLocateOrder(t *testing.T) {
	installed, fallback := t.TempDir(), t.TempDir()
	for _, dir := range []string{installed, fallback} {
		os.WriteFile(filepath.Join(dir, "wotctx.exe"), []byte("x"), 0o755)
	}
	defer func(i, d func() string, l func(string) (string, error)) { installDir, defaultDir, lookPath = i, d, l }(installDir, defaultDir, lookPath)
	installDir = func() string { return installed }
	defaultDir = func() string { return fallback }
	lookPath = func(string) (string, error) { return "", errors.New("not on the PATH") }
	t.Setenv("WOTCTX_EXE", "")

	if got, _ := locate(); got != filepath.Join(installed, "wotctx.exe") {
		t.Errorf("locate = %q, want the installed app first", got)
	}
	installDir = func() string { return "" }
	if got, _ := locate(); got != filepath.Join(fallback, "wotctx.exe") {
		t.Errorf("locate = %q, want the default folder next", got)
	}
	defaultDir = func() string { return t.TempDir() }
	got, tried := locate()
	if got != "" || len(tried) == 0 || !strings.Contains(strings.Join(tried, ";"), "PATH") {
		t.Errorf("locate with nothing installed = %q, tried %v", got, tried)
	}
	override := filepath.Join(fallback, "wotctx.exe")
	t.Setenv("WOTCTX_EXE", override)
	if got, _ := locate(); got != override {
		t.Errorf("WOTCTX_EXE is not honoured: %q", got)
	}
}

// TestMissingAppAnswersWithTheFix: with no app, the extension still answers,
// with one failing check that names the fix.
func TestMissingAppAnswersWithTheFix(t *testing.T) {
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	go serveMissing(ctx, serverT, []string{`C:\nowhere\wotctx.exe`})

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "wot_data_status"})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{`"status":"fail"`, "Tank Advisor", "releases", `nowhere`} {
		if !strings.Contains(text, want) {
			t.Errorf("result lacks %q: %s", want, text)
		}
	}
}

// TestLauncherRunsTheInstalledWotctx builds both binaries and speaks MCP to
// wotctx through the launcher, as Claude Desktop does: every tool of the real
// server must come through.
func TestLauncherRunsTheInstalledWotctx(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two binaries")
	}
	dir := t.TempDir()
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	launcher, wotctx := filepath.Join(dir, "wotctx-launcher"+exe), filepath.Join(dir, "wotctx"+exe)
	for out, pkg := range map[string]string{launcher: ".", wotctx: "../wotctx"} {
		if b, err := exec.Command("go", "build", "-o", out, pkg).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", pkg, err, b)
		}
	}

	cmd := exec.Command(launcher)
	cmd.Env = append(os.Environ(), "WOTCTX_EXE="+wotctx, "WOTCTX_CONFIG_DIR="+t.TempDir(), "WOTCTX_DATA_DIR="+t.TempDir())
	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if len(names) != 12 || !strings.Contains(strings.Join(names, " "), "wot_guide") {
		t.Errorf("tools through the launcher = %v, want wotctx's twelve", names)
	}
}

// TestEndingTheLauncherEndsWotctx: Claude Desktop stops an extension by
// ending the launcher. wotctx must go with it, or it keeps its own executable
// locked against the next update. A running .exe cannot be deleted on
// Windows, so deleting it proves it has exited.
func TestEndingTheLauncherEndsWotctx(t *testing.T) {
	if runtime.GOOS != "windows" || testing.Short() {
		t.Skip("the job object is Windows-only")
	}
	dir := t.TempDir()
	launcher, wotctx := filepath.Join(dir, "wotctx-launcher.exe"), filepath.Join(dir, "wotctx.exe")
	for out, pkg := range map[string]string{launcher: ".", wotctx: "../wotctx"} {
		if b, err := exec.Command("go", "build", "-o", out, pkg).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", pkg, err, b)
		}
	}
	cmd := exec.Command(launcher)
	cmd.Env = append(os.Environ(), "WOTCTX_EXE="+wotctx, "WOTCTX_CONFIG_DIR="+t.TempDir(), "WOTCTX_DATA_DIR="+t.TempDir())
	stdin, _ := cmd.StdinPipe() // held open: wotctx mcp waits on it
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond) // let wotctx start
	if err := os.Remove(wotctx); err == nil {
		t.Fatal("wotctx.exe could be deleted while it should be running; the test proves nothing")
	}
	cmd.Process.Kill()
	cmd.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.Remove(wotctx)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("wotctx.exe is still running after the launcher ended: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
