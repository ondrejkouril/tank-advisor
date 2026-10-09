package store

import (
	"context"
	"errors"
	"strings"
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

// TestGameVersionStateDatesOnlyAChange: the version in use when recording
// began is undated; a change between two syncs is bracketed by them.
func TestGameVersionStateDatesOnlyAChange(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }

	must := func(v string, at time.Time) {
		t.Helper()
		if err := db.RecordGameVersion(ctx, v, time.Time{}, at); err != nil {
			t.Fatal(err)
		}
	}
	must("2.4", day(9))
	must("2.4", day(12))
	s, err := db.GameVersionState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Previous != nil || !strings.Contains(s.Describe(), "recorded since 2026-10-09; when it was released is not known") {
		t.Errorf("before any change: %+v, %q", s, s.Describe())
	}

	must("2.4.1", day(14))
	s, err = db.GameVersionState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Previous == nil || s.Previous.Version != "2.4" || !s.Previous.LastSeen.Equal(day(12)) {
		t.Fatalf("after the change: %+v", s)
	}
	if d := s.Describe(); !strings.Contains(d, "2.4.1, which replaced 2.4 between 2026-10-12 12:00 UTC and 2026-10-14 12:00 UTC") {
		t.Errorf("describe = %q", d)
	}
}
