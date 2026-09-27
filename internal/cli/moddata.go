package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/game"
	"github.com/ondrejkouril/tank-advisor/internal/mod"
	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// modInstallFix is what to run when the mod is missing from the game's
// current mods folder, which every game update causes.
const modInstallFix = "make mod-install (in the repository), or: wotctx mod install <package.wotmod>"

// gameLocator is replaced in tests so they never read this machine's Game
// Center.
var gameLocator = game.DefaultLocator

// gameDir is the World of Tanks folder: game_dir from the config when set,
// otherwise the production client Game Center (or Steam, or the default
// folder) has for the account's realm (docs/spec-desktop.md section 6.1).
func (e *Env) gameDir() (string, error) {
	if e.Config.GameDir != "" {
		return e.Config.GameDir, nil
	}
	found := gameLocator().Find(e.Config.Account.Realm)
	if len(found) == 0 {
		return "", fmt.Errorf("no World of Tanks client for %s found; set game_dir in %s, or pass --game-dir",
			e.Config.Account.Realm, e.Paths.ConfigFile)
	}
	return found[0].Dir, nil
}

// modDumpPath is where the client mod writes, beside the cache.
func (e *Env) modDumpPath() string {
	if e.Paths.DataDir == "" {
		return ""
	}
	return filepath.Join(e.Paths.DataDir, "mod", "garage.json")
}

// checkModData reports the client mod's dump: whether there is one, how old
// it is, and whether it still describes the account.
//
// The dump is only as fresh as the last time the game sat in the garage, and
// no sync can refresh it. Its age alone therefore means little - a dump from
// yesterday is exact if nothing has been played since. What makes it stale is
// a battle after it, which the Wargaming data shows, or a game update the mod
// has not been reinstalled for.
func checkModData(ctx context.Context, env *Env) check {
	const name = "mod-data"
	if env.ConfigErr != nil {
		return check{Name: name, Status: statusUnknown, Detail: "config unresolved"}
	}

	// What the game folder says: its version, and whether the mod is there.
	gameDir, gameErr := env.gameDir()
	var gameVersion, modsDir string
	if gameErr == nil {
		gameVersion, gameErr = game.Version(gameDir)
	}
	if gameErr == nil {
		modsDir, gameErr = game.ModsDir(gameDir)
	}
	installed := false
	if gameErr == nil {
		if pkgs, err := game.InstalledPackages(modsDir); err == nil {
			installed = len(pkgs) > 0
		}
	}

	var (
		acct    store.ModAccount
		acctErr = store.ErrNotFound
		played  = false
	)
	if env.HasStore() {
		db, err := env.OpenStore(ctx)
		if err != nil {
			return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
		}
		defer db.Close()
		acct, acctErr = db.LatestModAccount(ctx)
		if acctErr == nil {
			if state, err := db.LatestAccountState(ctx); err == nil && state.LastBattleTime.After(acct.CapturedAt) {
				played = true
			}
		}
	}
	if acctErr != nil && !errors.Is(acctErr, store.ErrNotFound) {
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(acctErr)}
	}

	// A dump the mod has written but sync has not yet taken.
	fileNewer := false
	if raw, err := os.ReadFile(env.modDumpPath()); err == nil {
		if d, err := mod.Parse(raw); err == nil && (acctErr != nil || d.CapturedAt.After(acct.CapturedAt)) {
			fileNewer = true
		}
	}

	if acctErr != nil {
		switch {
		case fileNewer:
			return check{Name: name, Status: statusWarn,
				Detail: "the client mod has written a dump that is not synced yet", Fix: "wotctx sync"}
		case installed:
			return check{Name: name, Status: statusWarn,
				Detail: fmt.Sprintf("client mod installed for game %s, but no dump yet", gameVersion),
				Fix:    "start the game and wait in the garage, then: wotctx sync"}
		default:
			return check{Name: name, Status: statusUnknown,
				Detail: "client mod not installed: vehicle XP, marks, loadouts and crew are unavailable",
				Fix:    modInstallFix}
		}
	}

	detail := fmt.Sprintf("dump from %s (%s) by mod %s on game %s",
		acct.CapturedAt.UTC().Format("2006-01-02 15:04 UTC"),
		humanizeAge(int64(env.now().Sub(acct.CapturedAt).Seconds())), acct.ModVersion, acct.GameVersion)

	var problems []string
	fix := ""
	if fileNewer {
		problems = append(problems, "a newer dump is not synced yet")
		fix = "wotctx sync"
	}
	if played {
		problems = append(problems, "battles were played after it, so vehicle XP and marks are behind")
		if fix == "" {
			fix = "start the game and wait in the garage, then: wotctx sync"
		}
	}
	if gameErr == nil && acct.GameVersion != gameVersion {
		problems = append(problems, fmt.Sprintf("it predates the installed game %s", gameVersion))
	}
	if gameErr == nil && !installed {
		rel, err := filepath.Rel(gameDir, modsDir)
		if err != nil {
			rel = modsDir
		}
		problems = append(problems, fmt.Sprintf("the mod is not in the game's %s folder (a game update starts a new one)", filepath.ToSlash(rel)))
		fix = modInstallFix
	}
	if n := len(acct.Errors); n > 0 {
		problems = append(problems, fmt.Sprintf("the mod could not read %d part(s): %s", n, acct.Errors[0]))
	}
	if gameErr != nil {
		detail += "; game folder unreadable: " + gameErr.Error()
	}

	if len(problems) == 0 {
		return check{Name: name, Status: statusOK, Detail: detail}
	}
	return check{Name: name, Status: statusWarn, Detail: detail + "; " + strings.Join(problems, "; "), Fix: fix}
}
