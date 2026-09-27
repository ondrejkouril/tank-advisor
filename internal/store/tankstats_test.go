package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// putSnapshotAt records a snapshot at a given time and returns its id, so the
// delta tests can lay out a timeline.
func putSnapshotAt(t *testing.T, db *DB, endpoint string, at time.Time) int64 {
	t.Helper()

	id, err := db.PutSnapshot(context.Background(), Snapshot{
		Source: "wg", Endpoint: endpoint, RequestedAt: at, HTTPStatus: 200, Raw: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	return id
}

func TestPutTankStatsRoundTrips(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	id := putSnapshotAt(t, db, "tanks/stats", base)

	want := TankStats{
		SnapshotID: id, TankID: 4929, Mode: "random",
		Battles: 529, Wins: 259, Losses: 265, Draws: 5, Survived: 159,
		DamageDealt: 592480, DamageReceived: 410200, Frags: 602, Spotted: 1450,
		XP: 420100, BattleAvgXP: 794, HitsPercents: 71,
		AvgDamageAssisted: 845.6, AvgDamageAssistedRadio: 610.2,
		AvgDamageAssistedTrack: 235.4, AvgDamageBlocked: 12.4,
		TankingFactor: 0.04, MarkOfMastery: 4,
	}
	if _, err := db.PutTankStats(ctx, []TankStats{want}); err != nil {
		t.Fatalf("PutTankStats: %v", err)
	}

	got, err := db.LatestTankStatsFor(ctx, 4929, "random")
	if err != nil {
		t.Fatalf("LatestTankStatsFor: %v", err)
	}
	if got.Battles != want.Battles || got.DamageDealt != want.DamageDealt {
		t.Errorf("stats did not round-trip: %+v", got)
	}
	if got.AvgDamageAssistedRadio != want.AvgDamageAssistedRadio {
		t.Errorf("assist radio = %v, want %v", got.AvgDamageAssistedRadio, want.AvgDamageAssistedRadio)
	}
	if got.MarkOfMastery != 4 {
		t.Errorf("mastery = %d, want 4", got.MarkOfMastery)
	}

	// Derived figures, which every rollup depends on.
	if wr := got.WinRate(); wr < 0.489 || wr > 0.490 {
		t.Errorf("WinRate = %.4f, want about 0.4896", wr)
	}
	if dpg := got.DPG(); dpg < 1119 || dpg > 1121 {
		t.Errorf("DPG = %.1f, want about 1120", dpg)
	}
	if sr := got.SurvivalRate(); sr < 0.300 || sr > 0.301 {
		t.Errorf("SurvivalRate = %.4f, want about 0.3006", sr)
	}
}

func TestDerivedFiguresHandleZeroBattles(t *testing.T) {
	// A tank with no battles must not divide by zero, and must not report a
	// win rate that looks like a real 0%.
	var empty TankStats
	if empty.WinRate() != 0 || empty.DPG() != 0 || empty.SurvivalRate() != 0 {
		t.Error("derived figures on an empty row are not zero")
	}
}

func TestPutTankStatsSkipsIncompleteRows(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	id := putSnapshotAt(t, db, "tanks/stats", base)

	if _, err := db.PutTankStats(ctx, []TankStats{
		{SnapshotID: id, TankID: 0, Mode: "random", Battles: 10},
		{SnapshotID: id, TankID: 4929, Mode: "", Battles: 10},
		{SnapshotID: 0, TankID: 4929, Mode: "random", Battles: 10},
	}); err != nil {
		t.Fatalf("PutTankStats: %v", err)
	}
	if _, err := db.LatestTankStats(ctx, "random"); !errors.Is(err, ErrNotFound) {
		t.Errorf("incomplete rows were stored: %v", err)
	}
}

// TestTankStatsSinceComputesADelta is the core of recent form on the Wargaming
// side, since the API is cumulative only.
func TestTankStatsSinceComputesADelta(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	// 45 days ago: the baseline.
	oldID := putSnapshotAt(t, db, "tanks/stats", base.Add(-45*24*time.Hour))
	if _, err := db.PutTankStats(ctx, []TankStats{{
		SnapshotID: oldID, TankID: 4929, Mode: "random",
		Battles: 500, Wins: 250, Survived: 150, DamageDealt: 550000, Frags: 560, XP: 390000,
		AvgDamageAssisted: 800,
	}}); err != nil {
		t.Fatalf("PutTankStats(baseline): %v", err)
	}

	// Now: 29 more battles, played noticeably better.
	newID := putSnapshotAt(t, db, "tanks/stats", base)
	if _, err := db.PutTankStats(ctx, []TankStats{{
		SnapshotID: newID, TankID: 4929, Mode: "random",
		Battles: 529, Wins: 269, Survived: 159, DamageDealt: 592480, Frags: 602, XP: 420100,
		AvgDamageAssisted: 845.6,
	}}); err != nil {
		t.Fatalf("PutTankStats(latest): %v", err)
	}

	deltas, err := db.TankStatsSince(ctx, "random", base.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("TankStatsSince: %v", err)
	}
	if len(deltas) != 1 {
		t.Fatalf("got %d deltas, want 1", len(deltas))
	}

	d := deltas[0]
	if d.Delta.Battles != 29 {
		t.Errorf("delta battles = %d, want 29", d.Delta.Battles)
	}
	if d.Delta.Wins != 19 {
		t.Errorf("delta wins = %d, want 19", d.Delta.Wins)
	}
	if d.Delta.DamageDealt != 42480 {
		t.Errorf("delta damage = %d, want 42480", d.Delta.DamageDealt)
	}
	// The interval DPG is far above the lifetime figure, which is the whole
	// point of computing it.
	if dpg := d.Delta.DPG(); dpg < 1464 || dpg > 1466 {
		t.Errorf("interval DPG = %.1f, want about 1465", dpg)
	}
	if wr := d.Delta.WinRate(); wr < 0.655 || wr > 0.656 {
		t.Errorf("interval win rate = %.4f, want about 0.6552", wr)
	}

	// The window reported is the one the data actually spans, not the 30 days
	// asked for, so a caller can state it honestly.
	if got := d.Window(); got != 45*24*time.Hour {
		t.Errorf("Window = %s, want 45 days - the real span of the baseline", got)
	}
	if !d.BaselineAt.Equal(base.Add(-45 * 24 * time.Hour)) {
		t.Errorf("BaselineAt = %s", d.BaselineAt)
	}
}

// TestAssistDeltaIsWeightedNotSubtracted: the difference of two averages is
// meaningless, so the interval average is recovered by weighting each side by
// its battle count.
func TestAssistDeltaIsWeightedNotSubtracted(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	oldID := putSnapshotAt(t, db, "tanks/stats", base.Add(-45*24*time.Hour))
	if _, err := db.PutTankStats(ctx, []TankStats{{
		SnapshotID: oldID, TankID: 4929, Mode: "all",
		Battles: 100, AvgDamageAssisted: 500,
	}}); err != nil {
		t.Fatalf("PutTankStats: %v", err)
	}

	newID := putSnapshotAt(t, db, "tanks/stats", base)
	if _, err := db.PutTankStats(ctx, []TankStats{{
		SnapshotID: newID, TankID: 4929, Mode: "all",
		Battles: 110, AvgDamageAssisted: 550,
	}}); err != nil {
		t.Fatalf("PutTankStats: %v", err)
	}

	deltas, err := db.TankStatsSince(ctx, "all", base.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("TankStatsSince: %v", err)
	}
	if len(deltas) != 1 {
		t.Fatalf("got %d deltas, want 1", len(deltas))
	}

	// (550*110 - 500*100) / 10 = 1050. Naive subtraction would have said 50,
	// which would be wrong by a factor of twenty.
	if got := deltas[0].Delta.AvgDamageAssisted; got < 1049 || got > 1051 {
		t.Errorf("interval assist = %.1f, want 1050", got)
	}
}

func TestWeightedDeltaEdgeCases(t *testing.T) {
	cases := []struct {
		name                   string
		latestAvg, baselineAvg float64
		latestN, baselineN     int
		want                   float64
	}{
		{"no new battles", 500, 500, 100, 100, 0},
		{"negative battle count", 500, 500, 90, 100, 0},
		// A total that goes backwards cannot happen from real play; it means the
		// inputs disagree, and inventing a negative average would be worse than
		// reporting nothing.
		{"total decreased", 100, 900, 110, 100, 0},
		{"clean case", 600, 500, 200, 100, 700},
	}
	for _, tc := range cases {
		got := weightedDelta(tc.latestAvg, tc.latestN, tc.baselineAvg, tc.baselineN)
		if got != tc.want {
			t.Errorf("%s: weightedDelta = %.1f, want %.1f", tc.name, got, tc.want)
		}
	}
}

// TestTankStatsSinceRefusesWithoutABaseline is the rule that prevents the single
// worst failure mode: claiming a trend when no snapshot is old enough to
// support one.
func TestTankStatsSinceRefusesWithoutABaseline(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	id := putSnapshotAt(t, db, "tanks/stats", base)
	if _, err := db.PutTankStats(ctx, []TankStats{{
		SnapshotID: id, TankID: 4929, Mode: "random", Battles: 529,
	}}); err != nil {
		t.Fatalf("PutTankStats: %v", err)
	}

	// Only one snapshot exists, so a 30-day window has nothing to measure from.
	if _, err := db.TankStatsSince(ctx, "random", base.Add(-30*24*time.Hour)); !errors.Is(err, ErrNoBaseline) {
		t.Errorf("TankStatsSince = %v, want ErrNoBaseline", err)
	}
}

func TestTankStatsSinceIncludesTanksNewSinceTheBaseline(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	oldID := putSnapshotAt(t, db, "tanks/stats", base.Add(-45*24*time.Hour))
	if _, err := db.PutTankStats(ctx, []TankStats{{
		SnapshotID: oldID, TankID: 4929, Mode: "random", Battles: 500, DamageDealt: 550000,
	}}); err != nil {
		t.Fatalf("PutTankStats: %v", err)
	}

	// A tank bought since the baseline has no row there at all.
	newID := putSnapshotAt(t, db, "tanks/stats", base)
	if _, err := db.PutTankStats(ctx, []TankStats{
		{SnapshotID: newID, TankID: 4929, Mode: "random", Battles: 500, DamageDealt: 550000},
		{SnapshotID: newID, TankID: 26705, Mode: "random", Battles: 40, DamageDealt: 120000},
	}); err != nil {
		t.Fatalf("PutTankStats: %v", err)
	}

	deltas, err := db.TankStatsSince(ctx, "random", base.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("TankStatsSince: %v", err)
	}
	// The untouched tank is omitted; the new one is reported in full.
	if len(deltas) != 1 {
		t.Fatalf("got %d deltas, want 1: %+v", len(deltas), deltas)
	}
	if deltas[0].TankID != 26705 || deltas[0].Delta.Battles != 40 {
		t.Errorf("delta = %+v, want the new tank with all 40 battles", deltas[0])
	}
}

func TestLatestTankStatsReportsNotFoundOnAnEmptyStore(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.LatestTankStats(ctx, "random"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestTankStats = %v, want ErrNotFound", err)
	}
	if _, err := db.LatestTankStatsFor(ctx, 4929, "random"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestTankStatsFor = %v, want ErrNotFound", err)
	}
	if _, err := db.TankStatsSince(ctx, "random", base); !errors.Is(err, ErrNotFound) {
		t.Errorf("TankStatsSince = %v, want ErrNotFound", err)
	}
}

// TestLatestTankStatsPicksTheNewestSnapshot guards against reading a stale
// snapshot when several exist.
func TestLatestTankStatsPicksTheNewestSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	for i, at := range []time.Time{base.Add(-2 * time.Hour), base, base.Add(-time.Hour)} {
		id := putSnapshotAt(t, db, "tanks/stats", at)
		if _, err := db.PutTankStats(ctx, []TankStats{{
			SnapshotID: id, TankID: 4929, Mode: "random", Battles: 500 + i,
		}}); err != nil {
			t.Fatalf("PutTankStats: %v", err)
		}
	}

	got, err := db.LatestTankStatsFor(ctx, 4929, "random")
	if err != nil {
		t.Fatalf("LatestTankStatsFor: %v", err)
	}
	// The snapshot written second is the newest by requested_at.
	if got.Battles != 501 {
		t.Errorf("battles = %d, want 501 from the newest snapshot", got.Battles)
	}
}
