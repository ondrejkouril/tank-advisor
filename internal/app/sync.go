package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/cli"
)

// sync runs wotctx sync and says how it really went. The sync counts a
// source that failed as a caveat, not a failure, so "done" alone could hide
// that nothing of the account came back: a blocked application id fails
// every Wargaming source and still finishes.
func (s *Service) sync(ctx context.Context) Result {
	r := s.wotctx(ctx, "Synced.", "sync")
	if !r.OK {
		return r
	}
	env := s.opts.NewEnv(io.Discard, io.Discard)
	db, err := env.OpenStore(ctx)
	if err != nil {
		return r
	}
	defer db.Close()
	var notes []string
	if run, err := db.LatestSyncRun(ctx); err == nil {
		notes = run.NoteLines()
	}
	if !hasAccountData(ctx, env) {
		r.OK = false
		r.Message = "The sync ran, but no account data came back from Wargaming."
		if note := accountNote(notes); note != "" {
			r.Message += " " + env.Redactor().Redact(note)
		}
		return r
	}
	if len(notes) > 0 {
		r.Message = fmt.Sprintf("Synced, with %s: %s", plural(len(notes), "problem", "problems"), env.Redactor().Redact(notes[0]))
	}
	return r
}

// accountNote picks the caveat about the account itself, which says why no
// account data came back, or else the first one.
func accountNote(notes []string) string {
	for _, n := range notes {
		if strings.HasPrefix(n, "wg:account/info") {
			return n
		}
	}
	if len(notes) > 0 {
		return notes[0]
	}
	return ""
}

// hasAccountData reports whether the cache holds the account itself, the one
// source without which no answer can be grounded.
func hasAccountData(ctx context.Context, env *cli.Env) bool {
	if !env.HasStore() {
		return false
	}
	db, err := env.OpenStore(ctx)
	if err != nil {
		return false
	}
	defer db.Close()
	_, err = db.LatestUsableSnapshot(ctx, "wg", "account/info")
	return err == nil
}
