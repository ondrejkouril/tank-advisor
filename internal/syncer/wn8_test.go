package syncer

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
	"github.com/ondrejkouril/tank-advisor/internal/xvm"
)

func xvmFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "xvm", "wn8exp.json"))
	if err != nil {
		t.Fatalf("reading xvm fixture: %v", err)
	}
	return raw
}

// newWN8Syncer wires a syncer to an XVM stand-in and counts its requests.
func newWN8Syncer(t *testing.T, db *store.DB, status int, body []byte) (*Syncer, *int) {
	t.Helper()
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(server.Close)

	return &Syncer{
		DB:     db,
		Config: config.Default(),
		XVM:    &xvm.Client{URL: server.URL, HTTP: server.Client(), Now: func() time.Time { return testNow }},
		Now:    func() time.Time { return testNow },
	}, &requests
}

func TestSyncWN8StoresExpectedValues(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s, _ := newWN8Syncer(t, db, http.StatusOK, xvmFixture(t))

	report, err := s.Sync(ctx, OnlyWN8)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !contains(report.Synced, "xvm:wn8exp") {
		t.Fatalf("synced = %v", report.Synced)
	}
	// The fixture carries one entry with a zero expDef, which would be a
	// division by zero; it is skipped and said so.
	if len(report.Caveats) != 1 || !strings.Contains(report.Caveats[0], "1 entr") {
		t.Errorf("caveats = %v, want one about the skipped entry", report.Caveats)
	}

	set, err := db.LoadWN8Expected(ctx)
	if err != nil {
		t.Fatalf("LoadWN8Expected: %v", err)
	}
	if set.Version != "2026-09-12" || len(set.ByTank) != 5 || !set.SyncedAt.Equal(testNow) {
		t.Errorf("set = version %q, %d tanks, synced %v", set.Version, len(set.ByTank), set.SyncedAt)
	}
	// The Executor, missing from the frozen pre-split file, is present here.
	if e := set.ByTank[26705]; e.Damage != 2147.761 || e.WinRate != 48.847 {
		t.Errorf("Executor = %+v", e)
	}

	// The raw body is recorded like every other source.
	snap, err := db.LatestUsableSnapshot(ctx, "xvm", "wn8exp")
	if err != nil || len(snap.Raw) == 0 {
		t.Errorf("snapshot = %+v, %v", snap, err)
	}
}

func TestSyncWN8RespectsItsTTL(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s, requests := newWN8Syncer(t, db, http.StatusOK, xvmFixture(t))

	for i := 0; i < 2; i++ {
		if _, err := s.Sync(ctx, OnlyWN8); err != nil {
			t.Fatalf("Sync %d: %v", i, err)
		}
	}
	if *requests != 1 {
		t.Errorf("requests = %d, want 1 within the TTL", *requests)
	}
}

// TestFailedWN8FetchKeepsTheOldSetAndRetries: a 404 must neither erase good
// expected values nor count as fresh data that blocks the next attempt.
func TestFailedWN8FetchKeepsTheOldSetAndRetries(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	good, _ := newWN8Syncer(t, db, http.StatusOK, xvmFixture(t))
	good.Force = true
	if _, err := good.Sync(ctx, OnlyWN8); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	bad, _ := newWN8Syncer(t, db, http.StatusNotFound, []byte("<html>not found</html>"))
	bad.Force = true
	report, err := bad.Sync(ctx, OnlyWN8)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(report.Caveats) == 0 || !strings.Contains(report.Caveats[0], "404") {
		t.Errorf("caveats = %v, want the 404", report.Caveats)
	}
	if set, err := db.LoadWN8Expected(ctx); err != nil || len(set.ByTank) != 5 {
		t.Errorf("after a failed fetch: %d tanks, %v; want the old 5 kept", len(set.ByTank), err)
	}

	// Freshness is judged on the newest usable snapshot, so the 404 must not
	// be one - or its body would count as data for the whole TTL.
	usable, err := db.LatestUsableSnapshot(ctx, "xvm", "wn8exp")
	if err != nil || usable.HTTPStatus != http.StatusOK {
		t.Errorf("latest usable snapshot = HTTP %d, %v; the 404 must not be usable", usable.HTTPStatus, err)
	}
}

func TestSyncWN8NeedsNoWGClient(t *testing.T) {
	db := openTestDB(t)
	s, _ := newWN8Syncer(t, db, http.StatusOK, xvmFixture(t))

	report, err := s.Sync(context.Background(), OnlyWN8)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	for _, c := range report.Caveats {
		if strings.HasPrefix(c, "wg:") {
			t.Errorf("a WN8-only sync complained about Wargaming: %q", c)
		}
	}
}

func TestSyncParsesDefencePoints(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, nil))
	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	amx, err := db.LatestTankStatsFor(ctx, 4929, wg.ModeRandom)
	if err != nil {
		t.Fatalf("LatestTankStatsFor: %v", err)
	}
	if amx.DroppedCapturePoints != 380 || amx.CapturePoints != 120 ||
		amx.RadioAssistedDamage != 322800 || amx.TrackAssistedDamage != 124500 {
		t.Errorf("AMX 13 90 totals = %+v", amx)
	}
	if amx.ParserVersion != store.TankStatsParserVersion {
		t.Errorf("parser version = %d, want %d", amx.ParserVersion, store.TankStatsParserVersion)
	}
}

// TestReparseBackfillsFromStoredBodies: rows written before defence points
// were parsed get them from the raw body already on disk, with no request and
// without disturbing mastery, which came from a different response.
func TestReparseBackfillsFromStoredBodies(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "wotctx.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	s := newTestSyncer(t, db, allSources(t, nil, nil))
	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	// Make the rows look as a version-1 parser left them.
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := raw.Exec(`UPDATE tank_stats SET dropped_capture_points = 0, capture_points = 0,
		radio_assisted_damage = 0, track_assisted_damage = 0, parser_version = 1`); err != nil {
		t.Fatalf("aging rows: %v", err)
	}
	raw.Close()

	stale, err := db.TankStatsSnapshotsToReparse(ctx)
	if err != nil || len(stale) != 1 {
		t.Fatalf("snapshots to re-parse = %v, %v; want one", stale, err)
	}

	n, err := s.ReparseTankStats(ctx)
	if err != nil {
		t.Fatalf("ReparseTankStats: %v", err)
	}
	if n != 4 { // two tanks, two modes each
		t.Errorf("re-parsed %d rows, want 4", n)
	}

	amx, err := db.LatestTankStatsFor(ctx, 4929, wg.ModeRandom)
	if err != nil {
		t.Fatalf("LatestTankStatsFor: %v", err)
	}
	if amx.DroppedCapturePoints != 380 || amx.ParserVersion != store.TankStatsParserVersion {
		t.Errorf("after re-parse: def %d, parser %d", amx.DroppedCapturePoints, amx.ParserVersion)
	}
	if amx.MarkOfMastery != 4 {
		t.Errorf("mastery = %d after re-parse, want the 4 left untouched", amx.MarkOfMastery)
	}

	// And a second pass has nothing to do.
	if n, err := s.ReparseTankStats(ctx); err != nil || n != 0 {
		t.Errorf("second re-parse = (%d, %v), want (0, nil)", n, err)
	}
}

// TestSyncStoresPersonalMissions: statuses ride on account/info, and the
// encyclopedia names the missions it describes.
func TestSyncStoresPersonalMissions(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, nil))
	report, err := s.SyncWG(ctx)
	if err != nil {
		t.Fatalf("SyncWG: %v", err)
	}
	if report.Counts["wg:account/info:personal_missions"] != 3 || report.Counts["wg:encyclopedia/personalmissions:missions"] != 4 {
		t.Errorf("counts = %v", report.Counts)
	}

	statuses, at, err := db.LatestMissionStatuses(ctx)
	if err != nil {
		t.Fatalf("LatestMissionStatuses: %v", err)
	}
	if statuses[226] != "MAIN_REWARD_GOTTEN" || statuses[301] != "ALL_REWARDS_GOTTEN" || at.IsZero() {
		t.Errorf("statuses = %v at %v", statuses, at)
	}
	missions, err := db.PersonalMissions(ctx)
	if err != nil || len(missions) != 4 || missions[0].Name != "LT-1: For Victory!" {
		t.Errorf("missions = %+v, %v", missions, err)
	}
}
