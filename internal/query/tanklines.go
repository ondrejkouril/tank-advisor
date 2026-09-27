package query

import (
	"context"
	"errors"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// TankLines is one StatLine per tank over a window. It is for callers such as
// the brief that lay many tanks side by side and carry provenance themselves,
// so it is returned bare rather than in an envelope.
type TankLines struct {
	Available bool
	Span      *Span
	// Reason explains an unavailable window, as notYet phrases it.
	Reason string
	Lines  map[int]StatLine
}

// TankLines returns per-tank random-battle lines for a window. Tanks with no
// battles in the window are absent.
func (s *Service) TankLines(ctx context.Context, w Window) (TankLines, error) {
	r, err := s.begin(ctx)
	if err != nil {
		return TankLines{}, err
	}
	out := TankLines{Lines: map[int]StatLine{}}

	var rows []store.TankStats
	if w.Lifetime {
		latest, err := s.DB.LatestTankStats(ctx, "random")
		if errors.Is(err, store.ErrNotFound) {
			out.Reason = "no tank statistics synced"
			return out, nil
		}
		if err != nil {
			return TankLines{}, err
		}
		rows = latest
	} else {
		deltas, span, history, err := r.windowDeltas(w)
		switch {
		case errors.Is(err, store.ErrNoBaseline), errors.Is(err, store.ErrNotFound):
			out.Reason = notYet(history, w.Days)
			return out, nil
		case err != nil:
			return TankLines{}, err
		}
		out.Span = span
		rows = deltaRows(deltas)
	}

	expected := r.expected()
	for _, row := range rows {
		if row.Battles > 0 {
			out.Lines[row.TankID] = s.line([]store.TankStats{row}, expected)
		}
	}
	out.Available = true
	return out, nil
}

// History reports how much tank-statistics history the cache holds. Its first
// snapshot is day zero: nothing earlier can ever be known.
func (s *Service) History(ctx context.Context) (History, error) {
	refs, err := s.DB.TankStatsSnapshots(ctx, "random")
	if err != nil || len(refs) == 0 {
		return History{}, err
	}
	return History{
		FirstSnapshot: refs[0].At,
		Days:          round(s.now().Sub(refs[0].At).Hours()/24, 1),
		Snapshots:     len(refs),
	}, nil
}
