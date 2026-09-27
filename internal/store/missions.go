package store

import (
	"context"
	"fmt"
	"time"
)

// PersonalMission is a row of pm_missions.
type PersonalMission struct {
	MissionID   int
	CampaignID  int
	Campaign    string
	OperationID int
	Operation   string
	SetID       int
	Name        string
	Class       string
	MinTier     int
	MaxTier     int
	Primary     string
	Secondary   string
}

// ReplacePersonalMissions swaps in the whole mission list.
func (db *DB) ReplacePersonalMissions(ctx context.Context, missions []PersonalMission, syncedAt time.Time) (int, error) {
	if len(missions) == 0 {
		return 0, fmt.Errorf("refusing to replace personal missions with an empty list")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storing personal missions: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM pm_missions`); err != nil {
		return 0, fmt.Errorf("clearing personal missions: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO pm_missions(mission_id, campaign_id, campaign, operation_id, operation, set_id,
			name, class, min_tier, max_tier, primary_conditions, secondary_conditions, synced_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("storing personal missions: %w", err)
	}
	defer stmt.Close()
	for _, m := range missions {
		if _, err := stmt.ExecContext(ctx, m.MissionID, m.CampaignID, m.Campaign, m.OperationID, m.Operation,
			m.SetID, m.Name, m.Class, m.MinTier, m.MaxTier, m.Primary, m.Secondary, formatTime(syncedAt)); err != nil {
			return 0, fmt.Errorf("storing personal mission %d: %w", m.MissionID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storing personal missions: %w", err)
	}
	return len(missions), nil
}

// PersonalMissions returns every described mission, in mission-id order.
func (db *DB) PersonalMissions(ctx context.Context) ([]PersonalMission, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT mission_id, campaign_id, campaign, operation_id, operation, set_id, name, class,
		       min_tier, max_tier, primary_conditions, secondary_conditions
		FROM pm_missions ORDER BY mission_id`)
	if err != nil {
		return nil, fmt.Errorf("reading personal missions: %w", err)
	}
	defer rows.Close()

	var out []PersonalMission
	for rows.Next() {
		var m PersonalMission
		if err := rows.Scan(&m.MissionID, &m.CampaignID, &m.Campaign, &m.OperationID, &m.Operation,
			&m.SetID, &m.Name, &m.Class, &m.MinTier, &m.MaxTier, &m.Primary, &m.Secondary); err != nil {
			return nil, fmt.Errorf("reading personal missions: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PutMissionStatuses records mission statuses against an account snapshot.
func (db *DB) PutMissionStatuses(ctx context.Context, snapshotID int64, statuses map[int]string) (int, error) {
	if snapshotID == 0 {
		return 0, fmt.Errorf("mission statuses need a snapshot id")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storing mission statuses: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO pm_status(snapshot_id, mission_id, status) VALUES(?, ?, ?)
		ON CONFLICT(snapshot_id, mission_id) DO UPDATE SET status = excluded.status`)
	if err != nil {
		return 0, fmt.Errorf("storing mission statuses: %w", err)
	}
	defer stmt.Close()
	for id, status := range statuses {
		if _, err := stmt.ExecContext(ctx, snapshotID, id, status); err != nil {
			return 0, fmt.Errorf("storing mission %d status: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storing mission statuses: %w", err)
	}
	return len(statuses), nil
}

// LatestMissionStatuses returns the statuses from the newest account snapshot
// that recorded any, with that snapshot's time. ErrNotFound means none has.
func (db *DB) LatestMissionStatuses(ctx context.Context) (map[int]string, time.Time, error) {
	var (
		snapshotID int64
		at         string
	)
	err := db.sql.QueryRowContext(ctx, `
		SELECT s.id, s.requested_at FROM snapshots s
		WHERE s.id IN (SELECT DISTINCT snapshot_id FROM pm_status)
		ORDER BY s.requested_at DESC, s.id DESC LIMIT 1`).Scan(&snapshotID, &at)
	if isNoRows(err) {
		return nil, time.Time{}, ErrNotFound
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("finding mission statuses: %w", err)
	}
	when, err := parseTime(at)
	if err != nil {
		return nil, time.Time{}, err
	}

	rows, err := db.sql.QueryContext(ctx, `SELECT mission_id, status FROM pm_status WHERE snapshot_id = ?`, snapshotID)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("reading mission statuses: %w", err)
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var (
			id     int
			status string
		)
		if err := rows.Scan(&id, &status); err != nil {
			return nil, time.Time{}, fmt.Errorf("reading mission statuses: %w", err)
		}
		out[id] = status
	}
	return out, when, rows.Err()
}
