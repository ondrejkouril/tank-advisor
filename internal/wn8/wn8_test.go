package wn8

import (
	"math"
	"testing"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// The golden figures below were computed independently of this package, with
// an awk transcription of the published formula (wiki.wnefficiency.net), and
// are pinned to six decimals.

var expected = map[int]store.WN8Expected{
	1: {TankID: 1, Damage: 1400, Frags: 1.0, Spot: 1.2, Def: 0.8, WinRate: 52},
	2: {TankID: 2, Damage: 2200, Frags: 0.9, Spot: 1.5, Def: 0.6, WinRate: 50},
	9: {TankID: 9, Damage: 2000, Frags: 1, Spot: 1, Def: 1, WinRate: 50},
}

func row(tankID, battles, damage, frags, spotted, def, wins int) store.TankStats {
	return store.TankStats{
		TankID: tankID, Mode: "random", Battles: battles, DamageDealt: damage,
		Frags: frags, Spotted: spotted, DroppedCapturePoints: def, Wins: wins,
		ParserVersion: store.TankStatsParserVersion,
	}
}

var (
	tankA = row(1, 100, 150000, 110, 130, 70, 55)
	tankB = row(2, 400, 800000, 300, 700, 200, 200)
)

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s = %.6f, want %.6f", name, got, want)
	}
}

// TestAtExpectedIsExactly1565 is the one figure checkable by hand: every
// ratio is 1, every clamp is 1, and the weights sum to 980+210+155+75+145.
func TestAtExpectedIsExactly1565(t *testing.T) {
	e := expected[1]
	r := Compute([]store.TankStats{row(1, 1000, int(e.Damage*1000), 1000, 1200, 800, 520)}, expected)
	near(t, "WN8 at expected values", r.WN8, 1565)
}

func TestPerTankGoldenFigures(t *testing.T) {
	r := Compute([]store.TankStats{tankA, tankB}, expected)
	if len(r.Tanks) != 2 {
		t.Fatalf("tanks = %+v", r.Tanks)
	}
	near(t, "tank A", r.Tanks[0].WN8, 1766.606206)
	near(t, "tank B", r.Tanks[1].WN8, 1334.269705)
}

// TestAccountIsTheBattleAggregate is the step 7 acceptance criterion that
// proves which computation was implemented: the aggregate matches neither the
// mean of per-tank ratings nor their battle-weighted mean.
func TestAccountIsTheBattleAggregate(t *testing.T) {
	r := Compute([]store.TankStats{tankA, tankB}, expected)
	near(t, "account WN8", r.WN8, 1404.405805)
	if r.Battles != 500 {
		t.Errorf("battles = %d, want 500", r.Battles)
	}

	mean := (r.Tanks[0].WN8 + r.Tanks[1].WN8) / 2
	weighted := (r.Tanks[0].WN8*100 + r.Tanks[1].WN8*400) / 500
	for name, other := range map[string]float64{"mean": mean, "battle-weighted mean": weighted} {
		if math.Abs(r.WN8-other) < 10 {
			t.Errorf("account WN8 %.1f is within 10 of the %s %.1f; the aggregate is not what was computed", r.WN8, name, other)
		}
	}
}

// TestSecondaryRatiosAreClampedToDamage: frags far above damage cannot carry a
// rating, per the rDAMAGEc+0.2 cap.
func TestSecondaryRatiosAreClampedToDamage(t *testing.T) {
	// Per battle: half the expected damage, five times the expected frags.
	r := Compute([]store.TankStats{row(9, 2, 2000, 10, 2, 2, 1)}, expected)
	near(t, "clamped", r.WN8, 597.940565)
}

// TestMissingExpectedValuesAreExcludedNotZeroed is the other acceptance
// criterion: a tank XVM does not rate leaves the aggregate untouched and is
// reported, with its battles, rather than dragging the figure down.
func TestMissingExpectedValuesAreExcludedNotZeroed(t *testing.T) {
	unrated := row(26705, 50, 50000, 10, 10, 0, 20) // Executor-shaped: no expected values
	with := Compute([]store.TankStats{tankA, tankB, unrated}, expected)
	without := Compute([]store.TankStats{tankA, tankB}, expected)

	near(t, "account WN8 with an unrated tank", with.WN8, without.WN8)
	if with.Battles != 500 {
		t.Errorf("battles = %d, want the 500 scored", with.Battles)
	}
	if len(with.Excluded) != 1 || with.Excluded[0].TankID != 26705 ||
		with.Excluded[0].Reason != ReasonNoExpected || with.ExcludedBattles != 50 {
		t.Errorf("excluded = %+v (%d battles)", with.Excluded, with.ExcludedBattles)
	}
}

// TestUnparsedDefenceIsExcluded: a row from before defence points were parsed
// has def 0 because it was never read, not because none was earned.
func TestUnparsedDefenceIsExcluded(t *testing.T) {
	old := tankA
	old.ParserVersion = 1
	r := Compute([]store.TankStats{old, tankB}, expected)
	if len(r.Excluded) != 1 || r.Excluded[0].Reason != ReasonOldParser {
		t.Errorf("excluded = %+v", r.Excluded)
	}
	near(t, "account WN8", r.WN8, r.Tanks[0].WN8) // only B is scored
}

func TestNoBattlesMeansNoRating(t *testing.T) {
	r := Compute([]store.TankStats{row(1, 0, 0, 0, 0, 0, 0)}, expected)
	if r.WN8 != 0 || r.Battles != 0 || len(r.Tanks) != 0 || len(r.Excluded) != 0 {
		t.Errorf("result = %+v, want empty", r)
	}
	if got := Compute(nil, expected); got.Battles != 0 {
		t.Errorf("Compute(nil) = %+v", got)
	}
}

// TestRecentWN8RatesOnlyTheWindow: the same formula over a delta, so a
// window's rating reflects only the battles in it.
func TestRecentWN8RatesOnlyTheWindow(t *testing.T) {
	baseline := tankA
	latest := row(1, 200, 150000+150000, 110+110, 130+130, 70+70, 55+55)
	delta := store.TankStatsDelta{
		TankID: 1, Baseline: baseline, Latest: latest,
		Delta: row(1, 100, 150000, 110, 130, 70, 55),
	}
	r := ComputeDeltas([]store.TankStatsDelta{delta}, expected)
	near(t, "recent WN8", r.WN8, 1766.606206)
}
