package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AccountState is the account-level row for one snapshot.
//
// One row is written per sync rather than updated in place, which makes
// resource history - credits and free XP over time - a by-product of syncing
// rather than something to build separately.
type AccountState struct {
	SnapshotID int64
	ObservedAt time.Time
	Credits    int
	Gold       int
	Bonds      int
	FreeXP     int

	// IsPremium and PremiumExpiresAt are stored because they are what the API
	// said, but they must not be presented as the account's premium status.
	// Measured against an account holding both a Premium Account and WoT Plus,
	// the API reported false with an expiry eight years in the past: these
	// fields track only the legacy time-based product. Premium status comes
	// from the overlay (docs/spec.md sections 3.1 and 5).
	IsPremium        bool
	PremiumExpiresAt time.Time

	GlobalRating   int
	LastBattleTime time.Time
	BattleLifeTime time.Duration
}

// Booster is one Personal Reserve as recorded in a snapshot.
type Booster struct {
	Kind      string
	Count     int
	State     string
	ExpiresAt time.Time
}

// PutAccountState records account-level figures against a snapshot.
func (db *DB) PutAccountState(ctx context.Context, state AccountState, garage []int, boosters []Booster) error {
	if state.SnapshotID == 0 {
		return fmt.Errorf("account state needs a snapshot id")
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storing account state: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_snapshots(snapshot_id, observed_at, credits, gold, bonds, free_xp,
			is_premium, premium_expires_at, global_rating, last_battle_time, battle_life_time)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(snapshot_id) DO UPDATE SET
			observed_at = excluded.observed_at, credits = excluded.credits,
			gold = excluded.gold, bonds = excluded.bonds, free_xp = excluded.free_xp,
			is_premium = excluded.is_premium, premium_expires_at = excluded.premium_expires_at,
			global_rating = excluded.global_rating, last_battle_time = excluded.last_battle_time,
			battle_life_time = excluded.battle_life_time`,
		state.SnapshotID, formatTime(state.ObservedAt), state.Credits, state.Gold, state.Bonds,
		state.FreeXP, boolToInt(state.IsPremium), nullTime(state.PremiumExpiresAt),
		state.GlobalRating, nullTime(state.LastBattleTime), int(state.BattleLifeTime.Seconds()),
	); err != nil {
		return fmt.Errorf("storing account state: %w", err)
	}

	for _, tankID := range garage {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO garage(snapshot_id, tank_id) VALUES(?, ?)`,
			state.SnapshotID, tankID); err != nil {
			return fmt.Errorf("storing garage entry %d: %w", tankID, err)
		}
	}

	for _, b := range boosters {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO boosters(snapshot_id, kind, count, state, expires_at)
			VALUES(?, ?, ?, ?, ?)
			ON CONFLICT(snapshot_id, kind, state) DO UPDATE SET
				count = excluded.count, expires_at = excluded.expires_at`,
			state.SnapshotID, b.Kind, b.Count, b.State, nullTime(b.ExpiresAt)); err != nil {
			return fmt.Errorf("storing booster %s: %w", b.Kind, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storing account state: %w", err)
	}
	return nil
}

// LatestAccountState returns the most recent account-level figures.
func (db *DB) LatestAccountState(ctx context.Context) (AccountState, error) {
	row := db.sql.QueryRowContext(ctx, `
		SELECT snapshot_id, observed_at, credits, gold, bonds, free_xp,
		       is_premium, premium_expires_at, global_rating, last_battle_time, battle_life_time
		FROM account_snapshots
		ORDER BY observed_at DESC, snapshot_id DESC
		LIMIT 1`)

	var (
		state          AccountState
		observedAt     string
		premiumExpires sql.NullString
		lastBattle     sql.NullString
		isPremium      int
		lifeSeconds    int
	)
	err := row.Scan(&state.SnapshotID, &observedAt, &state.Credits, &state.Gold, &state.Bonds,
		&state.FreeXP, &isPremium, &premiumExpires, &state.GlobalRating, &lastBattle, &lifeSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return AccountState{}, ErrNotFound
	}
	if err != nil {
		return AccountState{}, fmt.Errorf("reading account state: %w", err)
	}

	if state.ObservedAt, err = parseTime(observedAt); err != nil {
		return AccountState{}, err
	}
	if premiumExpires.Valid {
		if state.PremiumExpiresAt, err = parseTime(premiumExpires.String); err != nil {
			return AccountState{}, err
		}
	}
	if lastBattle.Valid {
		if state.LastBattleTime, err = parseTime(lastBattle.String); err != nil {
			return AccountState{}, err
		}
	}
	state.IsPremium = isPremium != 0
	state.BattleLifeTime = time.Duration(lifeSeconds) * time.Second
	return state, nil
}

// LatestGarage returns every tank id private.garage reported, resolvable or
// not. Callers reporting a garage size to a person want Garage instead, which
// separates the ids that describe real vehicles from the ones that do not.
func (db *DB) LatestGarage(ctx context.Context) ([]int, error) {
	state, err := db.LatestAccountState(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := db.sql.QueryContext(ctx,
		`SELECT tank_id FROM garage WHERE snapshot_id = ? ORDER BY tank_id`, state.SnapshotID)
	if err != nil {
		return nil, fmt.Errorf("reading garage: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("reading garage: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading garage: %w", err)
	}
	return ids, nil
}

// GarageBreakdown splits what private.garage returned into the part that
// describes real vehicles and the part that does not.
//
// The split exists because the API over-reports. Measured against the live
// account: private.garage returned 48 ids while the client showed 37 vehicles,
// and the 11 extras are exactly the ids the encyclopedia refuses to describe
// (it returns the id as a key with a null body, even when asked for it
// directly). Since every resolvable id corresponds to a vehicle the player can
// actually see, Identified is the honest answer to "how many tanks do I have"
// and Unresolved is a data artefact to disclose, not a set of hidden tanks.
type GarageBreakdown struct {
	// Identified are ids with a row in the reference vehicle list.
	Identified []int
	// Unresolved are ids the encyclopedia does not describe. They do not appear
	// in the game client either, so they must not be counted as owned.
	Unresolved []int
}

// Total returns how many ids the API reported, resolvable or not.
func (g GarageBreakdown) Total() int {
	return len(g.Identified) + len(g.Unresolved)
}

// Garage returns the latest garage, split into identified and unresolved ids.
func (db *DB) Garage(ctx context.Context) (GarageBreakdown, error) {
	state, err := db.LatestAccountState(ctx)
	if err != nil {
		return GarageBreakdown{}, err
	}

	rows, err := db.sql.QueryContext(ctx, `
		SELECT g.tank_id, v.tank_id IS NOT NULL
		FROM garage g
		LEFT JOIN vehicles v ON v.tank_id = g.tank_id
		WHERE g.snapshot_id = ?
		ORDER BY g.tank_id`, state.SnapshotID)
	if err != nil {
		return GarageBreakdown{}, fmt.Errorf("reading garage: %w", err)
	}
	defer rows.Close()

	var out GarageBreakdown
	for rows.Next() {
		var (
			id         int
			identified bool
		)
		if err := rows.Scan(&id, &identified); err != nil {
			return GarageBreakdown{}, fmt.Errorf("reading garage: %w", err)
		}
		if identified {
			out.Identified = append(out.Identified, id)
		} else {
			out.Unresolved = append(out.Unresolved, id)
		}
	}
	if err := rows.Err(); err != nil {
		return GarageBreakdown{}, fmt.Errorf("reading garage: %w", err)
	}
	return out, nil
}

// LatestBoosters returns the Personal Reserves recorded in the most recent
// account snapshot.
func (db *DB) LatestBoosters(ctx context.Context) ([]Booster, error) {
	state, err := db.LatestAccountState(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := db.sql.QueryContext(ctx,
		`SELECT kind, count, state, expires_at FROM boosters WHERE snapshot_id = ? ORDER BY kind, state`,
		state.SnapshotID)
	if err != nil {
		return nil, fmt.Errorf("reading boosters: %w", err)
	}
	defer rows.Close()

	var boosters []Booster
	for rows.Next() {
		var (
			b         Booster
			expiresAt sql.NullString
		)
		if err := rows.Scan(&b.Kind, &b.Count, &b.State, &expiresAt); err != nil {
			return nil, fmt.Errorf("reading boosters: %w", err)
		}
		if expiresAt.Valid {
			if b.ExpiresAt, err = parseTime(expiresAt.String); err != nil {
				return nil, err
			}
		}
		boosters = append(boosters, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading boosters: %w", err)
	}
	return boosters, nil
}

// AccountStateAtOrBefore returns the newest account snapshot no later than
// cutoff, for reporting how resources have moved.
func (db *DB) AccountStateAtOrBefore(ctx context.Context, cutoff time.Time) (AccountState, error) {
	var snapshotID int64
	err := db.sql.QueryRowContext(ctx, `
		SELECT snapshot_id FROM account_snapshots
		WHERE observed_at <= ?
		ORDER BY observed_at DESC, snapshot_id DESC LIMIT 1`, formatTime(cutoff)).Scan(&snapshotID)
	if errors.Is(err, sql.ErrNoRows) {
		return AccountState{}, ErrNotFound
	}
	if err != nil {
		return AccountState{}, fmt.Errorf("reading account state: %w", err)
	}
	return db.accountStateBySnapshot(ctx, snapshotID)
}

func (db *DB) accountStateBySnapshot(ctx context.Context, snapshotID int64) (AccountState, error) {
	row := db.sql.QueryRowContext(ctx, `
		SELECT snapshot_id, observed_at, credits, gold, bonds, free_xp,
		       is_premium, premium_expires_at, global_rating, last_battle_time, battle_life_time
		FROM account_snapshots WHERE snapshot_id = ?`, snapshotID)

	var (
		state          AccountState
		observedAt     string
		premiumExpires sql.NullString
		lastBattle     sql.NullString
		isPremium      int
		lifeSeconds    int
	)
	err := row.Scan(&state.SnapshotID, &observedAt, &state.Credits, &state.Gold, &state.Bonds,
		&state.FreeXP, &isPremium, &premiumExpires, &state.GlobalRating, &lastBattle, &lifeSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return AccountState{}, ErrNotFound
	}
	if err != nil {
		return AccountState{}, fmt.Errorf("reading account state: %w", err)
	}

	if state.ObservedAt, err = parseTime(observedAt); err != nil {
		return AccountState{}, err
	}
	if premiumExpires.Valid {
		if state.PremiumExpiresAt, err = parseTime(premiumExpires.String); err != nil {
			return AccountState{}, err
		}
	}
	if lastBattle.Valid {
		if state.LastBattleTime, err = parseTime(lastBattle.String); err != nil {
			return AccountState{}, err
		}
	}
	state.IsPremium = isPremium != 0
	state.BattleLifeTime = time.Duration(lifeSeconds) * time.Second
	return state, nil
}

// nullTime formats a time for a nullable column, so that "unknown" is stored as
// NULL rather than as the zero instant.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return formatTime(t)
}
