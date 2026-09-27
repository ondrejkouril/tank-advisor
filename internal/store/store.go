// Package store is the only thing that talks to SQLite.
//
// The database is a cache, not a record of truth: it can be deleted and rebuilt
// from the APIs at any time. What it does hold uniquely is provenance - when
// each response arrived and what it said verbatim - because every answer wotctx
// gives has to be able to state how fresh it is.
//
// The driver is modernc.org/sqlite, a pure-Go implementation, so one tree
// cross-compiles for Windows and macOS without a C toolchain.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// DB is an open database with its schema applied.
type DB struct {
	sql *sql.DB
}

// ErrNotFound reports that a lookup found no row. Callers distinguish "no data
// yet" from a real failure, because the former is a caveat and the latter is an
// error.
var ErrNotFound = errors.New("store: not found")

// ErrSchemaTooNew reports a database migrated by a newer wotctx than this one.
var ErrSchemaTooNew = errors.New("store: database is newer than this wotctx")

// Open opens or creates the database at path and applies any outstanding
// migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	// WAL keeps a long sync from blocking a concurrent read, and the busy
	// timeout covers the case of a sync and a query racing.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)",
		url.PathEscape(path))

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	// modernc's driver serialises writes internally; one connection avoids
	// "database is locked" surprises for a single-user tool.
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	db := &DB{sql: sqlDB}
	if err := db.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Close releases the database.
func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

// migrate applies every migration newer than the recorded schema version.
// PRAGMA user_version is the bookkeeping, which keeps the migration state inside
// the file itself and makes a repeat run a no-op.
func (db *DB) migrate(ctx context.Context) error {
	var current int
	if err := db.sql.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	// The CLI and the Desktop bundle each carry a binary, and both open this
	// one file. An older binary would read tables it does not know as if they
	// were the ones it does, so it stops instead.
	if latest := migrations[len(migrations)-1].version; current > latest {
		return fmt.Errorf("%w: the cache has schema version %d and this wotctx knows only up to %d; install the newer wotctx (make install, or reinstall the Desktop bundle)",
			ErrSchemaTooNew, current, latest)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("starting migration %d: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx, m.stmts); err != nil {
			tx.Rollback()
			return fmt.Errorf("applying migration %d: %w", m.version, err)
		}
		// PRAGMA does not accept a bound parameter, and the value is an int
		// constant from this file rather than anything user-supplied.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", m.version, err)
		}
	}
	return nil
}

// SchemaVersion returns the applied schema version.
func (db *DB) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	if err := db.sql.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("reading schema version: %w", err)
	}
	return v, nil
}

// isNoRows reports whether a query found nothing, so callers can map that to
// ErrNotFound without importing database/sql.
func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// timeFormat is how every timestamp is stored: RFC3339 in UTC, which sorts
// lexicographically and so can be compared in SQL.
const timeFormat = time.RFC3339Nano

func formatTime(t time.Time) string {
	return t.UTC().Format(timeFormat)
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(timeFormat, s)
	if err != nil {
		// Tolerate second-precision values, which is what a hand-edited row or
		// an upstream timestamp will look like.
		if t2, err2 := time.Parse(time.RFC3339, s); err2 == nil {
			return t2.UTC(), nil
		}
		return time.Time{}, fmt.Errorf("parsing timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
