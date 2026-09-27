package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func TestPayloadsAreWrittenOnlyWhenTheyDiffer(t *testing.T) {
	dir := t.TempDir()
	payload := fstest.MapFS{
		"README.md":                        {Data: []byte("not a payload")},
		"wotctx.exe":                       {Data: []byte("v1")},
		"wotctx-0.5.0.mcpb":                {Data: []byte("bundle")},
		"ondrejkouril.wotctx_0.3.0.wotmod": {Data: []byte("mod")},
	}
	written, err := InstallPayloads(payload, dir)
	if err != nil || len(written) != 3 {
		t.Fatalf("written %v, %v", written, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); !os.IsNotExist(err) {
		t.Error("the README was installed")
	}
	if written, _ = InstallPayloads(payload, dir); len(written) != 0 {
		t.Errorf("the same payloads were written again: %v", written)
	}

	// An update brings a newer mod and bundle: the old ones go.
	payload = fstest.MapFS{
		"wotctx.exe":                       {Data: []byte("v2")},
		"wotctx-0.6.0.mcpb":                {Data: []byte("bundle 2")},
		"ondrejkouril.wotctx_0.4.0.wotmod": {Data: []byte("mod 2")},
	}
	if written, _ = InstallPayloads(payload, dir); len(written) != 3 {
		t.Errorf("written %v", written)
	}
	for _, gone := range []string{"wotctx-0.5.0.mcpb", "ondrejkouril.wotctx_0.3.0.wotmod"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s is still there", gone)
		}
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "wotctx.exe")); string(raw) != "v2" {
		t.Errorf("wotctx.exe = %q", raw)
	}
}

// TestARunningExecutableIsRenamedAside replaces an executable while it runs,
// as an update does while Claude Desktop holds wotctx.exe.
func TestARunningExecutableIsRenamedAside(t *testing.T) {
	ping, err := exec.LookPath("ping")
	if err != nil {
		t.Skip("no ping to run")
	}
	dir := t.TempDir()
	raw, _ := os.ReadFile(ping)
	target := filepath.Join(dir, "wotctx.exe")
	os.WriteFile(target, raw, 0o755)
	cmd := exec.Command(target, "-n", "4", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot run the copy:", err)
	}
	defer cmd.Wait()
	time.Sleep(200 * time.Millisecond)

	if _, err := InstallPayloads(fstest.MapFS{"wotctx.exe": {Data: []byte("new")}}, dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Error("the new executable is not in place")
	}
	if _, err := os.Stat(filepath.Join(dir, "wotctx.old.exe")); err != nil {
		t.Error("the running one was not renamed aside")
	}
	cmd.Wait()
	CleanOldPayloads(dir)
	if _, err := os.Stat(filepath.Join(dir, "wotctx.old.exe")); !os.IsNotExist(err) {
		t.Error("the old copy was not cleaned up once it stopped")
	}
}

func TestUninstallTakesTheModOutAndKeepsTheDataUnlessAsked(t *testing.T) {
	f := newFixture(t, true)
	f.modPackage(t, "0.3.0")
	f.do(t, "mod-install", "")
	local := t.TempDir()
	os.MkdirAll(filepath.Join(local, "Tank Advisor", "WebView2"), 0o755)
	write(t, f.paths.DBFile, "db")

	r := f.svc.Uninstall(context.Background(), false, local)
	if len(r.Problems) != 0 {
		t.Fatalf("problems: %v", r.Problems)
	}
	if _, err := os.Stat(filepath.Join(f.game, "mods", "2.4.0.1", "ondrejkouril.wotctx_0.3.0.wotmod")); !os.IsNotExist(err) {
		t.Error("the mod is still in the game")
	}
	if _, err := os.Stat(filepath.Join(local, "Tank Advisor")); !os.IsNotExist(err) {
		t.Error("the browser cache is still there")
	}
	if _, err := os.Stat(f.paths.DBFile); err != nil {
		t.Error("the data went without being asked")
	}
	if cfg := f.config(t); cfg.Mod.Managed || cfg.Account.AccountID == 0 {
		t.Errorf("config = %+v", cfg)
	}

	f.svc.Uninstall(context.Background(), true, local)
	if _, err := os.Stat(f.paths.DBFile); !os.IsNotExist(err) {
		t.Error("asked to, the data is still there")
	}
}
