package query

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// Dimensions `query performance --by` accepts.
const (
	ByClass  = "class"
	ByTier   = "tier"
	ByNation = "nation"
)

// Performance is `query performance`.
type Performance struct {
	By     string `json:"by"`
	Window string `json:"window"`
	// MinTier, when set, leaves out tanks below that tier: the way to compare
	// current play without years-old low-tier games in the totals.
	MinTier   int  `json:"min_tier,omitempty"`
	Available bool `json:"available"`
	// Span is the interval actually covered, for a windowed result.
	Span *Span `json:"span,omitempty"`
	// Reason explains an unavailable window.
	Reason string `json:"reason,omitempty"`

	Total *StatLine `json:"total,omitempty"`
	Rows  []Rollup  `json:"rows"`
}

// Rollup is one group's line.
type Rollup struct {
	Key   string   `json:"key"`
	Tanks int      `json:"tanks"`
	Stats StatLine `json:"stats"`
}

// Performance rolls random-battle statistics up by class, tier or nation,
// over a lifetime or a window.
func (s *Service) Performance(ctx context.Context, by string, w Window, minTier int) (Envelope, error) {
	switch by {
	case ByClass, ByTier, ByNation:
	default:
		return Envelope{}, fmt.Errorf("--by %q: want class, tier or nation", by)
	}

	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcTankStats, srcVehicles)
	out := Performance{By: by, Window: w.String(), MinTier: minTier, Rows: []Rollup{}}

	var rows []store.TankStats
	if w.Lifetime {
		latest, err := s.DB.LatestTankStats(ctx, "random")
		if errors.Is(err, store.ErrNotFound) {
			out.Reason = "no tank statistics synced"
			r.caveat("%s", out.Reason)
			return r.finish(out), nil
		}
		if err != nil {
			return Envelope{}, err
		}
		rows = latest
	} else {
		deltas, span, history, err := r.windowDeltas(w)
		switch {
		case errors.Is(err, store.ErrNoBaseline), errors.Is(err, store.ErrNotFound):
			out.Reason = notYet(history, w.Days)
			r.caveat("%s", out.Reason)
			return r.finish(out), nil
		case err != nil:
			return Envelope{}, err
		}
		out.Span = span
		rows = deltaRows(deltas)
	}
	out.Available = true

	vehicles, err := s.DB.AllVehicles(ctx)
	if err != nil {
		return Envelope{}, err
	}
	expected := r.expected()
	r.caveat("%s", AssistCaveat)

	if minTier > 0 {
		kept := make([]store.TankStats, 0, len(rows))
		for _, row := range rows {
			if vehicles[row.TankID].Tier >= minTier {
				kept = append(kept, row)
			}
		}
		rows = kept
	}

	groups := map[string][]store.TankStats{}
	for _, row := range rows {
		if row.Battles <= 0 {
			continue
		}
		groups[groupKey(by, vehicles[row.TankID])] = append(groups[groupKey(by, vehicles[row.TankID])], row)
	}
	for key, group := range groups {
		out.Rows = append(out.Rows, Rollup{Key: key, Tanks: len(group), Stats: s.line(group, expected)})
	}
	sort.Slice(out.Rows, func(i, j int) bool { return rollupLess(by, out.Rows[i].Key, out.Rows[j].Key) })

	total := s.line(rows, expected)
	out.Total = &total
	if unknown, ok := groups["unknown"]; ok {
		r.caveat("%d tank(s) with battles are missing from the vehicle list and are grouped as unknown", len(unknown))
	}
	return r.finish(out), nil
}

func groupKey(by string, v store.Vehicle) string {
	if v.TankID == 0 {
		return "unknown"
	}
	switch by {
	case ByTier:
		return fmt.Sprint(v.Tier)
	case ByNation:
		return v.Nation
	default:
		return v.Type
	}
}

// rollupLess orders tiers numerically and everything else by name. The order
// is a presentation convenience, not a ranking.
func rollupLess(by, a, b string) bool {
	if by == ByTier {
		var x, y int
		fmt.Sscan(a, &x)
		fmt.Sscan(b, &y)
		return x < y
	}
	return a < b
}
