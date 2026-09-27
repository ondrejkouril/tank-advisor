package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// putAt records a usable account/info snapshot at an offset from base.
func putAt(t *testing.T, db *DB, endpoint string, at time.Time) int64 {
	t.Helper()
	id, err := db.PutSnapshot(context.Background(), Snapshot{
		Source: "wg", Endpoint: endpoint, RequestedAt: at, HTTPStatus: 200, Raw: []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	return id
}

func TestPruneRawBodiesKeepsThePrunedSnapshotUsable(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	old := putAt(t, db, "account/info", base.Add(-100*24*time.Hour))
	recent := putAt(t, db, "account/info", base.Add(-10*24*time.Hour))
	// The only snapshot of its endpoint is kept whole however old it is.
	lone := putAt(t, db, "encyclopedia/vehicles", base.Add(-200*24*time.Hour))

	n, err := db.PruneRawBodies(ctx, base.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatalf("PruneRawBodies: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned %d, want 1 (only the old account/info)", n)
	}

	snap, err := db.Snapshot(ctx, old)
	if err != nil {
		t.Fatalf("Snapshot(old): %v", err)
	}
	if len(snap.Raw) != 0 {
		t.Errorf("old snapshot still has a body")
	}
	// Still a baseline: pruning drops the body, not the data point.
	base90, err := db.SnapshotAtOrBefore(ctx, "wg", "account/info", base.Add(-50*24*time.Hour))
	if err != nil || base90.ID != old {
		t.Errorf("SnapshotAtOrBefore = %d, %v; want the pruned %d", base90.ID, err, old)
	}
	for _, id := range []int64{recent, lone} {
		if s, err := db.Snapshot(ctx, id); err != nil || len(s.Raw) == 0 {
			t.Errorf("snapshot %d lost its body (err %v)", id, err)
		}
	}

	// Pruning again is a no-op.
	if n, err := db.PruneRawBodies(ctx, base.Add(-90*24*time.Hour)); err != nil || n != 0 {
		t.Errorf("second prune = %d, %v; want 0", n, err)
	}
}

func TestDeleteSnapshotsBeforeRemovesRowsButNeverTheNewest(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	old := putAt(t, db, "account/info", base.Add(-400*24*time.Hour))
	newest := putAt(t, db, "account/info", base.Add(-300*24*time.Hour))

	n, err := db.DeleteSnapshotsBefore(ctx, base.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteSnapshotsBefore: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted %d, want 1", n)
	}
	if _, err := db.Snapshot(ctx, old); !errors.Is(err, ErrNotFound) {
		t.Errorf("old snapshot: err = %v, want ErrNotFound", err)
	}
	// The newest is kept even past the cutoff: current answers read from it.
	if _, err := db.Snapshot(ctx, newest); err != nil {
		t.Errorf("newest snapshot deleted: %v", err)
	}
}

func TestLongestGapFindsTheWidestIntervalBetweenUsableSnapshots(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, ok, err := db.LongestGap(ctx, "wg", "account/info"); err != nil || ok {
		t.Fatalf("empty history: ok=%v err=%v, want no gap", ok, err)
	}
	putAt(t, db, "account/info", base)
	if _, ok, _ := db.LongestGap(ctx, "wg", "account/info"); ok {
		t.Fatal("one snapshot has no gap")
	}
	putAt(t, db, "account/info", base.Add(24*time.Hour))
	putAt(t, db, "account/info", base.Add(5*24*time.Hour))
	putAt(t, db, "account/info", base.Add(6*24*time.Hour))
	// A failed fetch inside the wide gap does not split it.
	if _, err := db.PutSnapshot(ctx, Snapshot{Source: "wg", Endpoint: "account/info",
		RequestedAt: base.Add(3 * 24 * time.Hour), HTTPStatus: 200, WGError: "SOURCE_NOT_AVAILABLE"}); err != nil {
		t.Fatal(err)
	}
	// Another endpoint's snapshots do not count.
	putAt(t, db, "tanks/stats", base.Add(3*24*time.Hour))

	gap, ok, err := db.LongestGap(ctx, "wg", "account/info")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !gap.From.Equal(base.Add(24*time.Hour)) || gap.Length() != 4*24*time.Hour {
		t.Errorf("gap = %v from %v, want 4 days from day 1", gap.Length(), gap.From)
	}
}
