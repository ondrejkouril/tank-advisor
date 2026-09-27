package game

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixture is the installed client's own version.xml (EU 2.4.0.1 #952),
// CRLF line endings and tabs included.
func TestVersionReadsTheClientsFile(t *testing.T) {
	got, err := Version(filepath.Join("..", "..", "testdata", "game"))
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if got != "2.4.0.1" {
		t.Errorf("Version = %q, want 2.4.0.1", got)
	}
}

func TestModsDir(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "game")
	got, err := ModsDir(dir)
	if err != nil {
		t.Fatalf("ModsDir: %v", err)
	}
	if want := filepath.Join(dir, "mods", "2.4.0.1"); got != want {
		t.Errorf("ModsDir = %q, want %q", got, want)
	}
}

func TestVersionWithoutAGameSaysSo(t *testing.T) {
	_, err := Version(t.TempDir())
	if err == nil {
		t.Fatal("Version of an empty folder = nil error")
	}
}

func TestInstalledPackagesFindsOnlyThisMod(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		ModID + "_0.1.0.wotmod",
		ModID + "_0.2.0.wotmod",
		"com.modxvm.xvm_13.1.0.0090.wotmod",
		ModID + "_0.1.0.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := InstalledPackages(dir)
	if err != nil {
		t.Fatalf("InstalledPackages: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("InstalledPackages = %v, want this mod's two packages", got)
	}
}
