package query

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/overlay"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// StaleAfter is when a server-wide page's own date earns a caveat: tomato.gg
// recomputes its tables daily, and two of its pages were found stuck for a
// month (docs/plan.md phase 6).
const StaleAfter = 14 * 24 * time.Hour

// ServerPerfCaveat travels with every server-wide performance figure.
const ServerPerfCaveat = "server figures are tomato.gg's, over every random battle on the server it records in the window; " +
	"they describe the tank across all its players, not the player. win_rate_differential is the tank's win rate minus " +
	"the overall win rate of the players who played it: above zero, the tank lifts its players above their usual; below, it holds them back. " +
	"win_rate_margin is the 95 % sampling margin of the win rate (at least that wide: battles of the same players are not independent), so a differential or a gap between tanks smaller than it is noise. " +
	"tomato.gg's WN8 uses its own expected values, so it is close to, but not the same scale as, the WN8 wotctx computes"

// ServerLine is one tank's server-wide figures over a window.
type ServerLine struct {
	Battles             int     `json:"battles"`
	WinRate             float64 `json:"win_rate"`
	PlayersWinRate      float64 `json:"players_win_rate"`
	WinRateDifferential float64 `json:"win_rate_differential"`
	WinRateMargin       float64 `json:"win_rate_margin"`
	DPG                 int     `json:"dpg"`
	Frags               float64 `json:"frags"`
	Spots               float64 `json:"spots"`
	Assist              int     `json:"assist"`
	AssistRadio         int     `json:"assist_radio"`
	AssistTrack         int     `json:"assist_track"`
	Blocked             int     `json:"blocked"`
	SurvivalRate        float64 `json:"survival_rate"`
	HitRate             float64 `json:"hit_rate"`
	PenRate             float64 `json:"pen_rate"`
	AvgBaseXP           int     `json:"avg_base_xp"`
	WN8                 int     `json:"wn8"`
	PlayersWN8          int     `json:"players_wn8"`
}

func serverLine(p meta.TankPerf) *ServerLine {
	return &ServerLine{
		Battles: p.Battles, WinRate: p.WinRate, PlayersWinRate: p.PlayersWinRate,
		WinRateDifferential: p.Differential, WinRateMargin: winRateMargin(p.WinRate, p.Battles),
		DPG: p.Damage, Frags: p.Frags, Spots: p.Spots,
		Assist: p.SpottingAssist + p.TrackingAssist, AssistRadio: p.SpottingAssist, AssistTrack: p.TrackingAssist,
		Blocked: p.DamageBlocked, SurvivalRate: p.Survival, HitRate: p.HitRate, PenRate: p.PenRate,
		AvgBaseXP: p.BaseXP, WN8: p.WN8, PlayersWN8: p.PlayersWN8,
	}
}

// ServerPerf is `meta perf <tank>`: the tank across the server, beside the
// player's own figures on it.
type ServerPerf struct {
	TankID int    `json:"tank_id"`
	Name   string `json:"name"`
	Tier   int    `json:"tier"`
	Class  string `json:"class"`
	Window string `json:"window"`

	// Server is nil when tomato.gg's table has no row for the tank.
	Server *ServerLine `json:"server"`

	// Lifetime is the player's random battles on the tank; nil if never played.
	Lifetime *StatLine `json:"lifetime"`
	// Recent is the player's figures over the same window as the server's.
	Recent *Recent `json:"recent,omitempty"`
	// Versus is the player minus the server, once per basis available.
	Versus []Versus `json:"versus,omitempty"`
}

// Versus is the player's figures minus the server's.
type Versus struct {
	Basis      string  `json:"basis"` // "lifetime" or the window
	Battles    int     `json:"battles"`
	Confidence string  `json:"confidence"`
	WinRate    float64 `json:"win_rate"` // percentage points
	DPG        float64 `json:"dpg"`
}

// ServerPerf reports one tank's server-wide figures from a freshly fetched
// table. A nil table means the fetch is switched off: the player's figures
// still come back, and the caveat says why the server's do not.
func (s *Service) ServerPerf(ctx context.Context, ref string, table *meta.PerfTable, switchName string) (Envelope, error) {
	v, err := s.resolveTank(ctx, ref)
	if err != nil {
		return Envelope{}, err
	}
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcTankStats)

	days := 30
	if table != nil {
		days = table.Days
	}
	out := ServerPerf{TankID: v.TankID, Name: v.Name, Tier: v.Tier, Class: v.Type, Window: Window{Days: days}.String()}

	if table == nil {
		r.caveat("server figures are unavailable: fetching tomato.gg is switched off (%s in config.yaml)", switchName)
	} else {
		r.metaPage(table.URL, "tank performance table", table.Updated)
		if p, ok := table.ByTank[v.TankID]; ok {
			out.Server = serverLine(p)
			r.caveat("%s", ServerPerfCaveat)
		} else {
			r.caveat("tomato.gg's %d-day table has no row for %s: too new, or too few battles in the window", days, v.Name)
		}
	}

	expected := r.expected()
	random, err := s.DB.LatestTankStatsFor(ctx, v.TankID, wg.ModeRandom)
	switch {
	case errors.Is(err, store.ErrNotFound):
		r.caveat("the player has no random battles recorded on this vehicle")
	case err != nil:
		return Envelope{}, err
	default:
		line := s.line([]store.TankStats{random}, expected)
		out.Lifetime = &line
		rec, err := r.recentFor(v.TankID, days, expected)
		if err != nil {
			return Envelope{}, err
		}
		out.Recent = &rec
		if out.Server != nil {
			out.Versus = append(out.Versus, versus("lifetime", line, out.Server, s))
			if rec.Available && rec.Stats != nil && rec.Stats.Battles > 0 {
				out.Versus = append(out.Versus, versus(rec.Window, *rec.Stats, out.Server, s))
			}
		}
	}
	return r.finish(out), nil
}

func versus(basis string, l StatLine, srv *ServerLine, s *Service) Versus {
	return Versus{
		Basis: basis, Battles: l.Battles, Confidence: s.Config.Confidence.Classify(l.Battles),
		WinRate: round(l.WinRate-srv.WinRate, 2), DPG: round(l.DPG-float64(srv.DPG), 0),
	}
}

// ServerPerfFilter selects the rows of `meta perf --tier/--class/--owned`.
type ServerPerfFilter struct {
	Tier  int
	Class string // canonical: lightTank, mediumTank, heavyTank, AT-SPG, SPG
	// Owned keeps only the player's garage.
	Owned bool
	// MinBattles drops rows too thin to rank.
	MinBattles int
}

// ServerPerfRow is one tank in a ranked list.
type ServerPerfRow struct {
	TankID    int    `json:"tank_id"`
	Name      string `json:"name"`
	Tier      int    `json:"tier"`
	Class     string `json:"class"`
	IsPremium bool   `json:"is_premium"`
	Owned     bool   `json:"owned"`

	Battles             int     `json:"battles"`
	WinRate             float64 `json:"win_rate"`
	PlayersWinRate      float64 `json:"players_win_rate"`
	WinRateDifferential float64 `json:"win_rate_differential"`
	WinRateMargin       float64 `json:"win_rate_margin"`
	DPG                 int     `json:"dpg"`
	WN8                 int     `json:"wn8"`

	// The player's lifetime random battles on it, when any.
	PlayerBattles int      `json:"player_battles,omitempty"`
	PlayerWinRate *float64 `json:"player_win_rate,omitempty"`
}

// ServerPerfList is the ranked list.
type ServerPerfList struct {
	Window string          `json:"window"`
	Filter string          `json:"filter"`
	Rows   []ServerPerfRow `json:"rows"`
	// Dropped counts rows under MinBattles.
	Dropped int `json:"dropped_thin_rows,omitempty"`
}

// ServerPerfRanked lists a tier's, a class's or the garage's tanks ranked by
// win-rate differential: how much each lifts its players.
func (s *Service) ServerPerfRanked(ctx context.Context, f ServerPerfFilter, table *meta.PerfTable) (Envelope, error) {
	if f.Tier == 0 && !f.Owned {
		return Envelope{}, errors.New("name a tank, or narrow the list with a tier or to the garage")
	}
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcVehicles, srcTankStats)
	r.metaPage(table.URL, "tank performance table", table.Updated)
	r.caveat("%s", ServerPerfCaveat)

	vehicles, err := s.DB.AllVehicles(ctx)
	if err != nil {
		return Envelope{}, err
	}
	owned, err := r.ownedSet()
	if err != nil {
		return Envelope{}, err
	}
	stats, err := s.DB.LatestTankStats(ctx, wg.ModeRandom)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Envelope{}, err
	}
	mine := make(map[int]store.TankStats, len(stats))
	for _, st := range stats {
		mine[st.TankID] = st
	}

	out := ServerPerfList{Window: Window{Days: table.Days}.String(), Filter: describeFilter(f), Rows: []ServerPerfRow{}}
	for id, p := range table.ByTank {
		v, known := vehicles[id]
		tier, class, name, premium := p.Tier, overlay.CanonicalClass(p.Class), p.Name, p.Premium
		if known {
			tier, class, name, premium = v.Tier, v.Type, v.Name, v.IsPremium
		}
		if (f.Tier != 0 && tier != f.Tier) || (f.Class != "" && class != f.Class) || (f.Owned && !owned[id]) {
			continue
		}
		if p.Battles < f.MinBattles {
			out.Dropped++
			continue
		}
		row := ServerPerfRow{
			TankID: id, Name: name, Tier: tier, Class: class, IsPremium: premium, Owned: owned[id],
			Battles: p.Battles, WinRate: p.WinRate, PlayersWinRate: p.PlayersWinRate,
			WinRateDifferential: p.Differential, WinRateMargin: winRateMargin(p.WinRate, p.Battles),
			DPG: p.Damage, WN8: p.WN8,
		}
		if st, ok := mine[id]; ok && st.Battles > 0 {
			row.PlayerBattles = st.Battles
			wr := round(100*float64(st.Wins)/float64(st.Battles), 2)
			row.PlayerWinRate = &wr
		}
		out.Rows = append(out.Rows, row)
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		if out.Rows[i].WinRateDifferential != out.Rows[j].WinRateDifferential {
			return out.Rows[i].WinRateDifferential > out.Rows[j].WinRateDifferential
		}
		return out.Rows[i].TankID < out.Rows[j].TankID
	})
	if out.Dropped > 0 {
		r.caveat("%d tank(s) with fewer than %d server battles in the window were left out as too thin to rank", out.Dropped, f.MinBattles)
	}
	if f.Owned {
		r.caveat("garage tanks missing from the list had no row in tomato.gg's table (too new, or too few battles)")
	}
	return r.finish(out), nil
}

// winRateMargin is the 95 % binomial sampling margin of a win rate over a
// number of battles, in percentage points. It is a lower bound: the battles
// of one player are not independent of each other.
func winRateMargin(winRate float64, battles int) float64 {
	if battles <= 0 {
		return 0
	}
	p := winRate / 100
	return round(196*math.Sqrt(p*(1-p)/float64(battles)), 2)
}

func describeFilter(f ServerPerfFilter) string {
	d := ""
	if f.Tier != 0 {
		d = fmt.Sprintf("tier %d", f.Tier)
	}
	if f.Class != "" {
		d += " " + f.Class
	}
	if f.Owned {
		d += " in the garage"
	}
	if d == "" {
		return "all"
	}
	return d
}

// metaPage records a question-time page as a source, aged by its own date,
// and says so when that date is old enough that the page may have stopped
// refreshing.
func (r *run) metaPage(url, what string, updated time.Time) {
	if r.used[url] {
		return
	}
	r.used[url] = true
	// Clamped: a page stamped ahead of the local clock is fresh, not from the future.
	age := max(0, r.now.Sub(updated))
	r.meta.Sources = append(r.meta.Sources, Source{Name: url, ObservedAt: updated, AgeSeconds: int64(age.Seconds())})
	if age > StaleAfter {
		r.caveat("tomato.gg's %s was last recomputed on %s, %d days ago: it may have stopped refreshing, so treat it as that old",
			what, updated.Format("2006-01-02"), int(age.Hours()/24))
	}
}
