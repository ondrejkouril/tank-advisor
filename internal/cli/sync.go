package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/secrets"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/syncer"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
	"github.com/ondrejkouril/tank-advisor/internal/xvm"
)

func runSync(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "sync")
	only := fs.String("only", "", "sync just one source: wg, wn8 or mod (the client mod's garage dump)")
	full := fs.Bool("full", false, "ignore freshness and refetch everything")
	dryRun := fs.Bool("dry-run", false, "report what would be fetched without making any request or writing anything")
	quiet := fs.Bool("quiet", false, "print nothing unless the sync fails; for hooks and scheduled tasks")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if err := checkSyncSource(*only); err != nil {
		return usageErr(env, err.Error())
	}

	// Quiet silences progress and the report. A failure still reaches the
	// terminal, because main writes errors to the process's stderr directly,
	// and the caveats still reach every later answer through sync_runs.
	if *quiet {
		env.Stdout, env.Stderr = io.Discard, io.Discard
	}

	report, err := env.sync(ctx, syncOptions{Only: *only, Full: *full, DryRun: *dryRun})
	if errors.Is(err, store.ErrSyncRunning) {
		// Not a failure: the other sync is fetching the same data.
		fmt.Fprintln(env.Stdout, "another sync is still running; its data will be in the cache when it finishes")
		return nil
	}
	if err != nil {
		return err
	}
	printSyncReport(env, report, *dryRun)

	if !report.OK() && len(report.Caveats) > 0 {
		return fmt.Errorf("nothing could be synced")
	}
	return nil
}

func checkSyncSource(only string) error {
	switch only {
	case "", "wg", "wn8", "mod":
		return nil
	}
	return fmt.Errorf("unknown source %q, want wg, wn8 or mod", only)
}

// syncLockWait is how long a sync waits for another one to finish. A full sync
// takes seconds; tests shorten it.
var syncLockWait = time.Minute

type syncOptions struct {
	Only   string // "", "wg", "wn8" or "mod"
	Full   bool
	DryRun bool
}

// sync runs one sync and returns its report; the CLI prints it and the MCP
// server returns it. Progress lines go to stderr, which is never the MCP
// protocol stream. The caveats are redacted here, once, for both callers.
func (e *Env) sync(ctx context.Context, opts syncOptions) (syncer.Report, error) {
	if e.ConfigErr != nil {
		return syncer.Report{}, e.ConfigErr
	}
	if !e.Config.Configured() {
		return syncer.Report{}, errNotSetUp
	}
	withWG := opts.Only == "" || opts.Only == "wg"
	withWN8 := opts.Only == "" || opts.Only == "wn8"

	s := &syncer.Syncer{
		Config:    e.Config,
		AccountID: e.Config.Account.AccountID,
		Now:       e.now,
		DryRun:    opts.DryRun,
		Force:     opts.Full,
		Log:       func(line string) { fmt.Fprintln(e.Stderr, line) },
		LockWait:  syncLockWait,
		ModDump:   e.modDumpPath(),
	}

	// A dry run must leave the filesystem exactly as it found it, so it opens
	// the cache only if one already exists - opening creates the file. With no
	// cache, nothing is fresh, which is the right answer anyway.
	if !opts.DryRun || e.HasStore() {
		db, err := e.OpenStore(ctx)
		if err != nil {
			return syncer.Report{}, err
		}
		defer db.Close()
		s.DB = db
	}

	// A dry run needs no credentials either: its whole purpose is to say what
	// would happen without touching anything.
	if !opts.DryRun && withWG {
		client, err := e.wgClient()
		if err != nil {
			return syncer.Report{}, err
		}
		s.WG = client

		// The token is optional. Without it the public endpoints still work, so
		// a sync is worth attempting; the syncer reports the missing private
		// block as a caveat rather than pretending the numbers are zero.
		if token, err := loadToken(e); err == nil && token.Valid(e.now()) {
			s.AccessToken = token.AccessToken
		} else {
			fmt.Fprintln(e.Stderr,
				"no valid access token: credits, free XP and the garage will be missing (run: wotctx auth wg)")
		}
	}
	// XVM needs no credential: it is one public static file.
	if !opts.DryRun && withWN8 {
		s.XVM = xvm.New()
	}

	report, err := s.Sync(ctx, syncer.Only(opts.Only))
	if err != nil {
		return syncer.Report{}, err
	}

	// Overlay research paths go in after the encyclopedia, whose names they
	// resolve against. The file is local, so there is no TTL to respect.
	if !opts.DryRun && withWG {
		caveat, err := loadOverlayEdges(ctx, e, s.DB)
		if err != nil {
			report.Caveats = append(report.Caveats, "overlay: "+err.Error())
		} else if caveat != "" {
			report.Caveats = append(report.Caveats, caveat)
		}
	}

	for i, c := range report.Caveats {
		report.Caveats[i] = e.Redactor().Redact(c)
	}
	return report, nil
}

func printSyncReport(env *Env, report syncer.Report, dryRun bool) {
	verb := "synced"
	if dryRun {
		verb = "would sync"
	}

	if len(report.Synced) > 0 {
		fmt.Fprintf(env.Stdout, "%s: %s\n", verb, strings.Join(report.Synced, ", "))
	}
	if len(report.Skipped) > 0 {
		fmt.Fprintf(env.Stdout, "still fresh: %s\n", strings.Join(report.Skipped, ", "))
	}

	if len(report.Counts) > 0 {
		keys := make([]string, 0, len(report.Counts))
		for k := range report.Counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(env.Stdout, "  %s: %d\n", k, report.Counts[k])
		}
	}

	// Caveats go last and are never silent: a known gap the user does not hear
	// about is indistinguishable from data that simply does not exist.
	for _, caveat := range report.Caveats {
		fmt.Fprintf(env.Stdout, "caveat: %s\n", caveat) // redacted by env.sync
	}

	if !dryRun && !report.Finished.IsZero() {
		fmt.Fprintf(env.Stdout, "took %s\n", report.Finished.Sub(report.Started).Round(100_000_000))
	}
}

// wgClient builds a Wargaming client from the stored credentials.
func (e *Env) wgClient() (*wg.Client, error) {
	appID, _, err := e.applicationID()
	if err != nil {
		return nil, err
	}
	return wg.New(e.Config.Account.Realm, appID)
}

// applicationID returns the Wargaming application id and where it came from:
// the player's own, from the keychain or the environment, wins over the one
// built into a release (docs/spec-desktop.md section 12.1).
func (e *Env) applicationID() (string, string, error) {
	if e.Secrets != nil {
		if id, source, err := e.Secrets.Get(secrets.WGApplicationID); err == nil {
			return id, string(source), nil
		}
	}
	if id := wg.BuiltinApplicationID(); id != "" {
		return id, "built in", nil
	}
	return "", "", errors.New("no Wargaming application_id: this build has none built in\n" +
		"register an application at https://developers.wargaming.net/applications/ " +
		"choosing type Mobile (not Server), then run:\n" +
		"  wotctx auth wg --application-id <id>")
}

// errNotSetUp is what every command that needs an account says without one.
var errNotSetUp = errors.New("no account is set up yet; log in first: wotctx auth wg --realm eu|com|asia (or use the Tank Advisor app)")
