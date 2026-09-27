package store

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// SnapshotRef names one snapshot and when it was taken.
type SnapshotRef struct {
	ID int64
	At time.Time
}

// TankStatsSnapshots lists every snapshot holding tank statistics for a mode,
// oldest first. It is the history recent-form questions are answered from, and
// its first entry is day zero: nothing before it can be known (spec section
// 6.1).
func (db *DB) TankStatsSnapshots(ctx context.Context, mode string) ([]SnapshotRef, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT DISTINCT s.id, s.requested_at
		FROM tank_stats ts JOIN snapshots s ON s.id = ts.snapshot_id
		WHERE ts.mode = ?
		ORDER BY s.requested_at, s.id`, mode)
	if err != nil {
		return nil, fmt.Errorf("listing tank stats snapshots: %w", err)
	}
	defer rows.Close()

	var refs []SnapshotRef
	for rows.Next() {
		var (
			ref SnapshotRef
			at  string
		)
		if err := rows.Scan(&ref.ID, &at); err != nil {
			return nil, fmt.Errorf("listing tank stats snapshots: %w", err)
		}
		if ref.At, err = parseTime(at); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing tank stats snapshots: %w", err)
	}
	return refs, nil
}

// TankStatsBetween computes per-tank deltas from one snapshot to a later one.
// Tanks with no battles in between are omitted.
func (db *DB) TankStatsBetween(ctx context.Context, mode string, baseline, latest SnapshotRef) ([]TankStatsDelta, error) {
	if baseline.ID == latest.ID {
		return nil, ErrNoBaseline
	}
	after, err := db.tankStatsForSnapshot(ctx, latest.ID, mode)
	if err != nil {
		return nil, err
	}
	before, err := db.tankStatsForSnapshot(ctx, baseline.ID, mode)
	if err != nil {
		return nil, err
	}

	byTank := make(map[int]TankStats, len(before))
	for _, s := range before {
		byTank[s.TankID] = s
	}

	var deltas []TankStatsDelta
	for _, current := range after {
		prior := byTank[current.TankID] // zero value for a tank new since the baseline
		delta := subtractStats(current, prior)
		if delta.Battles == 0 {
			continue // untouched in the window
		}
		deltas = append(deltas, TankStatsDelta{
			TankID:     current.TankID,
			Mode:       mode,
			Baseline:   prior,
			Latest:     current,
			Delta:      delta,
			BaselineAt: baseline.At,
			LatestAt:   latest.At,
		})
	}
	return deltas, nil
}

// AllVehicles returns the reference vehicle list keyed by tank_id, for callers
// that join many rows against it.
func (db *DB) AllVehicles(ctx context.Context) (map[int]Vehicle, error) {
	list, err := db.vehicles(ctx, vehicleSelect)
	if err != nil {
		return nil, err
	}
	out := make(map[int]Vehicle, len(list))
	for _, v := range list {
		out[v.TankID] = v
	}
	return out, nil
}

// AllEdges returns every tech-tree edge, one per (from, to) pair. The API
// reports most edges twice - from next_tanks and from prices_xp - and the
// overlay may add a third; they agree on cost wherever checked, so the first
// by source name is kept.
func (db *DB) AllEdges(ctx context.Context) ([]Edge, error) {
	edges, err := db.edges(ctx, `SELECT from_tank_id, to_tank_id, xp_cost, source
		FROM vehicle_edges ORDER BY from_tank_id, to_tank_id, source`)
	if err != nil {
		return nil, err
	}
	type pair struct{ from, to int }
	seen := map[pair]bool{}
	out := edges[:0]
	for _, e := range edges {
		p := pair{e.From, e.To}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out, nil
}
