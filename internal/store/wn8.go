package store

import (
	"context"
	"fmt"
	"time"
)

// WN8Expected is one vehicle's expected values, as XVM publishes them.
type WN8Expected struct {
	TankID  int
	Damage  float64
	Frags   float64
	Spot    float64
	Def     float64
	WinRate float64 // percent
}

// WN8ExpectedSet is the stored table with its provenance.
type WN8ExpectedSet struct {
	// Version is XVM's own stamp for the file, e.g. "2026-09-12".
	Version  string
	SyncedAt time.Time
	ByTank   map[int]WN8Expected
}

// ReplaceWN8Expected swaps in a complete set of expected values.
//
// The whole table is replaced rather than upserted: a vehicle XVM stops
// publishing must stop being scored, and one table never mixes vintages.
func (db *DB) ReplaceWN8Expected(ctx context.Context, version string, syncedAt time.Time, values []WN8Expected) (int, error) {
	if version == "" {
		return 0, fmt.Errorf("expected values need a version")
	}
	if len(values) == 0 {
		// Refuse rather than empty the table: a bad download must not erase a
		// good set.
		return 0, fmt.Errorf("refusing to replace expected values with an empty set")
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storing expected values: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM wn8_expected`); err != nil {
		return 0, fmt.Errorf("clearing expected values: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO wn8_expected(tank_id, damage, frags, spot, def, win_rate, synced_at, version)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("storing expected values: %w", err)
	}
	defer stmt.Close()

	for _, v := range values {
		if _, err := stmt.ExecContext(ctx, v.TankID, v.Damage, v.Frags, v.Spot, v.Def, v.WinRate,
			formatTime(syncedAt), version); err != nil {
			return 0, fmt.Errorf("storing expected values for tank %d: %w", v.TankID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storing expected values: %w", err)
	}
	return len(values), nil
}

// LoadWN8Expected returns the stored expected values, or ErrNotFound when none
// have been synced.
func (db *DB) LoadWN8Expected(ctx context.Context) (WN8ExpectedSet, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT tank_id, damage, frags, spot, def, win_rate, synced_at, version FROM wn8_expected`)
	if err != nil {
		return WN8ExpectedSet{}, fmt.Errorf("reading expected values: %w", err)
	}
	defer rows.Close()

	set := WN8ExpectedSet{ByTank: map[int]WN8Expected{}}
	for rows.Next() {
		var (
			v        WN8Expected
			syncedAt string
		)
		if err := rows.Scan(&v.TankID, &v.Damage, &v.Frags, &v.Spot, &v.Def, &v.WinRate,
			&syncedAt, &set.Version); err != nil {
			return WN8ExpectedSet{}, fmt.Errorf("reading expected values: %w", err)
		}
		if set.SyncedAt, err = parseTime(syncedAt); err != nil {
			return WN8ExpectedSet{}, err
		}
		set.ByTank[v.TankID] = v
	}
	if err := rows.Err(); err != nil {
		return WN8ExpectedSet{}, fmt.Errorf("reading expected values: %w", err)
	}
	if len(set.ByTank) == 0 {
		return WN8ExpectedSet{}, ErrNotFound
	}
	return set, nil
}
