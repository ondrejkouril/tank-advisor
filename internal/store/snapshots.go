package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"
)

// Snapshot is one recorded HTTP response.
//
// Raw holds the uncompressed body; it is gzipped on the way into the database
// and expanded on the way out, so callers never deal with the compression.
type Snapshot struct {
	ID          int64
	Source      string // 'wg' | 'xvm'
	Endpoint    string // 'account/info', 'wn8exp', ...
	RequestedAt time.Time
	HTTPStatus  int
	// WGError carries the code out of a Wargaming error body. Wargaming answers
	// failures with HTTP 200 and {"status":"error"}, so the status code alone
	// cannot tell a caller whether a response is usable.
	WGError     string
	ETag        string
	NotModified bool
	Raw         []byte
}

// SourceKey is the name this snapshot appears under in a query provenance
// envelope and in the sync TTL config, for example "wg:account/info".
func (s Snapshot) SourceKey() string {
	return SourceKey(s.Source, s.Endpoint)
}

// SourceKey builds the canonical name for a source and endpoint pair.
func SourceKey(source, endpoint string) string {
	return source + ":" + endpoint
}

// usable is the SQL condition for a snapshot that holds data: it succeeded, and
// its body is either still stored or was pruned after parsing (migration 8). A
// NULL body without the pruned mark is a failed request.
const usable = `(raw_gz IS NOT NULL OR raw_pruned = 1) AND wg_error = ''`

// OK reports whether the snapshot holds a usable body.
func (s Snapshot) OK() bool {
	return s.WGError == "" && s.HTTPStatus >= 200 && s.HTTPStatus < 300 && len(s.Raw) > 0
}

// PutSnapshot records a response and returns its id. A 304 is stored with an
// empty body and NotModified set: it is evidence that the data was checked, and
// the freshness story needs that distinct from never having asked.
func (db *DB) PutSnapshot(ctx context.Context, s Snapshot) (int64, error) {
	if s.Source == "" || s.Endpoint == "" {
		return 0, fmt.Errorf("snapshot needs a source and an endpoint")
	}
	if s.RequestedAt.IsZero() {
		return 0, fmt.Errorf("snapshot %s needs a requested_at", s.SourceKey())
	}

	var compressed []byte
	if len(s.Raw) > 0 {
		var err error
		if compressed, err = gzipBytes(s.Raw); err != nil {
			return 0, fmt.Errorf("compressing %s body: %w", s.SourceKey(), err)
		}
	}

	res, err := db.sql.ExecContext(ctx, `
		INSERT INTO snapshots(source, endpoint, requested_at, http_status, wg_error, etag, not_modified, raw_gz)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Source, s.Endpoint, formatTime(s.RequestedAt), s.HTTPStatus, s.WGError, s.ETag, boolToInt(s.NotModified), compressed)
	if err != nil {
		return 0, fmt.Errorf("recording %s snapshot: %w", s.SourceKey(), err)
	}
	return res.LastInsertId()
}

// Snapshot returns one snapshot by id.
func (db *DB) Snapshot(ctx context.Context, id int64) (Snapshot, error) {
	row := db.sql.QueryRowContext(ctx, `
		SELECT id, source, endpoint, requested_at, http_status, wg_error, etag, not_modified, raw_gz
		FROM snapshots WHERE id = ?`, id)
	return scanSnapshot(row)
}

// LatestSnapshot returns the most recent snapshot for a source and endpoint,
// whether or not it succeeded. Callers that need a usable body check OK.
func (db *DB) LatestSnapshot(ctx context.Context, source, endpoint string) (Snapshot, error) {
	row := db.sql.QueryRowContext(ctx, `
		SELECT id, source, endpoint, requested_at, http_status, wg_error, etag, not_modified, raw_gz
		FROM snapshots
		WHERE source = ? AND endpoint = ?
		ORDER BY requested_at DESC, id DESC
		LIMIT 1`, source, endpoint)
	return scanSnapshot(row)
}

// LatestUsableSnapshot returns the most recent snapshot that carries a body.
// A run of 304s or failures therefore still resolves to the last real payload,
// which is what a query needs in order to answer at all.
func (db *DB) LatestUsableSnapshot(ctx context.Context, source, endpoint string) (Snapshot, error) {
	row := db.sql.QueryRowContext(ctx, `
		SELECT id, source, endpoint, requested_at, http_status, wg_error, etag, not_modified, raw_gz
		FROM snapshots
		WHERE source = ? AND endpoint = ? AND `+usable+`
		ORDER BY requested_at DESC, id DESC
		LIMIT 1`, source, endpoint)
	return scanSnapshot(row)
}

// SnapshotAtOrBefore returns the newest usable snapshot no later than cutoff.
// This is the baseline half of a Wargaming self-delta: without a snapshot old
// enough, no trend claim can be made and the caller must say so.
func (db *DB) SnapshotAtOrBefore(ctx context.Context, source, endpoint string, cutoff time.Time) (Snapshot, error) {
	row := db.sql.QueryRowContext(ctx, `
		SELECT id, source, endpoint, requested_at, http_status, wg_error, etag, not_modified, raw_gz
		FROM snapshots
		WHERE source = ? AND endpoint = ? AND `+usable+`
		  AND requested_at <= ?
		ORDER BY requested_at DESC, id DESC
		LIMIT 1`, source, endpoint, formatTime(cutoff))
	return scanSnapshot(row)
}

// SourceAge describes how old one source's data is. doctor and every query
// envelope are built from these.
type SourceAge struct {
	Source     string    `json:"name"`
	ObservedAt time.Time `json:"observed_at"`
	AgeSeconds int64     `json:"age_seconds"`
	// LastError is the failure from the most recent attempt, if it failed while
	// an older usable snapshot is still being served.
	LastError string `json:"last_error,omitempty"`
}

// DataAges reports the age of the newest usable snapshot for every source
// present in the database, newest first.
func (db *DB) DataAges(ctx context.Context, now time.Time) ([]SourceAge, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT source, endpoint, MAX(requested_at)
		FROM snapshots
		WHERE `+usable+`
		GROUP BY source, endpoint
		ORDER BY 3 DESC`)
	if err != nil {
		return nil, fmt.Errorf("reading data ages: %w", err)
	}
	defer rows.Close()

	var ages []SourceAge
	for rows.Next() {
		var source, endpoint, observed string
		if err := rows.Scan(&source, &endpoint, &observed); err != nil {
			return nil, fmt.Errorf("reading data ages: %w", err)
		}
		at, err := parseTime(observed)
		if err != nil {
			return nil, err
		}
		ages = append(ages, SourceAge{
			Source:     SourceKey(source, endpoint),
			ObservedAt: at,
			AgeSeconds: int64(now.Sub(at).Seconds()),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading data ages: %w", err)
	}
	return ages, nil
}

// IsFresh reports whether the newest usable snapshot for a source is younger
// than ttl. A source with no data at all is not fresh.
func (db *DB) IsFresh(ctx context.Context, source, endpoint string, ttl time.Duration, now time.Time) (bool, error) {
	snap, err := db.LatestUsableSnapshot(ctx, source, endpoint)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return now.Sub(snap.RequestedAt) < ttl, nil
}

func scanSnapshot(row *sql.Row) (Snapshot, error) {
	var (
		s           Snapshot
		requestedAt string
		notModified int
		compressed  []byte
	)
	err := row.Scan(&s.ID, &s.Source, &s.Endpoint, &requestedAt, &s.HTTPStatus,
		&s.WGError, &s.ETag, &notModified, &compressed)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("reading snapshot: %w", err)
	}

	if s.RequestedAt, err = parseTime(requestedAt); err != nil {
		return Snapshot{}, err
	}
	s.NotModified = notModified != 0

	if len(compressed) > 0 {
		if s.Raw, err = gunzipBytes(compressed); err != nil {
			return Snapshot{}, fmt.Errorf("expanding %s body: %w", s.SourceKey(), err)
		}
	}
	return s, nil
}

func gzipBytes(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipBytes(compressed []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Gap is the interval between two consecutive usable snapshots.
type Gap struct {
	From time.Time
	To   time.Time
}

// Length is how long the gap lasted.
func (g Gap) Length() time.Duration { return g.To.Sub(g.From) }

// LongestGap returns the longest interval between consecutive usable
// snapshots of one source, and false when there are fewer than two. Recent
// form cannot be split finer than the snapshots allow, so the app shows this
// beside the data's age (docs/spec-desktop.md section 5.2).
func (db *DB) LongestGap(ctx context.Context, source, endpoint string) (Gap, bool, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT requested_at FROM snapshots
		WHERE source = ? AND endpoint = ? AND `+usable+`
		ORDER BY requested_at`, source, endpoint)
	if err != nil {
		return Gap{}, false, fmt.Errorf("reading snapshot times: %w", err)
	}
	defer rows.Close()

	var (
		longest Gap
		found   bool
		prev    time.Time
	)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return Gap{}, false, fmt.Errorf("reading snapshot times: %w", err)
		}
		at, err := parseTime(raw)
		if err != nil {
			return Gap{}, false, err
		}
		if !prev.IsZero() {
			if g := (Gap{From: prev, To: at}); !found || g.Length() > longest.Length() {
				longest, found = g, true
			}
		}
		prev = at
	}
	if err := rows.Err(); err != nil {
		return Gap{}, false, fmt.Errorf("reading snapshot times: %w", err)
	}
	return longest, found, nil
}
