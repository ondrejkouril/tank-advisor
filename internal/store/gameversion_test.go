package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestGameVersionKeepsTheFirstSighting: a version's first sync dates the
// patch; later syncs only move last_seen.
func TestGameVersionKeepsTheFirstSighting(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if _, err := db.LatestGameVersion(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty table = %v, want ErrNotFound", err)
	}

	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	tanks := time.Date(2026, 10, 1, 4, 3, 23, 0, time.UTC)
	for _, rec := range []struct {
		v  string
		at time.Time
	}{{"2.4", day(2)}, {"2.4", day(9)}, {"2.4.1", day(14)}, {"2.4.1", day(20)}} {
		if err := db.RecordGameVersion(ctx, rec.v, tanks, rec.at); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.LatestGameVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "2.4.1" || !got.FirstSeen.Equal(day(14)) || !got.LastSeen.Equal(day(20)) || !got.TanksUpdatedAt.Equal(tanks) {
		t.Errorf("latest = %+v", got)
	}
}
