package syncer

import (
	"context"
	"fmt"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

const (
	wn8Source   = "xvm"
	wn8Endpoint = "wn8exp"
)

var wn8SourceKey = store.SourceKey(wn8Source, wn8Endpoint)

// syncWN8Expected refreshes XVM's expected values.
func (s *Syncer) syncWN8Expected(ctx context.Context, report *Report) error {
	due, err := s.due(ctx, wn8Source, wn8Endpoint, wn8SourceKey, report)
	if err != nil || !due {
		return err
	}
	if s.DryRun {
		return s.reportIntent(wn8SourceKey, report)
	}

	result, fetchErr := s.XVM.Fetch(ctx)

	// Record before inspecting, like every other source. Nothing about the
	// request carries account data, so the raw body is safe to keep verbatim.
	// A failure is marked in the error column, which is what makes a snapshot
	// unusable: otherwise a 404 page would count as fresh data and block a
	// retry for the whole TTL.
	if len(result.Raw) > 0 || result.HTTPStatus != 0 {
		snap := store.Snapshot{
			Source:      wn8Source,
			Endpoint:    wn8Endpoint,
			RequestedAt: result.RequestedAt,
			HTTPStatus:  result.HTTPStatus,
			Raw:         result.Raw,
		}
		if fetchErr != nil {
			snap.WGError = oneLine(fetchErr.Error())
		}
		if _, err := s.DB.PutSnapshot(ctx, snap); err != nil {
			return err
		}
	}
	if fetchErr != nil {
		return fetchErr
	}

	values := make([]store.WN8Expected, 0, len(result.Expected))
	for _, e := range result.Expected {
		values = append(values, store.WN8Expected{
			TankID: e.TankID, Damage: e.Damage, Frags: e.Frags,
			Spot: e.Spot, Def: e.Def, WinRate: e.WinRate,
		})
	}
	n, err := s.DB.ReplaceWN8Expected(ctx, result.Version, result.RequestedAt, values)
	if err != nil {
		return err
	}

	report.Counts[wn8SourceKey+":tanks"] = n
	if result.Skipped > 0 {
		report.Caveats = append(report.Caveats,
			fmt.Sprintf("%s: %d entr(ies) with non-positive values skipped", wn8SourceKey, result.Skipped))
	}
	report.Synced = append(report.Synced, wn8SourceKey)
	s.logf("%s: version %s, %d vehicles", wn8SourceKey, result.Version, n)
	return nil
}
