package cli

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/game"
)

// runModInstall copies the client mod package into the installed game's mods
// folder for its current version. Each game update starts a new, empty
// mods/<version>/ folder, so this is run again after every update
// (docs/plan.md, phase 4). Older packages of this mod in that folder are
// removed first: two copies would both load.
func runModInstall(_ context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "mod install")
	gameDir := fs.String("game-dir", "", "the World of Tanks folder (default: game_dir from the config, else the one Game Center records)")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if fs.NArg() != 1 {
		return usageErr(env, "mod install takes the .wotmod package to install (make mod builds it into dist/)")
	}
	pkg := fs.Arg(0)
	dir := *gameDir
	if dir == "" {
		found, err := env.gameDir()
		if err != nil {
			return err
		}
		dir = found
	}

	if err := checkModPackage(pkg); err != nil {
		return err
	}
	modsDir, err := game.ModsDir(dir)
	if err != nil {
		return err
	}
	// After an update the folder paths.xml names may not exist until the
	// client first starts. Creating it lets the mod be in place before that
	// first start (docs/spec-desktop.md section 6.2).
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", modsDir, err)
	}

	old, err := game.InstalledPackages(modsDir)
	if err != nil {
		return err
	}
	for _, name := range old {
		if err := os.Remove(filepath.Join(modsDir, name)); err != nil {
			return fmt.Errorf("removing the old %s: %w (is the game running?)", name, err)
		}
		fmt.Fprintf(env.Stdout, "removed %s\n", name)
	}

	dest := filepath.Join(modsDir, filepath.Base(pkg))
	if err := copyFile(pkg, dest); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "installed %s\n", dest)
	fmt.Fprintf(env.Stdout, "Start the game and wait in the garage: the dump appears at %s\n",
		filepath.Join(env.Paths.DataDir, "mod", "garage.json"))
	return nil
}

// checkModPackage refuses anything but this mod's package, so a slip of the
// argument cannot drop some other file into the client's mods folder.
func checkModPackage(pkg string) error {
	name := filepath.Base(pkg)
	if !strings.HasPrefix(name, game.ModID+"_") || !strings.HasSuffix(name, ".wotmod") {
		return fmt.Errorf("%s is not a %s package (want %s_<version>.wotmod)", name, game.ModID, game.ModID)
	}
	z, err := zip.OpenReader(pkg)
	if err != nil {
		return fmt.Errorf("opening %s: %w", pkg, err)
	}
	defer z.Close()
	var hasMeta, hasScript bool
	for _, f := range z.File {
		// The client reads only uncompressed packages.
		if f.Method != zip.Store {
			return fmt.Errorf("%s: %s is compressed; the client needs a stored (uncompressed) zip", name, f.Name)
		}
		hasMeta = hasMeta || f.Name == "meta.xml"
		hasScript = hasScript || f.Name == "res/scripts/client/gui/mods/mod_wotctx.pyc"
	}
	if !hasMeta || !hasScript {
		return fmt.Errorf("%s lacks meta.xml or mod_wotctx.pyc; rebuild it with make mod", name)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
