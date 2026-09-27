package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// base is a fixed clock so age assertions are exact rather than approximate.
var base = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

func openTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "wotctx.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenAppliesSchema(t *testing.T) {
	db := openTestDB(t)

	got, err := db.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	want := migrations[len(migrations)-1].version
	if got != want {
		t.Errorf("schema version = %d, want %d", got, want)
	}
}

// TestMigrationsAreIdempotent is the plan step 4 acceptance criterion for
// migrations: reopening the same file twice must not reapply anything.
func TestMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wotctx.db")

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	// Leave a row behind so a re-applied schema would be obvious: CREATE TABLE
	// would fail, and a dropped-and-recreated table would lose the row.
	if _, err := first.PutSnapshot(ctx, Snapshot{
		Source: "wg", Endpoint: "account/info", RequestedAt: base, HTTPStatus: 200, Raw: []byte(`{}`),
	}); err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	if _, err := second.LatestSnapshot(ctx, "wg", "account/info"); err != nil {
		t.Errorf("row did not survive the second Open: %v", err)
	}
}

// TestSnapshotBodyRoundTrips is the other half of the step 4 criterion: the
// gzipped body must expand to the original bytes, since a parser fix must be a
// re-parse rather than a re-fetch.
func TestSnapshotBodyRoundTrips(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	// Something big and repetitive, like a real encyclopedia page, plus
	// non-ASCII, since tank names carry it.
	raw := []byte(`{"data":{"19281":{"name":"Concept No. 5","short_name":"Concept No. 5",` +
		`"tier":10,"nation":"uk","next_tanks":{"29473":325000}}},"meta":{"count":1}}` +
		strings.Repeat(` {"filler":"Škoda T 56 – Öbjekt"}`, 200))

	id, err := db.PutSnapshot(ctx, Snapshot{
		Source: "wg", Endpoint: "encyclopedia/vehicles", RequestedAt: base,
		HTTPStatus: 200, ETag: `W/"abc123"`, Raw: raw,
	})
	if err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}

	got, err := db.Snapshot(ctx, id)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if string(got.Raw) != string(raw) {
		t.Errorf("body did not round-trip: got %d bytes, want %d", len(got.Raw), len(raw))
	}
	if !got.RequestedAt.Equal(base) {
		t.Errorf("RequestedAt = %s, want %s", got.RequestedAt, base)
	}
	if got.ETag != `W/"abc123"` {
		t.Errorf("ETag = %q, want the stored validator", got.ETag)
	}
	if !got.OK() {
		t.Error("OK() = false for a 200 with a body")
	}
	if got.SourceKey() != "wg:encyclopedia/vehicles" {
		t.Errorf("SourceKey = %q", got.SourceKey())
	}
}

func TestPutSnapshotRejectsIncompleteRecords(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	cases := map[string]Snapshot{
		"no source":   {Endpoint: "account/info", RequestedAt: base},
		"no endpoint": {Source: "wg", RequestedAt: base},
		"no time":     {Source: "wg", Endpoint: "account/info"},
	}
	for name, s := range cases {
		if _, err := db.PutSnapshot(ctx, s); err == nil {
			t.Errorf("PutSnapshot(%s) = nil, want an error", name)
		}
	}
}

// TestNotModifiedPreservesTheLastUsableBody covers the ETag path: a 304 records
// that the data was checked, while queries keep resolving to the last real
// payload.
func TestNotModifiedPreservesTheLastUsableBody(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.PutSnapshot(ctx, Snapshot{
		Source: "wg", Endpoint: "account/info", RequestedAt: base,
		HTTPStatus: 200, Raw: []byte(`{"credits":1000}`),
	}); err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	if _, err := db.PutSnapshot(ctx, Snapshot{
		Source: "wg", Endpoint: "account/info", RequestedAt: base.Add(time.Hour),
		HTTPStatus: 304, NotModified: true,
	}); err != nil {
		t.Fatalf("PutSnapshot(304): %v", err)
	}

	latest, err := db.LatestSnapshot(ctx, "wg", "account/info")
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if !latest.NotModified {
		t.Error("LatestSnapshot did not return the 304; freshness needs to see it")
	}
	if latest.OK() {
		t.Error("a 304 with no body reports OK")
	}

	usable, err := db.LatestUsableSnapshot(ctx, "wg", "account/info")
	if err != nil {
		t.Fatalf("LatestUsableSnapshot: %v", err)
	}
	if string(usable.Raw) != `{"credits":1000}` {
		t.Errorf("usable body = %q, want the original payload", usable.Raw)
	}
}

// TestWGErrorBodyIsNotUsable covers Wargaming's habit of answering failures with
// HTTP 200.
func TestWGErrorBodyIsNotUsable(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.PutSnapshot(ctx, Snapshot{
		Source: "wg", Endpoint: "tanks/stats", RequestedAt: base,
		HTTPStatus: 200, WGError: "INVALID_ACCESS_TOKEN",
		Raw: []byte(`{"status":"error","error":{"code":407}}`),
	}); err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}

	snap, err := db.LatestSnapshot(ctx, "wg", "tanks/stats")
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if snap.OK() {
		t.Error("OK() = true for an HTTP 200 carrying a Wargaming error")
	}
	if _, err := db.LatestUsableSnapshot(ctx, "wg", "tanks/stats"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestUsableSnapshot = %v, want ErrNotFound", err)
	}
}

func TestLookupsReportNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.Snapshot(ctx, 404); !errors.Is(err, ErrNotFound) {
		t.Errorf("Snapshot = %v, want ErrNotFound", err)
	}
	if _, err := db.LatestSnapshot(ctx, "wg", "nothing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestSnapshot = %v, want ErrNotFound", err)
	}
	if _, err := db.LatestSyncRun(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestSyncRun = %v, want ErrNotFound", err)
	}
	if _, err := db.CacheEntryFor(ctx, "nothing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("CacheEntryFor = %v, want ErrNotFound", err)
	}
}

// TestSnapshotAtOrBeforeFindsTheDeltaBaseline exercises the lookup behind the
// Wargaming self-delta, including the case that must refuse to answer.
func TestSnapshotAtOrBeforeFindsTheDeltaBaseline(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	for _, age := range []time.Duration{45 * 24 * time.Hour, 20 * 24 * time.Hour, 0} {
		if _, err := db.PutSnapshot(ctx, Snapshot{
			Source: "wg", Endpoint: "tanks/stats", RequestedAt: base.Add(-age),
			HTTPStatus: 200, Raw: []byte(`{"age":"` + age.String() + `"}`),
		}); err != nil {
			t.Fatalf("PutSnapshot: %v", err)
		}
	}

	// A 30-day window has a baseline: the 45-day-old snapshot.
	got, err := db.SnapshotAtOrBefore(ctx, "wg", "tanks/stats", base.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("SnapshotAtOrBefore(30d): %v", err)
	}
	if want := base.Add(-45 * 24 * time.Hour); !got.RequestedAt.Equal(want) {
		t.Errorf("baseline = %s, want %s", got.RequestedAt, want)
	}

	// A 90-day window has none, and must say so rather than silently using the
	// oldest available snapshot and overstating the window.
	if _, err := db.SnapshotAtOrBefore(ctx, "wg", "tanks/stats", base.Add(-90*24*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("SnapshotAtOrBefore(90d) = %v, want ErrNotFound", err)
	}
}

func TestIsFreshUsesTheTTL(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	// No data at all is not fresh, and is not an error either.
	fresh, err := db.IsFresh(ctx, "wg", "account/info", time.Hour, base)
	if err != nil {
		t.Fatalf("IsFresh with no data: %v", err)
	}
	if fresh {
		t.Error("IsFresh = true with no data")
	}

	if _, err := db.PutSnapshot(ctx, Snapshot{
		Source: "wg", Endpoint: "account/info", RequestedAt: base.Add(-90 * time.Minute),
		HTTPStatus: 200, Raw: []byte(`{}`),
	}); err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}

	if fresh, _ = db.IsFresh(ctx, "wg", "account/info", time.Hour, base); fresh {
		t.Error("a 90-minute-old snapshot counts as fresh against a 1h TTL")
	}
	if fresh, _ = db.IsFresh(ctx, "wg", "account/info", 2*time.Hour, base); !fresh {
		t.Error("a 90-minute-old snapshot is stale against a 2h TTL")
	}
}

func TestDataAgesReportsEverySource(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	writes := []struct {
		source, endpoint string
		age              time.Duration
	}{
		{"wg", "account/info", 30 * time.Minute},
		{"wg", "tanks/stats", 30 * time.Minute},
		{"tomato", "recents", 2 * time.Hour},
	}
	for _, w := range writes {
		if _, err := db.PutSnapshot(ctx, Snapshot{
			Source: w.source, Endpoint: w.endpoint, RequestedAt: base.Add(-w.age),
			HTTPStatus: 200, Raw: []byte(`{}`),
		}); err != nil {
			t.Fatalf("PutSnapshot: %v", err)
		}
	}
	// A failure must not masquerade as fresh data.
	if _, err := db.PutSnapshot(ctx, Snapshot{
		Source: "tomato", Endpoint: "equipment", RequestedAt: base,
		HTTPStatus: 403, WGError: "", Raw: nil,
	}); err != nil {
		t.Fatalf("PutSnapshot(403): %v", err)
	}

	ages, err := db.DataAges(ctx, base)
	if err != nil {
		t.Fatalf("DataAges: %v", err)
	}
	if len(ages) != 3 {
		t.Fatalf("DataAges returned %d sources, want 3: %+v", len(ages), ages)
	}

	byName := map[string]SourceAge{}
	for _, a := range ages {
		byName[a.Source] = a
	}
	if got := byName["tomato:recents"].AgeSeconds; got != int64(2*time.Hour/time.Second) {
		t.Errorf("tomato:recents age = %ds, want 7200", got)
	}
	if _, ok := byName["tomato:equipment"]; ok {
		t.Error("a body-less 403 was reported as available data")
	}
	// Newest first.
	if ages[len(ages)-1].Source != "tomato:recents" {
		t.Errorf("DataAges is not ordered newest first: %+v", ages)
	}
}

func TestSyncRunLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	id, err := db.BeginSyncRun(ctx, base)
	if err != nil {
		t.Fatalf("BeginSyncRun: %v", err)
	}

	open, err := db.LatestSyncRun(ctx)
	if err != nil {
		t.Fatalf("LatestSyncRun: %v", err)
	}
	if open.Finished() {
		t.Error("a freshly begun run reports as finished")
	}

	notes := []string{
		"tomato:equipment unavailable: 403 (mod data not uploaded)",
		"wg:tanks/achievements skipped: fresh",
	}
	if err := db.FinishSyncRun(ctx, id, base.Add(12*time.Second), true, notes); err != nil {
		t.Fatalf("FinishSyncRun: %v", err)
	}

	done, err := db.LatestSyncRun(ctx)
	if err != nil {
		t.Fatalf("LatestSyncRun: %v", err)
	}
	if !done.Finished() || !done.OK {
		t.Errorf("run = %+v, want finished and ok", done)
	}
	if got := done.NoteLines(); len(got) != 2 || got[0] != notes[0] {
		t.Errorf("NoteLines = %q, want the caveats to survive the round trip", got)
	}
}

func TestFinishSyncRunRejectsUnknownID(t *testing.T) {
	db := openTestDB(t)

	err := db.FinishSyncRun(context.Background(), 999, base, true, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FinishSyncRun(999) = %v, want ErrNotFound", err)
	}
}

func TestSyncRunWithNoNotesHasNoNoteLines(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	id, err := db.BeginSyncRun(ctx, base)
	if err != nil {
		t.Fatalf("BeginSyncRun: %v", err)
	}
	if err := db.FinishSyncRun(ctx, id, base, true, nil); err != nil {
		t.Fatalf("FinishSyncRun: %v", err)
	}

	run, err := db.LatestSyncRun(ctx)
	if err != nil {
		t.Fatalf("LatestSyncRun: %v", err)
	}
	if got := run.NoteLines(); len(got) != 0 {
		t.Errorf("NoteLines = %q, want none", got)
	}
}

func TestCacheEntryUpserts(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	key := "wg:encyclopedia/vehicles?page_no=1"
	if err := db.PutCacheEntry(ctx, CacheEntry{URLKey: key, ETag: `"v1"`, FetchedAt: base}); err != nil {
		t.Fatalf("PutCacheEntry: %v", err)
	}
	if err := db.PutCacheEntry(ctx, CacheEntry{URLKey: key, ETag: `"v2"`, FetchedAt: base.Add(time.Hour)}); err != nil {
		t.Fatalf("PutCacheEntry (update): %v", err)
	}

	got, err := db.CacheEntryFor(ctx, key)
	if err != nil {
		t.Fatalf("CacheEntryFor: %v", err)
	}
	if got.ETag != `"v2"` {
		t.Errorf("ETag = %q, want the updated validator", got.ETag)
	}
	if !got.FetchedAt.Equal(base.Add(time.Hour)) {
		t.Errorf("FetchedAt = %s, want the updated time", got.FetchedAt)
	}
}

func TestPutCacheEntryNeedsAKey(t *testing.T) {
	if err := openTestDB(t).PutCacheEntry(context.Background(), CacheEntry{ETag: `"x"`}); err == nil {
		t.Error("PutCacheEntry with no key = nil, want an error")
	}
}

// TestParsedRowsAreDeletedWithTheirSnapshot checks the foreign keys: a parsed
// row must never outlive the response it came from, or provenance breaks.
func TestParsedRowsAreDeletedWithTheirSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	id, err := db.PutSnapshot(ctx, Snapshot{
		Source: "wg", Endpoint: "account/info", RequestedAt: base, HTTPStatus: 200, Raw: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	if _, err := db.sql.ExecContext(ctx,
		`INSERT INTO garage(snapshot_id, tank_id) VALUES(?, ?)`, id, 19281); err != nil {
		t.Fatalf("inserting garage row: %v", err)
	}

	if _, err := db.sql.ExecContext(ctx, `DELETE FROM snapshots WHERE id = ?`, id); err != nil {
		t.Fatalf("deleting snapshot: %v", err)
	}

	var remaining int
	if err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM garage WHERE snapshot_id = ?`, id).Scan(&remaining); err != nil {
		t.Fatalf("counting garage rows: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d garage rows outlived their snapshot; ON DELETE CASCADE is not in effect", remaining)
	}
}

func TestOpenFailsOnAnUnwritablePath(t *testing.T) {
	// A directory where a file should be.
	dir := t.TempDir()
	if _, err := Open(context.Background(), dir); err == nil {
		t.Error("Open on a directory = nil, want an error")
	}
}

// TestBeginSyncRunIsALock: while one run is unfinished, a second cannot begin,
// until the first finishes or goes stale (a killed sync must not block
// every later one).
func TestBeginSyncRunIsALock(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	first, err := db.BeginSyncRun(ctx, base)
	if err != nil {
		t.Fatalf("first BeginSyncRun: %v", err)
	}
	if _, err := db.BeginSyncRun(ctx, base.Add(30*time.Second)); !errors.Is(err, ErrSyncRunning) {
		t.Fatalf("second BeginSyncRun during the first = %v, want ErrSyncRunning", err)
	}

	if err := db.FinishSyncRun(ctx, first, base.Add(40*time.Second), true, nil); err != nil {
		t.Fatalf("FinishSyncRun: %v", err)
	}
	second, err := db.BeginSyncRun(ctx, base.Add(time.Minute))
	if err != nil {
		t.Fatalf("BeginSyncRun after the first finished: %v", err)
	}

	// Left unfinished, the second run blocks others until it is stale.
	if _, err := db.BeginSyncRun(ctx, base.Add(time.Minute+SyncRunStaleAfter-time.Second)); !errors.Is(err, ErrSyncRunning) {
		t.Errorf("BeginSyncRun just inside the stale limit = %v, want ErrSyncRunning", err)
	}
	// Half a second past it, the cutoff is "…12:01:00.5Z" against a start of
	// "…12:01:00Z": as strings the start sorts after the cutoff ('Z' > '.'),
	// so a string comparison would still call the run live.
	third, err := db.BeginSyncRun(ctx, base.Add(time.Minute+SyncRunStaleAfter+500*time.Millisecond))
	if err != nil {
		t.Fatalf("BeginSyncRun after the run went stale: %v", err)
	}
	if third == second {
		t.Error("the stale run's id was reused")
	}
}

// TestOpenRefusesANewerSchema: the CLI and the Desktop bundle carry separate
// binaries over one cache, so an older one must stop rather than misread it.
func TestOpenRefusesANewerSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wotctx.db")

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	next := migrations[len(migrations)-1].version + 1
	if _, err := db.sql.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", next)); err != nil {
		t.Fatalf("stamping a newer version: %v", err)
	}
	db.Close()

	_, err = Open(ctx, path)
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Open of a newer schema = %v, want ErrSchemaTooNew", err)
	}
	if !strings.Contains(err.Error(), "install the newer wotctx") {
		t.Errorf("error %q does not name the fix", err)
	}
}
