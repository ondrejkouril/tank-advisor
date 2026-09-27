// Package wn8 computes WN8 from cumulative statistics and XVM's expected values
// (docs/spec.md section 6.2).
//
// Two rules shape it. The account figure is computed on the battle aggregate -
// actual totals against expected values times battles, summed over every
// scored tank - not as a mean of per-tank ratings, which weights a 20-battle
// tank like a 2,000-battle one. And a tank that cannot be scored is excluded
// and reported, never scored against zero.
package wn8

import (
	"math"
	"sort"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// Reasons a tank is left out of a rating.
const (
	// ReasonNoExpected: XVM publishes no expected values for the vehicle -
	// typically one too new, hidden, or event-only.
	ReasonNoExpected = "no expected values"
	// ReasonOldParser: the row predates parsing of defence points, which WN8
	// needs. A re-parse on the next sync fixes it.
	ReasonOldParser = "statistics predate defence-point parsing"
)

// totals is one side of the comparison: what was achieved, or what was
// expected over the same battles.
type totals struct {
	damage, frags, spot, def, wins float64
}

// rating is the standard WN8 formula applied to actual versus expected totals.
// For a single tank this is identical to the per-battle-average form, since
// the battle counts cancel.
func rating(actual, expected totals) float64 {
	rDamage := actual.damage / expected.damage
	rFrag := actual.frags / expected.frags
	rSpot := actual.spot / expected.spot
	rDef := actual.def / expected.def
	rWin := actual.wins / expected.wins

	rWinC := math.Max(0, (rWin-0.71)/(1-0.71))
	rDamageC := math.Max(0, (rDamage-0.22)/(1-0.22))
	rFragC := math.Max(0, math.Min(rDamageC+0.2, (rFrag-0.12)/(1-0.12)))
	rSpotC := math.Max(0, math.Min(rDamageC+0.1, (rSpot-0.38)/(1-0.38)))
	rDefC := math.Max(0, math.Min(rDamageC+0.1, (rDef-0.10)/(1-0.10)))

	return 980*rDamageC + 210*rDamageC*rFragC + 155*rFragC*rSpotC + 75*rDefC*rFragC +
		145*math.Min(1.8, rWinC)
}

func actualOf(s store.TankStats) totals {
	return totals{
		damage: float64(s.DamageDealt),
		frags:  float64(s.Frags),
		spot:   float64(s.Spotted),
		def:    float64(s.DroppedCapturePoints),
		wins:   float64(s.Wins),
	}
}

func expectedOf(e store.WN8Expected, battles int) totals {
	n := float64(battles)
	return totals{
		damage: e.Damage * n,
		frags:  e.Frags * n,
		spot:   e.Spot * n,
		def:    e.Def * n,
		wins:   e.WinRate / 100 * n,
	}
}

func (t *totals) add(o totals) {
	t.damage += o.damage
	t.frags += o.frags
	t.spot += o.spot
	t.def += o.def
	t.wins += o.wins
}

// TankScore is one tank's rating.
type TankScore struct {
	TankID  int     `json:"tank_id"`
	Battles int     `json:"battles"`
	WN8     float64 `json:"wn8"`
}

// Excluded is a tank left out of the rating, with why.
type Excluded struct {
	TankID  int    `json:"tank_id"`
	Battles int    `json:"battles"`
	Reason  string `json:"reason"`
}

// Result is a rating over a set of tanks.
type Result struct {
	// WN8 is the battle-aggregate rating over the scored tanks. Zero when
	// nothing could be scored; check Battles.
	WN8 float64 `json:"wn8"`
	// Battles is how many battles the rating covers.
	Battles int `json:"battles"`

	Tanks    []TankScore `json:"tanks"`
	Excluded []Excluded  `json:"excluded,omitempty"`
	// ExcludedBattles is how many battles the excluded tanks account for, so a
	// reader can judge how much of the record the rating leaves out.
	ExcludedBattles int `json:"excluded_battles"`
}

// Compute rates the given rows, which must all be one mode - random battles,
// by convention, since that is what WN8 is defined on. Rows with no battles
// are ignored.
func Compute(rows []store.TankStats, expected map[int]store.WN8Expected) Result {
	var (
		result           Result
		actual, expTotal totals
	)
	for _, s := range rows {
		if s.Battles <= 0 {
			continue
		}
		if s.ParserVersion != 0 && s.ParserVersion < store.TankStatsParserVersion {
			result.Excluded = append(result.Excluded, Excluded{TankID: s.TankID, Battles: s.Battles, Reason: ReasonOldParser})
			result.ExcludedBattles += s.Battles
			continue
		}
		e, ok := expected[s.TankID]
		if !ok {
			result.Excluded = append(result.Excluded, Excluded{TankID: s.TankID, Battles: s.Battles, Reason: ReasonNoExpected})
			result.ExcludedBattles += s.Battles
			continue
		}

		a, x := actualOf(s), expectedOf(e, s.Battles)
		result.Tanks = append(result.Tanks, TankScore{TankID: s.TankID, Battles: s.Battles, WN8: rating(a, x)})
		actual.add(a)
		expTotal.add(x)
		result.Battles += s.Battles
	}

	if result.Battles > 0 {
		result.WN8 = rating(actual, expTotal)
	}
	sort.Slice(result.Tanks, func(i, j int) bool { return result.Tanks[i].TankID < result.Tanks[j].TankID })
	sort.Slice(result.Excluded, func(i, j int) bool { return result.Excluded[i].TankID < result.Excluded[j].TankID })
	return result
}

// ComputeDeltas rates the battles played between two snapshots - recent WN8.
// It is Compute over the differenced rows, which is why the delta arithmetic
// in the store has to be exact: every input here is a difference of totals.
func ComputeDeltas(deltas []store.TankStatsDelta, expected map[int]store.WN8Expected) Result {
	rows := make([]store.TankStats, 0, len(deltas))
	for _, d := range deltas {
		rows = append(rows, d.Delta)
	}
	return Compute(rows, expected)
}
