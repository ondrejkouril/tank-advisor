package query

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// MoECaveat qualifies every player estimate against mark thresholds.
const MoECaveat = "the player's figure is a range, not a mark progress: marks use damage plus the larger of " +
	"spotting and tracking assist in each battle, averaged over recent battles with more weight on the " +
	"latest, which cumulative statistics cannot reproduce. The range bounds the per-battle average: low is " +
	"damage plus the larger average assist, high is damage plus both. Ask the player for their actual mark percentage"

// NotEliteCaveat travels with every tank whose client block says elite is
// false. Reading false as "modules are locked" failed evaluation runs 8 and
// 9 (docs/evals.md): the data cannot tell the two apart, so the query says
// so where the figure is, rather than only in the references.
const NotEliteCaveat = "elite is false: a vehicle is elite only when every module and every follow-on vehicle is researched, " +
	"so false does not show that any module is locked (an unresearched follow-on is enough); the data cannot tell the two apart. " +
	"Do not say modules are locked, or advise on them, unless the player confirms it"

// MoEClientCaveat replaces MoECaveat when the game client's own figures are
// known: they are the real progress, and the ranges are only a comparison.
const MoEClientCaveat = "client holds the player's actual marks and progress as the garage showed them (client mod " +
	"dump; its moving_avg_damage is the figure the thresholds apply to); the lifetime and recent ranges are " +
	"estimates from cumulative statistics, kept for comparison"

// Marks is `wotctx meta moe`.
type Marks struct {
	TankID int    `json:"tank_id"`
	Name   string `json:"name"`
	Tier   int    `json:"tier"`

	// Thresholds are combined damage per battle for 65, 85, 95 and 100 %.
	Thresholds map[int]int `json:"thresholds"`
	// Change30d is how far each threshold moved in 30 days.
	Change30d map[int]int `json:"change_30d"`

	// Lifetime and Recent bound the player's combined damage; nil when there
	// are no battles in that scope.
	Lifetime *CombinedRange `json:"lifetime"`
	Recent   *CombinedRange `json:"recent_30d"`
	// RecentReason explains a missing recent range.
	RecentReason string `json:"recent_reason,omitempty"`

	// Client is the player's actual marks and progress as the garage shows
	// them, from the client mod's dump; absent without one.
	Client *ClientMarks `json:"client,omitempty"`
	// ClientCapturedAt is when the game wrote those figures.
	ClientCapturedAt *time.Time `json:"client_captured_at,omitempty"`
}

// CombinedRange bounds the player's average combined damage per battle.
type CombinedRange struct {
	Battles    int    `json:"battles"`
	Confidence string `json:"confidence"`
	Low        int    `json:"low"`
	High       int    `json:"high"`
	Span       *Span  `json:"span,omitempty"`
}

// MoE reports a vehicle's mark thresholds from a freshly fetched table,
// beside the player's own combined damage. A nil table means the fetch is
// switched off (meta.moe_fetch, docs/spec-desktop.md section 12.4): the
// player's own marks are still reported, and the thresholds are said to be
// unavailable.
func (s *Service) MoE(ctx context.Context, ref string, table *meta.MoETable) (Envelope, error) {
	v, err := s.resolveTank(ctx, ref)
	if err != nil {
		return Envelope{}, err
	}
	if v.Tier < 5 {
		return Envelope{}, fmt.Errorf("%s is tier %d; marks of excellence start at tier V", v.Name, v.Tier)
	}
	var m meta.MoE
	if table != nil {
		row, ok := table.ByTank[v.TankID]
		if !ok {
			return Envelope{}, fmt.Errorf("tomato.gg's table has no row for %s (tank_id %d)", v.Name, v.TankID)
		}
		m = row
	}

	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	if table != nil {
		// The page is fetched, not cached, so it is its own source: its age is
		// how long since tomato.gg recomputed it.
		r.meta.Sources = append(r.meta.Sources, Source{
			// Clamped: a page stamped ahead of the local clock is fresh, not from the future.
			Name: table.URL, ObservedAt: table.Updated, AgeSeconds: max(0, int64(r.now.Sub(table.Updated).Seconds())),
		})
		r.used[table.URL] = true
	} else {
		r.caveat("mark thresholds are unavailable: fetching tomato.gg's table is switched off (meta.moe_fetch in config.yaml)")
	}
	r.use(srcTankStats)

	// With the client's own figures there is nothing to estimate: the range
	// stays for comparison, and the caveat says which figure is which.
	var clientMarks *ClientMarks
	var capturedAt *time.Time
	if c := r.client(); c != nil {
		if m, ok := c.marks[v.TankID]; ok {
			clientMarks = &ClientMarks{Marks: m.Marks, Percent: m.Percent, MovingAvgDamage: c.vehicles[v.TankID].MovingAvgDamage}
			t := c.account.CapturedAt
			capturedAt = &t
		}
	}
	if clientMarks != nil {
		r.caveat("%s", MoEClientCaveat)
	} else {
		r.caveat("%s", MoECaveat)
	}

	out := Marks{
		TankID: v.TankID, Name: v.Name, Tier: v.Tier,
		Thresholds: m.Thresholds, Change30d: m.Change30d,
		Client: clientMarks, ClientCapturedAt: capturedAt,
	}

	random, err := s.DB.LatestTankStatsFor(ctx, v.TankID, "random")
	switch {
	case errors.Is(err, store.ErrNotFound):
		r.caveat("no random battles recorded on this vehicle")
	case err != nil:
		return Envelope{}, err
	default:
		out.Lifetime = s.combined([]store.TankStats{random}, nil)
		rec, err := r.recentFor(v.TankID, 30, nil)
		if err != nil {
			return Envelope{}, err
		}
		if rec.Available && rec.Stats != nil && rec.Stats.Battles > 0 {
			deltas, _, _, err := r.windowDeltas(Window{Days: 30})
			if err != nil {
				return Envelope{}, err
			}
			var rows []store.TankStats
			for _, d := range deltas {
				if d.TankID == v.TankID {
					rows = append(rows, d.Delta)
				}
			}
			out.Recent = s.combined(rows, rec.Span)
		} else if rec.Available {
			out.RecentReason = "no battles on this vehicle in the window"
		} else {
			out.RecentReason = rec.Reason
		}
	}
	return r.finish(out), nil
}

// combined bounds the average combined damage over rows.
func (s *Service) combined(rows []store.TankStats, span *Span) *CombinedRange {
	var battles, dmg, radio, track int
	for _, row := range rows {
		battles += row.Battles
		dmg += row.DamageDealt
		radio += row.RadioAssistedDamage
		track += row.TrackAssistedDamage
	}
	if battles == 0 {
		return nil
	}
	n := float64(battles)
	low := float64(dmg+max(radio, track)) / n
	high := float64(dmg+radio+track) / n
	return &CombinedRange{
		Battles: battles, Confidence: s.Config.Confidence.Classify(battles),
		Low: int(low + 0.5), High: int(high + 0.5), Span: span,
	}
}
