package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/secrets"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/syncer"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// checkStatus is the outcome of one doctor check.
type checkStatus string

const (
	statusOK      checkStatus = "ok"
	statusUnknown checkStatus = "unknown" // not implemented yet
	statusWarn    checkStatus = "warn"    // usable, but needs attention
	statusFail    checkStatus = "fail"    // blocks normal operation
)

// check is one line of doctor output.
//
// Detail is written to stdout and read back by the skill, so it must never
// contain secret material. Every detail string that could have touched a
// credential goes through the redactor first.
type check struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail"`
	// Fix is the command to run when Status is warn or fail, so the skill can
	// tell the user what to do instead of guessing.
	Fix string `json:"fix,omitempty"`
}

// doctorReport is the shape `doctor --json` emits. It is deliberately not the
// query provenance envelope: doctor reports on the cache, not from it.
type doctorReport struct {
	Version string  `json:"version"`
	Checks  []check `json:"checks"`
}

func runDoctor(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "doctor")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}

	report := doctorReport{Version: env.Version, Checks: collectChecks(ctx, env)}

	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("encoding report: %w", err)
		}
	} else {
		tw := tabwriter.NewWriter(env.Stdout, 0, 8, 2, ' ', 0)
		for _, c := range report.Checks {
			detail := c.Detail
			if c.Fix != "" {
				detail += "  ->  " + c.Fix
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Status, c.Name, detail)
		}
		tw.Flush()
	}

	for _, c := range report.Checks {
		if c.Status == statusFail {
			return errors.New("one or more checks failed")
		}
	}
	return nil
}

// collectChecks runs every registered check. Each later plan step replaces one
// of the `unknown` placeholders with a real check, so doctor output tracks
// progress honestly instead of claiming health it cannot verify.
func collectChecks(ctx context.Context, env *Env) []check {
	checks := []check{
		{
			Name:   "build",
			Status: statusOK,
			Detail: fmt.Sprintf("wotctx %s, %s/%s, built with %s",
				env.Version, runtime.GOOS, runtime.GOARCH, runtime.Version()),
		},
		checkConfig(env),
		checkSecrets(env),
		checkWGAuth(env),
		checkDataAge(ctx, env),
		checkVehicles(ctx, env),
		checkOverlay(ctx, env),
		checkWN8(ctx, env),
		checkModData(ctx, env),
	}
	return checks
}

// checkWGAuth reports token expiry, which is the failure the user is least
// likely to see coming: tokens last about two weeks and then stop working
// silently in the middle of a sync.
func checkWGAuth(env *Env) check {
	const name = "wg-auth"

	if env.Secrets == nil {
		return check{Name: name, Status: statusUnknown, Detail: "no secret store configured"}
	}

	token, err := loadToken(env)
	if err != nil {
		return check{
			Name:   name,
			Status: statusWarn,
			Detail: "no access token stored; only public data is reachable",
			Fix:    "wotctx auth wg",
		}
	}
	if token.ExpiresAt.IsZero() {
		return check{
			Name:   name,
			Status: statusWarn,
			Detail: "token stored but its expiry is unknown, so it cannot be trusted",
			Fix:    "wotctx auth wg --relogin",
		}
	}

	now := env.now()
	remaining := token.ExpiresAt.Sub(now)

	switch {
	case !token.Valid(now):
		return check{
			Name:   name,
			Status: statusFail,
			Detail: fmt.Sprintf("token expired %s", humanizeAge(int64(-remaining.Seconds()))),
			Fix:    "wotctx auth wg --relogin",
		}
	case token.NeedsProlongation(now, prolongWindow):
		return check{
			Name:   name,
			Status: statusWarn,
			Detail: fmt.Sprintf("token expires in %s", remaining.Round(time.Hour)),
			Fix:    "wotctx auth wg --prolong",
		}
	default:
		return check{
			Name:   name,
			Status: statusOK,
			Detail: fmt.Sprintf("token valid until %s (%s left)",
				token.ExpiresAt.Format(time.RFC3339), remaining.Round(time.Hour)),
		}
	}
}

// checkDataAge reports how stale the cache is. A missing cache warns rather than
// fails: a fresh install simply has not synced yet, and saying so with the
// command to fix it is more useful than an error.
func checkDataAge(ctx context.Context, env *Env) check {
	const name = "data-age"

	if env.ConfigErr != nil {
		return check{Name: name, Status: statusUnknown, Detail: "config unresolved"}
	}
	if !env.HasStore() {
		return check{
			Name:   name,
			Status: statusWarn,
			Detail: "no cache yet at " + env.Paths.DBFile,
			Fix:    "wotctx sync",
		}
	}

	db, err := env.OpenStore(ctx)
	if err != nil {
		return check{
			Name:   name,
			Status: statusFail,
			Detail: env.Redactor().RedactError(err),
			Fix:    "delete " + env.Paths.DBFile + " and run: wotctx sync",
		}
	}
	defer db.Close()

	all, err := db.DataAges(ctx, env.now())
	if err != nil {
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}
	// The client mod's dump ages whenever the game is closed, and no sync can
	// refresh it; mod-data judges it by whether the player has played since.
	var ages []store.SourceAge
	for _, a := range all {
		if !strings.HasPrefix(a.Source, "mod:") {
			ages = append(ages, a)
		}
	}
	if len(ages) == 0 {
		return check{
			Name:   name,
			Status: statusWarn,
			Detail: "cache exists but holds no data",
			Fix:    "wotctx sync",
		}
	}

	// ages is newest first, so the last entry is the stalest source. The
	// threshold is the same one the skill uses to decide whether to sync before
	// answering, which keeps doctor and the skill from disagreeing.
	stalest := ages[len(ages)-1]
	threshold := env.Config.Sync.AutoSyncAfter.Duration

	detail := fmt.Sprintf("%d sources; newest %s (%s), stalest %s (%s)",
		len(ages),
		ages[0].Source, humanizeAge(ages[0].AgeSeconds),
		stalest.Source, humanizeAge(stalest.AgeSeconds))

	// A source only counts as stale once it is past both the threshold and its
	// own TTL: reference data on a seven-day TTL is not due at six hours, and
	// sync would skip it, so warning about it would be a warning no command
	// can clear - and would make the skill sync before every answer for nothing.
	var due []string
	for _, age := range ages {
		limit := max(threshold, env.Config.TTLFor(age.Source))
		if time.Duration(age.AgeSeconds)*time.Second >= limit {
			due = append(due, age.Source)
		}
	}

	result := check{Name: name, Status: statusOK, Detail: detail}
	if len(due) > 0 {
		result.Status = statusWarn
		result.Detail = detail + fmt.Sprintf("; %d past the %s threshold and their TTL: %s",
			len(due), threshold, strings.Join(due, ", "))
		result.Fix = "wotctx sync"
	}

	if run, err := db.LatestSyncRun(ctx); err == nil {
		if notes := run.NoteLines(); len(notes) > 0 {
			result.Detail += fmt.Sprintf("; %d caveat(s) from the last sync", len(notes))
		}
		if run.Finished() && !run.OK {
			result.Status = statusWarn
			result.Fix = "wotctx sync"
		}
	}
	return result
}

// checkVehicles reports the reference data and, specifically, which tiers it
// covers.
//
// The tier breakdown is not decoration. If the encyclopedia does not return
// Tier XI vehicles, research paths into them have to come from the overlay
// instead, and every answer about them has to say where it got them. Reporting
// coverage is what makes that visible rather than a silent gap.
func checkVehicles(ctx context.Context, env *Env) check {
	const name = "vehicles"

	if env.ConfigErr != nil {
		return check{Name: name, Status: statusUnknown, Detail: "config unresolved"}
	}
	if !env.HasStore() {
		return check{Name: name, Status: statusWarn, Detail: "no cache yet", Fix: "wotctx sync"}
	}

	db, err := env.OpenStore(ctx)
	if err != nil {
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}
	defer db.Close()

	coverage, err := syncer.Coverage(ctx, db)
	if err != nil {
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}
	if coverage.Total == 0 {
		return check{
			Name:   name,
			Status: statusWarn,
			Detail: "no vehicles synced yet",
			Fix:    "wotctx sync --only wg",
		}
	}

	edges, err := db.EdgeCount(ctx, "")
	if err != nil {
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}
	overlayEdges, err := db.EdgeCount(ctx, wg.EdgeSourceOverlay)
	if err != nil {
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}

	detail := fmt.Sprintf("%d vehicles, %d tech-tree edges; by tier %s",
		coverage.Total, edges, coverage.Summary())
	if overlayEdges > 0 {
		detail += fmt.Sprintf("; %d edge(s) from the overlay", overlayEdges)
	}

	result := check{Name: name, Status: statusOK, Detail: detail}

	// Tier XI exists in the game as of update 2.0. If the API does not expose
	// it, say so here rather than letting a question about the Executor quietly
	// return nothing.
	if !coverage.HasTier(11) {
		result.Status = statusWarn
		result.Detail += "; no Tier XI vehicles returned by the API"
		result.Fix = "add research_paths: to the overlay for Tier XI lines"
	}

	// private.garage over-reports: it returns ids the encyclopedia does not
	// describe and the client does not show. Confirmed against a counted
	// garage, so these are not hidden tanks and must not be counted as owned.
	if garage, err := db.Garage(ctx); err == nil && len(garage.Unresolved) > 0 {
		result.Detail += fmt.Sprintf("; garage %d identified of %d ids reported (%d undescribed, not real vehicles)",
			len(garage.Identified), garage.Total(), len(garage.Unresolved))
	}
	return result
}

// humanizeAge renders an age the way a person reads it, so both the table and
// the JSON detail string stay legible.
func humanizeAge(seconds int64) string {
	d := time.Duration(seconds) * time.Second
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func checkConfig(env *Env) check {
	if env.ConfigErr != nil {
		return check{
			Name:   "config",
			Status: statusFail,
			Detail: env.Redactor().RedactError(env.ConfigErr),
			Fix:    "fix or delete " + env.Paths.ConfigFile,
		}
	}

	origin := "defaults; no file yet"
	if _, err := os.Stat(env.Paths.ConfigFile); err == nil {
		origin = "loaded"
	}

	paths := fmt.Sprintf("%s (%s); db %s; overlay %s",
		env.Paths.ConfigFile, origin, env.Paths.DBFile, env.Config.OverlayFile(env.Paths))
	if !env.Config.Configured() {
		return check{
			Name:   "config",
			Status: statusFail,
			Detail: "no account set up yet; " + paths,
			Fix:    "wotctx auth wg --realm eu|com|asia (logs in and records the account), or the Tank Advisor app",
		}
	}
	return check{
		Name:   "config",
		Status: statusOK,
		Detail: fmt.Sprintf("%s; account %s/%d (%s)", paths,
			env.Config.Account.Realm, env.Config.Account.AccountID, env.Config.Account.Nickname),
	}
}

// checkSecrets reports which credentials are present and where each came from.
// It never reports a value, and never reports a length either, since a length
// narrows a brute-force search.
func checkSecrets(env *Env) check {
	if env.Secrets == nil {
		return check{Name: "secrets", Status: statusFail, Detail: "no secret store configured"}
	}

	var (
		present = map[secrets.Key]secrets.Source{}
		missing []string
	)
	for _, k := range secrets.All() {
		if _, source, err := env.Secrets.Get(k); err == nil {
			present[k] = source
		} else {
			missing = append(missing, string(k))
		}
	}

	parts := make([]string, 0, len(present))
	// A release carries the project's application id; the player's own, if
	// stored, still wins (docs/spec-desktop.md section 12.1).
	if present[secrets.WGApplicationID] == "" && wg.BuiltinApplicationID() != "" {
		present[secrets.WGApplicationID] = "built in"
		missing = slices.DeleteFunc(missing, func(k string) bool { return k == string(secrets.WGApplicationID) })
	}
	for _, k := range secrets.All() {
		if source, ok := present[k]; ok {
			parts = append(parts, fmt.Sprintf("%s: %s", k, source))
		}
	}
	for _, k := range missing {
		parts = append(parts, fmt.Sprintf("%s: missing", k))
	}
	sort.Strings(missing)

	// A missing application_id blocks every Wargaming call, so it fails rather
	// than warns. A missing token still allows public data, and logging in
	// fixes it, so that only warns.
	result := check{Name: "secrets", Detail: strings.Join(parts, ", ")}
	switch {
	case present[secrets.WGApplicationID] == "":
		result.Status = statusFail
		result.Fix = "register a Mobile (standalone) app at developers.wargaming.net, then: wotctx auth wg --application-id <id>"
	case present[secrets.WGAccessToken] == "":
		result.Status = statusWarn
		result.Fix = "wotctx auth wg"
	default:
		result.Status = statusOK
	}
	return result
}
