package cli

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGame is a client folder as the installer leaves it: version.xml (the
// real one's format) and an existing mods/2.4.0.1/.
func fakeGame(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	xml := "<version.xml>\r\n\t<version> v.2.4.0.1 #952 </version>\r\n</version.xml>"
	if err := os.WriteFile(filepath.Join(dir, "version.xml"), []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "mods", "2.4.0.1"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fakePackage writes a package with the given file names and method.
func fakePackage(t *testing.T, name string, method uint16, files ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, file := range files {
		w, err := z.CreateHeader(&zip.FileHeader{Name: file, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("x"))
	}
	z.Close()
	f.Close()
	return path
}

const goodScript = "res/scripts/client/gui/mods/mod_wotctx.pyc"

func TestModInstallReplacesTheOldPackage(t *testing.T) {
	gameDir := fakeGame(t)
	modsDir := filepath.Join(gameDir, "mods", "2.4.0.1")
	old := filepath.Join(modsDir, "ondrejkouril.wotctx_0.0.9.wotmod")
	other := filepath.Join(modsDir, "com.modxvm.xvm_13.1.0.0090.wotmod")
	for _, p := range []string{old, other} {
		os.WriteFile(p, nil, 0o644)
	}
	pkg := fakePackage(t, "ondrejkouril.wotctx_0.1.0.wotmod", zip.Store, "meta.xml", goodScript)

	env, buf := newTestEnv(t)
	if err := Run(context.Background(), env, []string{"mod", "install", "--game-dir", gameDir, pkg}); err != nil {
		t.Fatalf("mod install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modsDir, "ondrejkouril.wotctx_0.1.0.wotmod")); err != nil {
		t.Errorf("the package was not installed: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("the old package of this mod was left beside the new one")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("another mod's package was touched")
	}
	if !strings.Contains(buf.String(), "garage.json") {
		t.Errorf("output does not say where the dump will appear:\n%s", buf.String())
	}
}

func TestModInstallRefusesABadPackage(t *testing.T) {
	cases := map[string]string{
		"compressed": fakePackage(t, "ondrejkouril.wotctx_0.1.0.wotmod", zip.Deflate, "meta.xml", goodScript),
		"no script":  fakePackage(t, "ondrejkouril.wotctx_0.1.0.wotmod", zip.Store, "meta.xml"),
		"other mod":  fakePackage(t, "com.modxvm.xvm_1.wotmod", zip.Store, "meta.xml", goodScript),
	}
	for name, pkg := range cases {
		gameDir := fakeGame(t)
		env, _ := newTestEnv(t)
		if err := Run(context.Background(), env, []string{"mod", "install", "--game-dir", gameDir, pkg}); err == nil {
			t.Errorf("%s: mod install = nil, want a refusal", name)
		}
		entries, _ := os.ReadDir(filepath.Join(gameDir, "mods", "2.4.0.1"))
		if len(entries) != 0 {
			t.Errorf("%s: something was copied into the mods folder", name)
		}
	}
}

// TestModInstallCreatesTheVersionFolder: after a game update the new mods
// folder may not exist until the client first starts. Creating it puts the
// mod in place before that start (docs/spec-desktop.md section 6.2).
func TestModInstallCreatesTheVersionFolder(t *testing.T) {
	gameDir := fakeGame(t)
	os.RemoveAll(filepath.Join(gameDir, "mods"))
	pkg := fakePackage(t, "ondrejkouril.wotctx_0.1.0.wotmod", zip.Store, "meta.xml", goodScript)
	env, _ := newTestEnv(t)
	if err := Run(context.Background(), env, []string{"mod", "install", "--game-dir", gameDir, pkg}); err != nil {
		t.Fatalf("mod install with no mods folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", "2.4.0.1", "ondrejkouril.wotctx_0.1.0.wotmod")); err != nil {
		t.Errorf("the package is not in the new folder: %v", err)
	}
}
