package query

import (
	"context"
	"sort"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// Sessions is `query sessions`.
//
// With no battle log, the finest grain available is the interval between two
// syncs: battles played on Saturday and first synced on Wednesday are one
// interval, and cannot be split (spec section 7.4). Each interval says exactly
// what it spans rather than pretending to be a play session.
type Sessions struct {
	Window string `json:"window"`
	// Complete is false when history does not reach back to the start of the
	// requested window, so the intervals cover only part of it.
	Complete bool   `json:"complete"`
	Span     *Span  `json:"span,omitempty"`
	Reason   string `json:"reason,omitempty"`

	Total     *StatLine  `json:"total,omitempty"`
	Intervals []Interval `json:"intervals"`
	// EmptyIntervals counts syncs between which no random battles were played.
	EmptyIntervals int `json:"empty_intervals"`
}

// Interval is the play between two consecutive syncs.
type Interval struct {
	From  time.Time    `json:"from"`
	To    time.Time    `json:"to"`
	Stats StatLine     `json:"stats"`
	Tanks []TankPlayed `json:"tanks"`
}

// TankPlayed is one tank's share of an interval.
type TankPlayed struct {
	TankID  int    `json:"tank_id"`
	Name    string `json:"name"`
	Battles int    `json:"battles"`
}

// Sessions reports play in the window, broken down by sync interval.
func (s *Service) Sessions(ctx context.Context, w Window) (Envelope, error) {
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcTankStats, srcVehicles)
	out := Sessions{Window: w.String(), Intervals: []Interval{}}

	refs, err := s.DB.TankStatsSnapshots(ctx, wg.ModeRandom)
	if err != nil {
		return Envelope{}, err
	}
	if len(refs) < 2 {
		out.Reason = "fewer than two snapshots, so no interval can be measured yet"
		r.caveat("%s", out.Reason)
		return r.finish(out), nil
	}

	// Start at the newest snapshot at or before the window start. With none
	// that old, start at the first snapshot and say the window is incomplete.
	cutoff := r.now.Add(-time.Duration(w.Days) * 24 * time.Hour)
	start := 0
	out.Complete = false
	for i, ref := range refs {
		if !ref.At.After(cutoff) {
			start = i
			out.Complete = true
		}
	}
	if start == len(refs)-1 {
		out.Reason = "no sync since the start of the window"
		r.caveat("%s", out.Reason)
		return r.finish(out), nil
	}

	vehicles, err := s.DB.AllVehicles(ctx)
	if err != nil {
		return Envelope{}, err
	}
	expected := r.expected()

	var all []store.TankStats
	for i := start + 1; i < len(refs); i++ {
		deltas, err := s.DB.TankStatsBetween(ctx, wg.ModeRandom, refs[i-1], refs[i])
		if err != nil {
			return Envelope{}, err
		}
		if len(deltas) == 0 {
			out.EmptyIntervals++
			continue
		}
		rows := deltaRows(deltas)
		all = append(all, rows...)

		iv := Interval{From: refs[i-1].At, To: refs[i].At, Stats: s.line(rows, expected)}
		for _, d := range deltas {
			iv.Tanks = append(iv.Tanks, TankPlayed{TankID: d.TankID, Name: vehicles[d.TankID].Name, Battles: d.Delta.Battles})
		}
		sort.Slice(iv.Tanks, func(a, b int) bool {
			if iv.Tanks[a].Battles != iv.Tanks[b].Battles {
				return iv.Tanks[a].Battles > iv.Tanks[b].Battles
			}
			return iv.Tanks[a].TankID < iv.Tanks[b].TankID
		})
		out.Intervals = append(out.Intervals, iv)
	}

	last := refs[len(refs)-1]
	out.Span = &Span{
		Requested: w.String(), From: refs[start].At, To: last.At,
		Days: round(last.At.Sub(refs[start].At).Hours()/24, 1),
	}
	if len(all) > 0 {
		total := s.line(all, expected)
		out.Total = &total
	}
	if out.Complete {
		r.spanCaveat(out.Span, w.Days)
	} else {
		r.caveat("history begins %s, so only %.1f of the %d requested days are covered",
			refs[0].At.Format("2006-01-02"), out.Span.Days, w.Days)
	}
	if len(all) > 0 {
		r.caveat("%s", AssistCaveat)
	}
	return r.finish(out), nil
}
