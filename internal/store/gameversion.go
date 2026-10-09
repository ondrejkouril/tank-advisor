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

// GameVersionState is the current version and, once a sync has seen the
// version change, the one it replaced.
//
// Only a change dates a release. The first version ever recorded was already
// in use when recording began, so its first_seen is when wotctx started
// looking, not when Wargaming released it: run 11 of the evaluation read it as
// a release date (docs/evals.md).
type GameVersionState struct {
	Current  GameVersion
	Previous *GameVersion
}

// GameVersionState reads the current version and the one before it.
func (db *DB) GameVersionState(ctx context.Context) (GameVersionState, error) {
	cur, err := db.LatestGameVersion(ctx)
	if err != nil {
		return GameVersionState{}, err
	}
	state := GameVersionState{Current: cur}
	var (
		prev        GameVersion
		first, last string
	)
	err = db.sql.QueryRowContext(ctx, `
		SELECT version, first_seen, last_seen FROM game_versions
		WHERE version <> ? AND last_seen <= ? ORDER BY last_seen DESC LIMIT 1`,
		cur.Version, formatTime(cur.FirstSeen)).Scan(&prev.Version, &first, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("reading the previous game version: %w", err)
	}
	if prev.FirstSeen, err = parseTime(first); err != nil {
		return state, err
	}
	if prev.LastSeen, err = parseTime(last); err != nil {
		return state, err
	}
	state.Previous = &prev
	return state, nil
}

// Describe says what is known about when the current version arrived, for
// doctor and the brief.
func (s GameVersionState) Describe() string {
	if s.Previous == nil {
		return fmt.Sprintf("%s, recorded since %s; when it was released is not known here (wotctx meta news --updates dates updates)",
			s.Current.Version, s.Current.FirstSeen.Format("2006-01-02"))
	}
	const at = "2006-01-02 15:04 UTC"
	return fmt.Sprintf("%s, which replaced %s between %s and %s (the last sync on the old version and the first on the new)",
		s.Current.Version, s.Previous.Version, s.Previous.LastSeen.Format(at), s.Current.FirstSeen.Format(at))
}
