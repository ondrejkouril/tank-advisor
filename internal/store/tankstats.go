package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TankStats is one tank's cumulative statistics in one mode, as recorded in one
// snapshot.
type TankStats struct {
	SnapshotID int64
	TankID     int
	Mode       string

	Battles                int
	Wins                   int
	Losses                 int
	Draws                  int
	Survived               int
	DamageDealt            int
	DamageReceived         int
	Frags                  int
	Spotted                int
	XP                     int
	BattleAvgXP            int
	HitsPercents           int
	AvgDamageAssisted      float64
	AvgDamageAssistedRadio float64
	AvgDamageAssistedTrack float64
	AvgDamageAssistedStun  float64
	AvgDamageBlocked       float64
	TankingFactor          float64
	MarkOfMastery          int

	// Totals, so a delta over them is exact. DroppedCapturePoints is WN8's "def".
	CapturePoints        int
	DroppedCapturePoints int
	RadioAssistedDamage  int
	TrackAssistedDamage  int
	StunAssistedDamage   int

	// ParserVersion is set on read; writes always record TankStatsParserVersion.
	ParserVersion int
}

// WinRate returns wins as a fraction of battles, or 0 with no battles.
func (s TankStats) WinRate() float64 {
	if s.Battles == 0 {
		return 0
	}
	return float64(s.Wins) / float64(s.Battles)
}

// DPG returns average damage per battle, or 0 with no battles.
func (s TankStats) DPG() float64 {
	if s.Battles == 0 {
		return 0
	}
	return float64(s.DamageDealt) / float64(s.Battles)
}

// SurvivalRate returns survived battles as a fraction, or 0 with no battles.
func (s TankStats) SurvivalRate() float64 {
	if s.Battles == 0 {
		return 0
	}
	return float64(s.Survived) / float64(s.Battles)
}

// PutTankStats records per-tank statistics against a snapshot.
func (db *DB) PutTankStats(ctx context.Context, stats []TankStats) (int, error) {
	if len(stats) == 0 {
		return 0, nil
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storing tank stats: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO tank_stats(snapshot_id, tank_id, mode, battles, wins, losses, draws, survived,
			damage_dealt, damage_received, frags, spotted, xp, battle_avg_xp, hits_percents,
			avg_damage_assisted, avg_damage_assisted_radio, avg_damage_assisted_track,
			avg_damage_assisted_stun, avg_damage_blocked, tanking_factor, mark_of_mastery,
			capture_points, dropped_capture_points, radio_assisted_damage,
			track_assisted_damage, stun_assisted_damage, parser_version)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(snapshot_id, tank_id, mode) DO UPDATE SET
			battles = excluded.battles, wins = excluded.wins, losses = excluded.losses,
			draws = excluded.draws, survived = excluded.survived,
			damage_dealt = excluded.damage_dealt, damage_received = excluded.damage_received,
			frags = excluded.frags, spotted = excluded.spotted, xp = excluded.xp,
			battle_avg_xp = excluded.battle_avg_xp, hits_percents = excluded.hits_percents,
			avg_damage_assisted = excluded.avg_damage_assisted,
			avg_damage_assisted_radio = excluded.avg_damage_assisted_radio,
			avg_damage_assisted_track = excluded.avg_damage_assisted_track,
			avg_damage_assisted_stun = excluded.avg_damage_assisted_stun,
			avg_damage_blocked = excluded.avg_damage_blocked,
			tanking_factor = excluded.tanking_factor,
			mark_of_mastery = excluded.mark_of_mastery,
			capture_points = excluded.capture_points,
			dropped_capture_points = excluded.dropped_capture_points,
			radio_assisted_damage = excluded.radio_assisted_damage,
			track_assisted_damage = excluded.track_assisted_damage,
			stun_assisted_damage = excluded.stun_assisted_damage,
			parser_version = excluded.parser_version`)
	if err != nil {
		return 0, fmt.Errorf("storing tank stats: %w", err)
	}
	defer stmt.Close()

	for _, s := range stats {
		if s.SnapshotID == 0 || s.TankID == 0 || s.Mode == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			s.SnapshotID, s.TankID, s.Mode, s.Battles, s.Wins, s.Losses, s.Draws, s.Survived,
			s.DamageDealt, s.DamageReceived, s.Frags, s.Spotted, s.XP, s.BattleAvgXP, s.HitsPercents,
			s.AvgDamageAssisted, s.AvgDamageAssistedRadio, s.AvgDamageAssistedTrack,
			s.AvgDamageAssistedStun, s.AvgDamageBlocked, s.TankingFactor, s.MarkOfMastery,
			s.CapturePoints, s.DroppedCapturePoints, s.RadioAssistedDamage,
			s.TrackAssistedDamage, s.StunAssistedDamage, TankStatsParserVersion,
		); err != nil {
			return 0, fmt.Errorf("storing tank %d stats: %w", s.TankID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storing tank stats: %w", err)
	}
	return len(stats), nil
}

// LatestTankStats returns the most recent per-tank statistics for a mode.
func (db *DB) LatestTankStats(ctx context.Context, mode string) ([]TankStats, error) {
	snapshotID, err := db.latestTankStatsSnapshot(ctx, mode)
	if err != nil {
		return nil, err
	}
	return db.tankStatsForSnapshot(ctx, snapshotID, mode)
}

// LatestTankStatsFor returns one tank's most recent statistics in a mode.
func (db *DB) LatestTankStatsFor(ctx context.Context, tankID int, mode string) (TankStats, error) {
	snapshotID, err := db.latestTankStatsSnapshot(ctx, mode)
	if err != nil {
		return TankStats{}, err
	}

	rows, err := db.tankStatsQuery(ctx,
		tankStatsSelect+` WHERE snapshot_id = ? AND mode = ? AND tank_id = ?`, snapshotID, mode, tankID)
	if err != nil {
		return TankStats{}, err
	}
	if len(rows) == 0 {
		return TankStats{}, ErrNotFound
	}
	return rows[0], nil
}

// TankStatsDelta is the change in one tank's statistics between two snapshots.
//
// This is how recent form is derived on the Wargaming side, because the API is
// cumulative only. Baseline and Latest are both reported so a caller can state
// the actual window rather than the one it asked for.
type TankStatsDelta struct {
	TankID   int
	Mode     string
	Baseline TankStats
	Latest   TankStats
	Delta    TankStats

	BaselineAt time.Time
	LatestAt   time.Time
}

// Window returns how much time the delta actually spans.
func (d TankStatsDelta) Window() time.Duration {
	return d.LatestAt.Sub(d.BaselineAt)
}

// ErrNoBaseline reports that no snapshot is old enough to support the requested
// window.
//
// This is deliberately an error rather than a silently narrower window: a trend
// claim with no baseline is the specific mistake this whole design exists to
// prevent.
var ErrNoBaseline = errors.New("store: no snapshot old enough for this window")

// TankStatsSince computes per-tank deltas from the newest snapshot at or before
// cutoff to the latest one.
func (db *DB) TankStatsSince(ctx context.Context, mode string, cutoff time.Time) ([]TankStatsDelta, error) {
	latestID, err := db.latestTankStatsSnapshot(ctx, mode)
	if err != nil {
		return nil, err
	}
	latestAt, err := db.snapshotTime(ctx, latestID)
	if err != nil {
		return nil, err
	}

	baselineID, baselineAt, err := db.tankStatsSnapshotAtOrBefore(ctx, mode, cutoff)
	if err != nil {
		return nil, err
	}
	return db.TankStatsBetween(ctx, mode,
		SnapshotRef{ID: baselineID, At: baselineAt}, SnapshotRef{ID: latestID, At: latestAt})
}

// subtractStats differences two cumulative rows.
//
// Averages are recomputed from the differenced totals rather than subtracted,
// since the difference of two averages is meaningless. Where the API only
// provides an average and no total - the assist breakdown - the value is
// reconstructed by weighting each side by its battle count.
func subtractStats(latest, baseline TankStats) TankStats {
	battles := latest.Battles - baseline.Battles

	out := TankStats{
		TankID:         latest.TankID,
		Mode:           latest.Mode,
		Battles:        battles,
		Wins:           latest.Wins - baseline.Wins,
		Losses:         latest.Losses - baseline.Losses,
		Draws:          latest.Draws - baseline.Draws,
		Survived:       latest.Survived - baseline.Survived,
		DamageDealt:    latest.DamageDealt - baseline.DamageDealt,
		DamageReceived: latest.DamageReceived - baseline.DamageReceived,
		Frags:          latest.Frags - baseline.Frags,
		Spotted:        latest.Spotted - baseline.Spotted,
		XP:             latest.XP - baseline.XP,
		MarkOfMastery:  latest.MarkOfMastery,

		CapturePoints:        latest.CapturePoints - baseline.CapturePoints,
		DroppedCapturePoints: latest.DroppedCapturePoints - baseline.DroppedCapturePoints,
		RadioAssistedDamage:  latest.RadioAssistedDamage - baseline.RadioAssistedDamage,
		TrackAssistedDamage:  latest.TrackAssistedDamage - baseline.TrackAssistedDamage,
		StunAssistedDamage:   latest.StunAssistedDamage - baseline.StunAssistedDamage,
		ParserVersion:        latest.ParserVersion,
	}
	// A delta is only as complete as the older of its two rows. A tank new
	// since the baseline has a zero-value baseline, which says nothing.
	if baseline.SnapshotID != 0 && baseline.ParserVersion < out.ParserVersion {
		out.ParserVersion = baseline.ParserVersion
	}
	if battles > 0 {
		out.BattleAvgXP = out.XP / battles
		out.AvgDamageAssisted = weightedDelta(latest.AvgDamageAssisted, latest.Battles, baseline.AvgDamageAssisted, baseline.Battles)
		out.AvgDamageAssistedRadio = weightedDelta(latest.AvgDamageAssistedRadio, latest.Battles, baseline.AvgDamageAssistedRadio, baseline.Battles)
		out.AvgDamageAssistedTrack = weightedDelta(latest.AvgDamageAssistedTrack, latest.Battles, baseline.AvgDamageAssistedTrack, baseline.Battles)
		out.AvgDamageAssistedStun = weightedDelta(latest.AvgDamageAssistedStun, latest.Battles, baseline.AvgDamageAssistedStun, baseline.Battles)
		out.AvgDamageBlocked = weightedDelta(latest.AvgDamageBlocked, latest.Battles, baseline.AvgDamageBlocked, baseline.Battles)
	}
	// hits_percents and tanking_factor are ratios with no recoverable totals,
	// so they are left at zero rather than guessed at.
	return out
}

// weightedDelta recovers the average over the interval between two cumulative
// averages: (avg2*n2 - avg1*n1) / (n2 - n1).
func weightedDelta(latestAvg float64, latestN int, baselineAvg float64, baselineN int) float64 {
	n := latestN - baselineN
	if n <= 0 {
		return 0
	}
	total := latestAvg*float64(latestN) - baselineAvg*float64(baselineN)
	if total < 0 {
		return 0
	}
	return total / float64(n)
}

func (db *DB) latestTankStatsSnapshot(ctx context.Context, mode string) (int64, error) {
	var id int64
	err := db.sql.QueryRowContext(ctx, `
		SELECT ts.snapshot_id
		FROM tank_stats ts
		JOIN snapshots s ON s.id = ts.snapshot_id
		WHERE ts.mode = ?
		ORDER BY s.requested_at DESC, s.id DESC
		LIMIT 1`, mode).Scan(&id)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("finding tank stats snapshot: %w", err)
	}
	return id, nil
}

func (db *DB) tankStatsSnapshotAtOrBefore(ctx context.Context, mode string, cutoff time.Time) (int64, time.Time, error) {
	var (
		id      int64
		atValue string
	)
	err := db.sql.QueryRowContext(ctx, `
		SELECT ts.snapshot_id, s.requested_at
		FROM tank_stats ts
		JOIN snapshots s ON s.id = ts.snapshot_id
		WHERE ts.mode = ? AND s.requested_at <= ?
		ORDER BY s.requested_at DESC, s.id DESC
		LIMIT 1`, mode, formatTime(cutoff)).Scan(&id, &atValue)
	if isNoRows(err) {
		return 0, time.Time{}, ErrNoBaseline
	}
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("finding baseline snapshot: %w", err)
	}

	at, err := parseTime(atValue)
	if err != nil {
		return 0, time.Time{}, err
	}
	return id, at, nil
}

func (db *DB) snapshotTime(ctx context.Context, snapshotID int64) (time.Time, error) {
	var value string
	err := db.sql.QueryRowContext(ctx,
		`SELECT requested_at FROM snapshots WHERE id = ?`, snapshotID).Scan(&value)
	if isNoRows(err) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("reading snapshot time: %w", err)
	}
	return parseTime(value)
}

func (db *DB) tankStatsForSnapshot(ctx context.Context, snapshotID int64, mode string) ([]TankStats, error) {
	return db.tankStatsQuery(ctx,
		tankStatsSelect+` WHERE snapshot_id = ? AND mode = ? ORDER BY tank_id`, snapshotID, mode)
}

const tankStatsSelect = `
	SELECT snapshot_id, tank_id, mode, battles, wins, losses, draws, survived,
	       damage_dealt, damage_received, frags, spotted, xp, battle_avg_xp, hits_percents,
	       avg_damage_assisted, avg_damage_assisted_radio, avg_damage_assisted_track,
	       avg_damage_assisted_stun, avg_damage_blocked, tanking_factor, mark_of_mastery,
	       capture_points, dropped_capture_points, radio_assisted_damage,
	       track_assisted_damage, stun_assisted_damage, parser_version
	FROM tank_stats`

func (db *DB) tankStatsQuery(ctx context.Context, query string, args ...any) ([]TankStats, error) {
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading tank stats: %w", err)
	}
	defer rows.Close()

	var out []TankStats
	for rows.Next() {
		var s TankStats
		if err := rows.Scan(&s.SnapshotID, &s.TankID, &s.Mode, &s.Battles, &s.Wins, &s.Losses,
			&s.Draws, &s.Survived, &s.DamageDealt, &s.DamageReceived, &s.Frags, &s.Spotted,
			&s.XP, &s.BattleAvgXP, &s.HitsPercents, &s.AvgDamageAssisted,
			&s.AvgDamageAssistedRadio, &s.AvgDamageAssistedTrack, &s.AvgDamageAssistedStun,
			&s.AvgDamageBlocked, &s.TankingFactor, &s.MarkOfMastery,
			&s.CapturePoints, &s.DroppedCapturePoints, &s.RadioAssistedDamage,
			&s.TrackAssistedDamage, &s.StunAssistedDamage, &s.ParserVersion); err != nil {
			return nil, fmt.Errorf("reading tank stats: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading tank stats: %w", err)
	}
	return out, nil
}

// TankStatsSnapshotsToReparse returns the snapshots holding rows written by a
// parser older than TankStatsParserVersion, oldest first. A snapshot whose
// body was pruned cannot be re-parsed, so its rows stay as they are.
func (db *DB) TankStatsSnapshotsToReparse(ctx context.Context) ([]int64, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT DISTINCT ts.snapshot_id FROM tank_stats ts
		JOIN snapshots s ON s.id = ts.snapshot_id
		WHERE ts.parser_version < ? AND s.raw_gz IS NOT NULL
		ORDER BY ts.snapshot_id`, TankStatsParserVersion)
	if err != nil {
		return nil, fmt.Errorf("finding stale tank stats: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("finding stale tank stats: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finding stale tank stats: %w", err)
	}
	return ids, nil
}

// UpdateTankStatsTotals rewrites the totals a newer parser added on rows that
// already exist, and marks them current. Everything else - mastery in
// particular, which came from a second request - is left alone, so a re-parse
// of one response cannot damage what another supplied.
func (db *DB) UpdateTankStatsTotals(ctx context.Context, stats []TankStats) (int, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("updating tank stats: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE tank_stats SET
			capture_points = ?, dropped_capture_points = ?, radio_assisted_damage = ?,
			track_assisted_damage = ?, stun_assisted_damage = ?, parser_version = ?
		WHERE snapshot_id = ? AND tank_id = ? AND mode = ?`)
	if err != nil {
		return 0, fmt.Errorf("updating tank stats: %w", err)
	}
	defer stmt.Close()

	updated := 0
	for _, s := range stats {
		res, err := stmt.ExecContext(ctx,
			s.CapturePoints, s.DroppedCapturePoints, s.RadioAssistedDamage,
			s.TrackAssistedDamage, s.StunAssistedDamage, TankStatsParserVersion,
			s.SnapshotID, s.TankID, s.Mode)
		if err != nil {
			return 0, fmt.Errorf("updating tank %d stats: %w", s.TankID, err)
		}
		if n, err := res.RowsAffected(); err == nil {
			updated += int(n)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("updating tank stats: %w", err)
	}
	return updated, nil
}
