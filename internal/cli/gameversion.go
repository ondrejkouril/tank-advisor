package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/game"
	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// checkGameVersion sets the server's game version, as Wargaming's
// encyclopedia reports it, beside the installed client's (docs/plan.md
// phase 6, step S5). A version change between two syncs dates a release; the
// version in use when recording began is undated.
func checkGameVersion(ctx context.Context, env *Env) check {
	const name = "game-version"
	if env.ConfigErr != nil {
		return check{Name: name, Status: statusUnknown, Detail: "config unresolved"}
	}
	if !env.HasStore() {
		return check{Name: name, Status: statusUnknown, Detail: "no cache yet"}
	}
	db, err := env.OpenStore(ctx)
	if err != nil {
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}
	defer db.Close()

	v, err := db.GameVersionState(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return check{Name: name, Status: statusWarn, Detail: "the server's game version has not been synced yet", Fix: "wotctx sync --only wg"}
	}
	if err != nil {
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}
	detail := "server " + v.Describe()

	dir, err := env.gameDir()
	var client string
	if err == nil {
		client, err = game.Version(dir)
	}
	if err != nil {
		return check{Name: name, Status: statusOK, Detail: detail + "; installed client unknown (" + env.Redactor().RedactError(err) + ")"}
	}
	detail += "; installed client " + client
	if !sameRelease(v.Current.Version, client) {
		return check{Name: name, Status: statusWarn, Detail: detail + ": they differ",
			Fix: "start the game so it updates"}
	}
	return check{Name: name, Status: statusOK, Detail: detail}
}

// sameRelease reports whether the client's four-part version ("2.4.0.1")
// belongs to the server's release as Wargaming writes it ("2.4").
func sameRelease(server, client string) bool {
	return client == server || strings.HasPrefix(client, server+".")
}
