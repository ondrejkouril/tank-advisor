package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/ondrejkouril/tank-advisor/internal/cli"
	"github.com/ondrejkouril/tank-advisor/internal/config"
)

// errNoElevation is what a host without elevation reports.
var errNoElevation = errors.New("administrator permission is not available here")

// installMod puts the bundled mod into the game's current mods folder, and
// from then on the app keeps it there (docs/spec-desktop.md section 6.2). A
// game folder the player cannot write to (a Program Files install) is
// written with administrator permission, which Windows asks for, for that
// one copy.
func (s *Service) installMod(ctx context.Context) Result {
	pkg := s.modPackage()
	if pkg == "" {
		return Result{Message: "This build carries no mod package."}
	}
	st := s.modState(ctx)
	if st.gameErr != nil {
		return Result{Message: "World of Tanks was not found: " + st.gameErr.Error()}
	}

	const done = "The mod is installed. Start the game and wait in the garage for a few seconds."
	var r Result
	if canWrite(st.modsDir) {
		r = s.wotctx(ctx, done, "mod", "install", "--game-dir", st.gameDir, pkg)
	} else {
		r = s.installElevated(pkg, st.gameDir, done)
	}
	if r.OK {
		if err := s.recordPlaced(st.modsDir); err != nil {
			r.Message += " (Could not note it in the settings: " + err.Error() + ")"
		}
	}
	return r
}

func (s *Service) installElevated(pkg, gameDir, done string) Result {
	if s.opts.Host == nil {
		return Result{Message: gameDir + " cannot be written to, and " + errNoElevation.Error() + "."}
	}
	if err := s.opts.Host.InstallModElevated(pkg, gameDir); err != nil {
		return Result{Message: "The game folder needs administrator permission, and the copy with it did not work: " + err.Error()}
	}
	return Result{OK: true, Message: done}
}

// recordPlaced marks the mod as managed and notes the folder, so the
// uninstaller can take it out of every folder it went into (section 4).
func (s *Service) recordPlaced(modsDir string) error {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	placed := env.Config.Mod.Placed
	if !containsDir(placed, modsDir) {
		placed = append(placed, modsDir)
	}
	return config.Set(env.Paths.ConfigFile,
		config.Update{Key: "mod.managed", Value: true},
		config.Update{Key: "mod.placed", Value: placed},
	)
}

// KeepModInstalled is the mod upkeep of docs/spec-desktop.md section 5.6: when
// the player has had the app install the mod, and the game's current mods
// folder lacks the bundled package (a game update made a new folder, or an
// app update brought a newer mod), it installs it again. It returns what it
// did, or "" when there was nothing to do. It never asks for permission by
// itself: a folder that needs it waits for the player's click, and the status
// window says so.
func (s *Service) KeepModInstalled(ctx context.Context) string {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	if env.ConfigErr != nil || !env.Config.Mod.Managed {
		return ""
	}
	pkg := s.modPackage()
	if pkg == "" {
		return ""
	}
	st := s.modState(ctx)
	if st.gameErr != nil || contains(st.installed, filepath.Base(pkg)) {
		s.setModNeedsPermission(false)
		return ""
	}
	if !canWrite(st.modsDir) {
		s.setModNeedsPermission(true)
		return ""
	}
	// A click in the window comes first; the next round tries again.
	if !s.busy.TryLock() {
		return ""
	}
	defer s.busy.Unlock()
	r := s.wotctx(ctx, "", "mod", "install", "--game-dir", st.gameDir, pkg)
	if !r.OK {
		// Typically the game is running and holds the old package; the next
		// round tries again.
		return ""
	}
	if err := s.recordPlaced(st.modsDir); err != nil {
		return "Installed the mod for game " + st.gameVersion + ", but could not note it: " + err.Error()
	}
	return "Installed the mod for game " + st.gameVersion + "."
}

func (s *Service) setModNeedsPermission(v bool) {
	s.mu.Lock()
	s.modNeedsPermission = v
	s.mu.Unlock()
}

// canWrite is writable, replaced in tests: a protected folder cannot be made
// on every machine that runs them.
var canWrite = writable

// writable reports whether files can be created in dir, making it first if
// needed, as mod install does.
func writable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".tankadvisor-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

func containsDir(list []string, dir string) bool {
	for _, d := range list {
		if sameDir(d, dir) {
			return true
		}
	}
	return false
}

// ElevatedModInstall is what the app runs, as its own process with
// administrator permission, to copy the mod into a protected game folder. It
// returns the exit code.
func ElevatedModInstall(ctx context.Context, version, pkg, gameDir string) int {
	env := cli.Bootstrap(io.Discard, io.Discard, version)
	if err := cli.Run(ctx, env, []string{"mod", "install", "--game-dir", gameDir, pkg}); err != nil {
		return 1
	}
	return 0
}
