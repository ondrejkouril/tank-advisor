package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SyncRun records one invocation of `wotctx sync`.
//
// Notes carries the per-source caveats: a Wargaming endpoint that timed out, an
// XVM file that returned 404. Those are reported to the user as known gaps
// rather than swallowed, so they have to survive the run.
type SyncRun struct {
	ID         int64
	StartedAt  time.Time
	FinishedAt time.Time
	OK         bool
	Notes      string
}

// Finished reports whether the run completed. An unfinished run is how a crash
// mid-sync shows up.
func (r SyncRun) Finished() bool {
	return !r.FinishedAt.IsZero()
}

// ErrSyncRunning reports that another sync holds the run. Two syncs at once -
// two sessions starting together, or a session-start hook and an MCP call -
// would each find the data stale and each store a snapshot of it.
var ErrSyncRunning = errors.New("another sync is running")

// SyncRunStaleAfter is how long an unfinished run holds off other syncs. A
// sync takes seconds; a run left unfinished for longer than this was killed,
// and must not block every sync after it.
const SyncRunStaleAfter = 5 * time.Minute

// BeginSyncRun opens a run and returns its id. It is also the lock between
// processes: while another run is unfinished and younger than
// SyncRunStaleAfter, it returns ErrSyncRunning instead. The check and the
// insert are one statement, so two processes cannot both pass the check.
func (db *DB) BeginSyncRun(ctx context.Context, startedAt time.Time) (int64, error) {
	// julianday, not string comparison: RFC 3339 with nanoseconds is not
	// fixed-width, so its strings do not sort as times.
	res, err := db.sql.ExecContext(ctx, `
		INSERT INTO sync_runs(started_at)
		SELECT ?1 WHERE NOT EXISTS (
			SELECT 1 FROM sync_runs
			WHERE finished_at IS NULL AND julianday(started_at) > julianday(?2))`,
		formatTime(startedAt), formatTime(startedAt.Add(-SyncRunStaleAfter)))
	if err != nil {
		return 0, fmt.Errorf("starting sync run: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return 0, ErrSyncRunning
	}
	return res.LastInsertId()
}

// FinishSyncRun closes a run. ok is false when nothing could be synced; a run
// that fetched some sources and recorded caveats for the rest is still ok, per
// the degradation rule in docs/spec.md section 7.3.
func (db *DB) FinishSyncRun(ctx context.Context, id int64, finishedAt time.Time, ok bool, notes []string) error {
	res, err := db.sql.ExecContext(ctx,
		`UPDATE sync_runs SET finished_at = ?, ok = ?, notes = ? WHERE id = ?`,
		formatTime(finishedAt), boolToInt(ok), strings.Join(notes, "\n"), id)
	if err != nil {
		return fmt.Errorf("finishing sync run %d: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return fmt.Errorf("finishing sync run %d: %w", id, ErrNotFound)
	}
	return nil
}

// LatestSyncRun returns the most recent run.
func (db *DB) LatestSyncRun(ctx context.Context) (SyncRun, error) {
	row := db.sql.QueryRowContext(ctx, `
		SELECT id, started_at, finished_at, ok, notes
		FROM sync_runs ORDER BY started_at DESC, id DESC LIMIT 1`)

	var (
		run        SyncRun
		startedAt  string
		finishedAt sql.NullString
		ok         sql.NullBool
	)
	err := row.Scan(&run.ID, &startedAt, &finishedAt, &ok, &run.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncRun{}, ErrNotFound
	}
	if err != nil {
		return SyncRun{}, fmt.Errorf("reading latest sync run: %w", err)
	}

	if run.StartedAt, err = parseTime(startedAt); err != nil {
		return SyncRun{}, err
	}
	if finishedAt.Valid {
		if run.FinishedAt, err = parseTime(finishedAt.String); err != nil {
			return SyncRun{}, err
		}
	}
	run.OK = ok.Valid && ok.Bool
	return run, nil
}

// NoteLines splits a run's notes back into the individual caveats.
func (r SyncRun) NoteLines() []string {
	if strings.TrimSpace(r.Notes) == "" {
		return nil
	}
	return strings.Split(r.Notes, "\n")
}

// CacheEntry is a stored conditional-request validator.
//
// Wargaming returns a content-stable ETag but ignores If-None-Match, answering
// 200 with the full body (measured 2026-09-17). This is kept because it is
// correct if that changes, and because a matching ETag still proves the content
// is unchanged - but today it saves no requests, so TTLs are what actually
// protect the budget.
type CacheEntry struct {
	URLKey       string
	ETag         string
	LastModified string
	FetchedAt    time.Time
}

// CacheEntryFor returns the stored validators for a request key.
func (db *DB) CacheEntryFor(ctx context.Context, urlKey string) (CacheEntry, error) {
	row := db.sql.QueryRowContext(ctx,
		`SELECT url_key, etag, last_modified, fetched_at FROM http_cache WHERE url_key = ?`, urlKey)

	var (
		e         CacheEntry
		fetchedAt string
	)
	err := row.Scan(&e.URLKey, &e.ETag, &e.LastModified, &fetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CacheEntry{}, ErrNotFound
	}
	if err != nil {
		return CacheEntry{}, fmt.Errorf("reading cache entry: %w", err)
	}
	if e.FetchedAt, err = parseTime(fetchedAt); err != nil {
		return CacheEntry{}, err
	}
	return e, nil
}

// PutCacheEntry stores the validators from a response.
func (db *DB) PutCacheEntry(ctx context.Context, e CacheEntry) error {
	if e.URLKey == "" {
		return fmt.Errorf("cache entry needs a url key")
	}
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO http_cache(url_key, etag, last_modified, fetched_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(url_key) DO UPDATE SET
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			fetched_at = excluded.fetched_at`,
		e.URLKey, e.ETag, e.LastModified, formatTime(e.FetchedAt))
	if err != nil {
		return fmt.Errorf("storing cache entry: %w", err)
	}
	return nil
}
