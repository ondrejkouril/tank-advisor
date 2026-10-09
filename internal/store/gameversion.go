package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// GameVersion is one server game version and when syncs saw it.
type GameVersion struct {
	Version        string
	FirstSeen      time.Time
	LastSeen       time.Time
	TanksUpdatedAt time.Time
}

// RecordGameVersion notes that a sync saw version at seenAt. The first
// sighting is kept; the last one moves.
func (db *DB) RecordGameVersion(ctx context.Context, version string, tanksUpdatedAt, seenAt time.Time) error {
	var tanks any
	if !tanksUpdatedAt.IsZero() {
		tanks = formatTime(tanksUpdatedAt)
	}
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO game_versions(version, first_seen, last_seen, tanks_updated_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(version) DO UPDATE SET last_seen = excluded.last_seen,
			tanks_updated_at = COALESCE(excluded.tanks_updated_at, tanks_updated_at)`,
		version, formatTime(seenAt), formatTime(seenAt), tanks)
	if err != nil {
		return fmt.Errorf("storing game version %s: %w", version, err)
	}
	return nil
}

// LatestGameVersion is the version the most recent sync saw.
func (db *DB) LatestGameVersion(ctx context.Context) (GameVersion, error) {
	var (
		v           GameVersion
		first, last string
		tanks       sql.NullString
	)
	err := db.sql.QueryRowContext(ctx, `
		SELECT version, first_seen, last_seen, tanks_updated_at FROM game_versions
		ORDER BY last_seen DESC, first_seen DESC LIMIT 1`).Scan(&v.Version, &first, &last, &tanks)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, fmt.Errorf("reading the game version: %w", err)
	}
	if v.FirstSeen, err = parseTime(first); err != nil {
		return v, err
	}
	if v.LastSeen, err = parseTime(last); err != nil {
		return v, err
	}
	if v.TanksUpdatedAt, err = parseTime(tanks.String); err != nil {
		return v, err
	}
	return v, nil
}
