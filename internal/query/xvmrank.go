package query

import (
	"errors"
	"net/http"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/xvm"
)

const srcScales = "xvm:scales"

// XVMRank places the account on XVM's scales: the share of the players XVM
// rates whose figure is lower, 0 to 100.
type XVMRank struct {
	WN8     *int `json:"wn8,omitempty"`
	WinRate *int `json:"win_rate,omitempty"`
}

// xvmRank ranks a lifetime, all-tier line. It is nil, with a caveat, when the
// scales were never synced: a rank is an addition, never a reason to fail.
func (r *run) xvmRank(l *StatLine) *XVMRank {
	if l == nil || l.Battles == 0 {
		return nil
	}
	snap, err := r.s.DB.LatestUsableSnapshot(r.ctx, "xvm", "scales")
	if errors.Is(err, store.ErrNotFound) {
		r.caveat("XVM's rating scales have never been synced, so no percentile rank is given (run: wotctx sync --only wn8)")
		return nil
	}
	if err != nil {
		return nil
	}
	scales, err := xvm.ParseScales(snap.Raw)
	if err != nil {
		r.caveat("XVM's stored rating scales do not parse (%s), so no percentile rank is given", oneLine(err.Error()))
		return nil
	}
	r.use(srcScales)
	if entry, err := r.s.DB.CacheEntryFor(r.ctx, srcScales); err == nil {
		if t, err := http.ParseTime(entry.LastModified); err == nil {
			for i := range r.meta.Sources {
				if r.meta.Sources[i].Name == srcScales {
					r.meta.Sources[i].Version = t.UTC().Format(time.DateOnly)
				}
			}
		}
	}

	rank := &XVMRank{}
	if l.WN8 != nil {
		n := xvm.Rank(scales.WN8, *l.WN8)
		rank.WN8 = &n
	}
	wr := xvm.Rank(scales.WinRate, l.WinRate)
	rank.WinRate = &wr
	r.caveat("xvm_rank is the share of the players XVM rates whose lifetime figure is lower (XVM's scale, 0 to 100). "+
		"XVM rates a field stronger than the whole server - its midpoint win rate is %.1f %% and its midpoint WN8 %.0f - "+
		"so the rank is against that field, not against everyone who plays", scales.WinRate[49], scales.WN8[49])
	return rank
}

// rankable says whether a performance result is the account's whole lifetime
// record, the only thing XVM's scales describe.
func rankable(w Window, minTier int) bool { return w.Lifetime && minTier == 0 }
