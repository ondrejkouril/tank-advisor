// Package syncer decides what to fetch, records every response, and turns a
// failure in one source into a caveat rather than a dead run.
//
// The ordering rule throughout is: record the raw response first, parse second.
// A parse that fails still leaves the evidence in the database, so the fix is a
// re-parse rather than another request against a budget of ten per second.
package syncer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
	"github.com/ondrejkouril/tank-advisor/internal/xvm"
)

// Syncer fetches data into the store.
//
// DB and WG may both be nil during a dry run: its purpose is to report what
// would happen, which must not require a cache to exist or a credential to be
// present.
type Syncer struct {
	DB     *store.DB
	Config config.Config
	WG     *wg.Client
	XVM    *xvm.Client

	// AccountID is whose data to fetch.
	AccountID int
	// AccessToken unlocks the private block: credits, gold, bonds, free XP and
	// the garage. A sync without it still works but is heavily degraded, not
	// merely smaller, so its absence is reported as a caveat.
	AccessToken string

	// Now supplies the clock; tests override it.
	Now func() time.Time
	// DryRun reports what would be fetched without writing anything or making
	// a single request.
	DryRun bool
	// Force ignores TTLs and refetches everything.
	Force bool

	// ModDump is the client mod's garage dump; empty leaves it out.
	ModDump string

	// Log receives progress lines. Nil discards them.
	Log func(string)

	// LockWait is how long to wait for another process's sync to finish
	// before giving up with store.ErrSyncRunning. Once it finishes, this sync
	// finds its sources fresh and skips them, which is the point of waiting.
	LockWait time.Duration
	// Sleep pauses between attempts to take the lock; tests override it.
	Sleep func(time.Duration)
}

// Report is the outcome of one sync.
type Report struct {
	Started  time.Time
	Finished time.Time
	// Synced lists the sources that were fetched.
	Synced []string
	// Skipped lists sources left alone because their data was still fresh.
	Skipped []string
	// Caveats are per-source failures. They do not fail the run: a sync that
	// got most of the way is more useful than one that reports nothing.
	Caveats []string
	// Counts carries per-source row counts, for the command's summary.
	Counts map[string]int
}

// OK reports whether anything at all was fetched or was already fresh.
func (r Report) OK() bool {
	return len(r.Synced) > 0 || len(r.Skipped) > 0
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// isFresh reports whether a source is still within its TTL. With no cache at
// all - which a dry run on a clean machine has - nothing is fresh.
func (s *Syncer) isFresh(ctx context.Context, source, endpoint, key string) (bool, error) {
	if s.DB == nil {
		return false, nil
	}
	return s.DB.IsFresh(ctx, source, endpoint, s.Config.TTLFor(key), s.now())
}

func (s *Syncer) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(fmt.Sprintf(format, args...))
	}
}

// Only selects which sources a sync covers.
type Only string

const (
	// All syncs every source.
	All Only = ""
	// OnlyWG syncs the Wargaming API.
	OnlyWG Only = "wg"
	// OnlyWN8 syncs XVM's WN8 expected values.
	OnlyWN8 Only = "wn8"
	// OnlyMod reads the client mod's garage dump.
	OnlyMod Only = "mod"
)

type source struct {
	key string
	run func(context.Context, *Report) error
}

// SyncWG fetches every Wargaming source that is due.
func (s *Syncer) SyncWG(ctx context.Context) (Report, error) {
	return s.Sync(ctx, OnlyWG)
}

// Sync fetches every selected source that is due.
func (s *Syncer) Sync(ctx context.Context, only Only) (Report, error) {
	report := Report{Started: s.now(), Counts: map[string]int{}}

	// A dry run reports intent, so it neither needs nor builds a client. A
	// missing client removes its sources from the run as a caveat.
	var sources []source
	if only == All || only == OnlyWG {
		if s.WG == nil && !s.DryRun {
			report.Caveats = append(report.Caveats, "wg: no client configured (missing application_id)")
		} else {
			// Order matters only in that reference data comes first: everything
			// else is more useful once vehicle names exist to attach it to.
			sources = append(sources,
				source{"wg:encyclopedia/vehicles", s.syncEncyclopedia},
				source{"wg:encyclopedia/achievements", s.syncAchievementDefs},
				source{"wg:encyclopedia/personalmissions", s.syncPersonalMissions},
				source{"wg:account/info", s.syncAccountInfo},
				source{"wg:tanks/stats", s.syncTankStats},
				source{"wg:account/achievements", s.syncAccountAchievements},
				source{"wg:tanks/achievements", s.syncTankAchievements},
			)
		}
	}
	if only == All || only == OnlyWN8 {
		if s.XVM == nil && !s.DryRun {
			report.Caveats = append(report.Caveats, "xvm: no client configured")
		} else {
			sources = append(sources, source{wn8SourceKey, s.syncWN8Expected})
		}
	}
	// The client mod's dump is a local file: no client, no credential.
	if (only == All || only == OnlyMod) && s.ModDump != "" {
		sources = append(sources, source{ModSourceKey, s.syncModGarage})
	}
	if len(sources) == 0 {
		report.Finished = s.now()
		return report, nil
	}

	var (
		runID  int64
		closed bool
	)
	if !s.DryRun {
		var err error
		if runID, err = s.beginRun(ctx); err != nil {
			return report, err
		}
		report.Started = s.now()

		// A run left unfinished holds off every other sync until it goes
		// stale, so one that stops early - cancelled, or failing to write -
		// is still closed, as not ok. The write must outlive a cancelled ctx.
		defer func() {
			if !closed {
				s.DB.FinishSyncRun(context.WithoutCancel(ctx), runID, s.now(), false, report.Caveats)
			}
		}()

		// Bring rows from an older parser up to date from their stored bodies
		// first, so everything below reads complete rows. No request is made.
		if n, err := s.ReparseTankStats(ctx); err != nil {
			report.Caveats = append(report.Caveats, "wg:tanks/stats: re-parse: "+oneLine(err.Error()))
		} else if n > 0 {
			report.Counts["wg:tanks/stats:reparsed"] = n
		}
	}

	for _, source := range sources {
		if err := source.run(ctx, &report); err != nil {
			// Cancellation is the user's decision, not a source failing.
			if ctx.Err() != nil {
				return report, err
			}
			// A source failing is a caveat, not the end of the run.
			report.Caveats = append(report.Caveats,
				fmt.Sprintf("%s: %s", source.key, oneLine(err.Error())))
		}
	}

	if !s.DryRun && s.DB != nil {
		s.prune(ctx, &report)
	}

	report.Finished = s.now()
	if !s.DryRun {
		if err := s.DB.FinishSyncRun(ctx, runID, report.Finished, report.OK(), report.Caveats); err != nil {
			return report, err
		}
		closed = true
	}
	return report, nil
}

// prune drops raw bodies past config.RawBodyRetention and, when a retention
// window is configured, whole snapshots past it (docs/spec-desktop.md
// sections 12.3 and 12.4). The newest snapshot of every source is never
// touched. A failure is a caveat: the data synced is still good.
func (s *Syncer) prune(ctx context.Context, report *Report) {
	now := s.now()
	if n, err := s.DB.PruneRawBodies(ctx, now.Add(-config.RawBodyRetention)); err != nil {
		report.Caveats = append(report.Caveats, "pruning raw bodies: "+oneLine(err.Error()))
	} else if n > 0 {
		report.Counts["snapshots:bodies_pruned"] = n
	}
	if keep := s.Config.Sync.Retention.Duration; keep > 0 {
		if n, err := s.DB.DeleteSnapshotsBefore(ctx, now.Add(-keep)); err != nil {
			report.Caveats = append(report.Caveats, "applying sync.retention: "+oneLine(err.Error()))
		} else if n > 0 {
			report.Counts["snapshots:deleted_by_retention"] = n
		}
	}
}

// beginRun opens a sync run, waiting up to LockWait for another process's run
// to finish first.
func (s *Syncer) beginRun(ctx context.Context) (int64, error) {
	const poll = time.Second
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for waited := time.Duration(0); ; waited += poll {
		id, err := s.DB.BeginSyncRun(ctx, s.now())
		if !errors.Is(err, store.ErrSyncRunning) || waited >= s.LockWait {
			return id, err
		}
		if waited == 0 {
			s.logf("another sync is running; waiting up to %s for it", s.LockWait)
		}
		sleep(poll)
		if err := ctx.Err(); err != nil {
			return 0, err
		}
	}
}

// syncEncyclopedia refreshes the reference vehicle list and the tech tree.
func (s *Syncer) syncEncyclopedia(ctx context.Context, report *Report) error {
	const (
		source   = "wg"
		endpoint = "encyclopedia/vehicles"
	)
	key := store.SourceKey(source, endpoint)

	due, err := s.due(ctx, source, endpoint, key, report)
	if err != nil || !due {
		return err
	}
	if s.DryRun {
		return s.reportIntent(key, report)
	}

	var (
		vehicles  []wg.Vehicle
		edges     []store.Edge
		pageNo    = 1
		pageTotal = 1
	)

	for pageNo <= pageTotal {
		// The ETag is per page, since each page is a separate resource.
		cacheKey := fmt.Sprintf("%s?page_no=%d", key, pageNo)
		etag := ""
		if entry, err := s.DB.CacheEntryFor(ctx, cacheKey); err == nil {
			etag = entry.ETag
		}

		page, fetchErr := s.WG.VehiclePage(ctx, pageNo, etag)

		// Record the attempt before inspecting it, so even a failure leaves a
		// trail.
		if _, err := s.DB.PutSnapshot(ctx, snapshotFrom(source, endpoint, page.Result)); err != nil {
			return err
		}
		if fetchErr != nil {
			return fetchErr
		}

		if page.Result.NotModified {
			s.logf("%s page %d: unchanged", key, pageNo)
			// An unchanged page means the stored vehicles are still correct, so
			// there is nothing to rewrite for it.
			pageNo++
			continue
		}

		if page.Result.ETag != "" {
			if err := s.DB.PutCacheEntry(ctx, store.CacheEntry{
				URLKey:    cacheKey,
				ETag:      page.Result.ETag,
				FetchedAt: page.Result.RequestedAt,
			}); err != nil {
				return err
			}
		}

		vehicles = append(vehicles, page.Vehicles...)
		for _, v := range page.Vehicles {
			for _, e := range v.Edges() {
				edges = append(edges, store.Edge{From: e.From, To: e.To, XPCost: e.XPCost, Source: e.Source})
			}
		}

		if page.PageTotal > 0 {
			pageTotal = page.PageTotal
		}
		s.logf("%s page %d/%d: %d vehicles", key, pageNo, pageTotal, len(page.Vehicles))
		pageNo++
	}

	if len(vehicles) == 0 {
		// Every page was a 304. The stored list stands.
		report.Skipped = append(report.Skipped, key)
		return nil
	}

	rows := make([]store.Vehicle, 0, len(vehicles))
	syncedAt := s.now()
	for _, v := range vehicles {
		rows = append(rows, store.Vehicle{
			TankID: v.TankID, Name: v.Name, ShortName: v.ShortName,
			Tier: v.Tier, Type: v.Type, Nation: v.Nation, Tag: v.Tag,
			IsPremium: v.IsPremium, IsGift: v.IsGift, IsWheeled: v.IsWheeled,
			PriceCredit: v.PriceCredit, PriceGold: v.PriceGold,
			SyncedAt: syncedAt,
		})
	}

	n, err := s.DB.UpsertVehicles(ctx, rows)
	if err != nil {
		return err
	}
	report.Counts[key+":vehicles"] = n

	// Edges are replaced per API source, which leaves any overlay-supplied
	// edges untouched - they exist because the API is missing something, so a
	// successful API sync must not delete them.
	for _, edgeSource := range []string{wg.EdgeSourceNextTanks, wg.EdgeSourcePricesXP} {
		subset := make([]store.Edge, 0, len(edges))
		for _, e := range edges {
			if e.Source == edgeSource {
				subset = append(subset, e)
			}
		}
		count, err := s.DB.ReplaceEdgesFromSource(ctx, edgeSource, subset)
		if err != nil {
			return err
		}
		report.Counts[key+":edges:"+edgeSource] = count
	}

	report.Synced = append(report.Synced, key)
	return nil
}

// snapshotFrom converts a client result into the store's record of it.
func snapshotFrom(source, endpoint string, result wg.Result) store.Snapshot {
	requestedAt := result.RequestedAt
	if requestedAt.IsZero() {
		requestedAt = time.Now().UTC()
	}
	return store.Snapshot{
		Source:      source,
		Endpoint:    endpoint,
		RequestedAt: requestedAt,
		HTTPStatus:  result.HTTPStatus,
		WGError:     result.WGError,
		ETag:        result.ETag,
		NotModified: result.NotModified,
		Raw:         result.Raw,
	}
}

// TierCoverage describes which tiers the reference data covers.
//
// It exists to answer one specific question: whether the Wargaming
// encyclopedia returns Tier XI vehicles at all. If it does not, research paths
// into those vehicles have to come from the overlay instead, and every answer
// about them has to say so.
type TierCoverage struct {
	CountsByTier map[int]int
	MaxTier      int
	Total        int
}

// HasTier reports whether any vehicle at a tier is known.
func (c TierCoverage) HasTier(tier int) bool {
	return c.CountsByTier[tier] > 0
}

// Summary renders the coverage as "1:12 2:34 ..." for a doctor line.
func (c TierCoverage) Summary() string {
	if len(c.CountsByTier) == 0 {
		return "no vehicles"
	}
	var parts []string
	for tier := 1; tier <= c.MaxTier; tier++ {
		if n := c.CountsByTier[tier]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d:%d", tier, n))
		}
	}
	return strings.Join(parts, " ")
}

// Coverage reports the tiers present in the reference data.
func Coverage(ctx context.Context, db *store.DB) (TierCoverage, error) {
	counts, err := db.VehicleCountsByTier(ctx)
	if err != nil {
		return TierCoverage{}, err
	}

	coverage := TierCoverage{CountsByTier: counts}
	for tier, n := range counts {
		coverage.Total += n
		if tier > coverage.MaxTier {
			coverage.MaxTier = tier
		}
	}
	return coverage, nil
}

// ErrNoVehicles reports that the reference list has not been synced.
var ErrNoVehicles = errors.New("no vehicles synced yet")
