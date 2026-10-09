package query

import (
	"context"
	"sort"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/overlay"
)

// OnslaughtCaveat travels with every Onslaught figure.
const OnslaughtCaveat = "Onslaught figures are tomato.gg's, over the current season on this server. There is no players' win rate here, " +
	"so a tank's win rate mixes the tank with the skill of those who choose it (strong players gather in the strongest tanks); " +
	"win_rate_margin is the 95 % sampling margin, and a gap smaller than it is noise. Wargaming's API has no Onslaught statistics at all, " +
	"so the player's own Onslaught results and rating are unknown: ask the player"

// OnslaughtLine is one tank's season figures.
type OnslaughtLine struct {
	Battles       int     `json:"battles"`
	WinRate       float64 `json:"win_rate"`
	WinRateMargin float64 `json:"win_rate_margin"`
	DPG           int     `json:"dpg"`
	Frags         float64 `json:"frags"`
	Spots         float64 `json:"spots"`
	Assist        int     `json:"assist"`
	Blocked       int     `json:"blocked"`
	SurvivalRate  float64 `json:"survival_rate"`
	AvgBaseXP     int     `json:"avg_base_xp"`
}

// OnslaughtRow is one tank of the season.
type OnslaughtRow struct {
	TankID    int    `json:"tank_id"`
	Name      string `json:"name"`
	Tier      int    `json:"tier"`
	Class     string `json:"class"`
	IsPremium bool   `json:"is_premium"`
	Owned     bool   `json:"owned"`
	OnslaughtLine
}

// RatingSummary condenses the season's rating distribution: how many
// players are rated, and the rating it takes to be in the top half,
// quarter, tenth and hundredth.
type RatingSummary struct {
	Players int `json:"players"`
	// TopPercent maps a share of players ("50", "25", "10", "1") to the
	// lowest rating bucket that reaches it.
	TopPercent map[string]int `json:"top_percent_from"`
}

// Onslaught is `meta onslaught`.
type Onslaught struct {
	Season string `json:"season"`
	// Tank is the one vehicle asked about; Rows the ranked list otherwise.
	Tank    *OnslaughtRow  `json:"tank,omitempty"`
	Rows    []OnslaughtRow `json:"rows,omitempty"`
	Dropped int            `json:"dropped_thin_rows,omitempty"`
	Ratings *RatingSummary `json:"ratings,omitempty"`
}

// OnslaughtFilter narrows the ranked list.
type OnslaughtFilter struct {
	Class      string
	Owned      bool
	MinBattles int
}

// Onslaught reports one tank's season, or ranks the season's tanks by win
// rate, with the rating distribution either way.
func (s *Service) Onslaught(ctx context.Context, ref string, f OnslaughtFilter, table *meta.OnslaughtTable) (Envelope, error) {
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcVehicles)
	r.metaPage(table.URL, "Onslaught page", table.Updated)
	r.caveat("%s", OnslaughtCaveat)

	vehicles, err := s.DB.AllVehicles(ctx)
	if err != nil {
		return Envelope{}, err
	}
	owned, err := r.ownedSet()
	if err != nil {
		return Envelope{}, err
	}
	row := func(id int, o meta.OnslaughtTank) OnslaughtRow {
		tier, class, name, premium := o.Tier, overlay.CanonicalClass(o.Class), o.Name, o.Premium
		if v, ok := vehicles[id]; ok {
			tier, class, name, premium = v.Tier, v.Type, v.Name, v.IsPremium
		}
		return OnslaughtRow{TankID: id, Name: name, Tier: tier, Class: class, IsPremium: premium, Owned: owned[id],
			OnslaughtLine: OnslaughtLine{
				Battles: o.Battles, WinRate: o.WinRate, WinRateMargin: winRateMargin(o.WinRate, o.Battles),
				DPG: o.Damage, Frags: o.Frags, Spots: o.Spots, Assist: o.SpottingAssist + o.TrackingAssist,
				Blocked: o.DamageBlocked, SurvivalRate: o.Survival, AvgBaseXP: o.BaseXP,
			}}
	}

	out := Onslaught{Season: table.Season, Ratings: ratingSummary(table.Ratings)}
	if ref != "" {
		v, err := s.resolveTank(ctx, ref)
		if err != nil {
			return Envelope{}, err
		}
		if o, ok := table.ByTank[v.TankID]; ok {
			t := row(v.TankID, o)
			out.Tank = &t
		} else {
			r.caveat("%s has no row in this season's Onslaught table: not played in Onslaught, or too rarely", v.Name)
		}
		return r.finish(out), nil
	}

	out.Rows = []OnslaughtRow{}
	for id, o := range table.ByTank {
		rw := row(id, o)
		if (f.Class != "" && rw.Class != f.Class) || (f.Owned && !rw.Owned) {
			continue
		}
		if rw.Battles < f.MinBattles {
			out.Dropped++
			continue
		}
		out.Rows = append(out.Rows, rw)
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		if out.Rows[i].WinRate != out.Rows[j].WinRate {
			return out.Rows[i].WinRate > out.Rows[j].WinRate
		}
		return out.Rows[i].TankID < out.Rows[j].TankID
	})
	if out.Dropped > 0 {
		r.caveat("%d tank(s) with fewer than %d Onslaught battles this season were left out as too thin to rank", out.Dropped, f.MinBattles)
	}
	return r.finish(out), nil
}

// ratingSummary finds, for each share of players, the lowest bucket at
// which that share of the season's rated players is at or above it.
func ratingSummary(buckets []meta.RatingBucket) *RatingSummary {
	total := 0
	for _, b := range buckets {
		total += b.Players
	}
	if total == 0 {
		return nil
	}
	out := &RatingSummary{Players: total, TopPercent: map[string]int{}}
	for _, share := range []struct {
		key  string
		frac float64
	}{{"50", 0.5}, {"25", 0.25}, {"10", 0.1}, {"1", 0.01}} {
		above := 0
		for i := len(buckets) - 1; i >= 0; i-- {
			above += buckets[i].Players
			if float64(above) >= share.frac*float64(total) {
				out.TopPercent[share.key] = buckets[i].Rating
				break
			}
		}
	}
	return out
}
