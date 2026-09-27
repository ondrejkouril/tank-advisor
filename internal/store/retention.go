package store

import (
	"context"
	"fmt"
	"time"
)

// newestUsable selects the newest usable snapshot of every source and
// endpoint. Neither pruning nor retention ever touches these: they are what
// every current answer is read from.
const newestUsable = `SELECT MAX(id) FROM snapshots WHERE ` + usable + ` GROUP BY source, endpoint`

// PruneRawBodies drops the stored response bodies of snapshots requested
// before cutoff, keeping their parsed rows (docs/spec-desktop.md section
// 12.3). The bodies exist only so a parser fix can be a re-parse rather than a
// re-fetch; a pruned snapshot stays usable but can no longer be re-parsed.
func (db *DB) PruneRawBodies(ctx context.Context, cutoff time.Time) (int, error) {
	res, err := db.sql.ExecContext(ctx, `
		UPDATE snapshots SET raw_gz = NULL, raw_pruned = 1
		WHERE raw_gz IS NOT NULL AND wg_error = '' AND requested_at < ?
		  AND id NOT IN (`+newestUsable+`)`, formatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("pruning raw bodies: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// DeleteSnapshotsBefore removes snapshots requested before cutoff, and with
// them (by cascade) every row parsed from them. It is the retention window of
// docs/spec-desktop.md section 12.4, off unless configured. Recent-form
// windows reaching past the cutoff then have no baseline and say so.
func (db *DB) DeleteSnapshotsBefore(ctx context.Context, cutoff time.Time) (int, error) {
	res, err := db.sql.ExecContext(ctx, `
		DELETE FROM snapshots
		WHERE requested_at < ? AND id NOT IN (`+newestUsable+`)`, formatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("deleting old snapshots: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}
