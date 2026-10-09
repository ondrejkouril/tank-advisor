package meta

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PerfWindows are the windows tomato.gg's tank-performance page serves.
var PerfWindows = []int{7, 30, 60}

// TankPerf is one vehicle's random-battle figures across the whole server
// over a window, as tomato.gg computes them.
type TankPerf struct {
	TankID  int
	Name    string
	Tier    int
	Class   string // tomato.gg's: LT, MT, HT, TD, SPG
	Premium bool

	Battles int
	// WinRate is the tank's; PlayersWinRate is the overall win rate of the
	// players who played it, and Differential the first minus the second:
	// how far the tank lifts (or drags) its players from their usual.
	WinRate, PlayersWinRate, Differential float64
	// WN8 is tomato.gg's rating of the tank's battles; PlayersWN8 the overall
	// WN8 of its players.
	WN8, PlayersWN8 int

	Damage, SniperDamage           int
	Frags, Spots                   float64
	SpottingAssist, TrackingAssist int
	DamageBlocked, DamageReceived  int
	HitRate, PenRate               float64 // percent
	Survival                       float64 // percent
	LifeTime                       int     // seconds
	BaseXP                         int
}

// PerfTable is a fetched performance table with its provenance.
type PerfTable struct {
	URL       string
	FetchedAt time.Time
	Updated   time.Time
	Days      int
	ByTank    map[int]TankPerf
}

// Performance fetches the server-wide table for a window of days (7, 30 or 60).
func (t *Tomato) Performance(ctx context.Context, server string, days int) (PerfTable, error) {
	if !validWindow(days) {
		return PerfTable{}, fmt.Errorf("tomato.gg serves %v-day windows, not %d", PerfWindows, days)
	}
	p, err := t.page(ctx, fmt.Sprintf("/tank-performance/recent/%s/%d", strings.ToUpper(server), days), "tank_id")
	if err != nil {
		return PerfTable{}, err
	}
	table := PerfTable{URL: p.URL, FetchedAt: p.FetchedAt, Updated: p.Updated, Days: days, ByTank: map[int]TankPerf{}}
	for _, r := range p.Records {
		perf, ok := tankPerf(r)
		if ok {
			table.ByTank[perf.TankID] = perf
		}
	}
	if len(table.ByTank) == 0 {
		return PerfTable{}, fmt.Errorf("%s: no tank rows with battles and a win rate; %w", p.URL, ErrFormat)
	}
	return table, nil
}

func validWindow(days int) bool {
	for _, d := range PerfWindows {
		if d == days {
			return true
		}
	}
	return false
}

// tankPerf reads one row; a row without an id, battles or win rate is not a
// performance row (the page embeds other tank lists too).
func tankPerf(r Record) (TankPerf, bool) {
	id, okID := r.Int("tank_id")
	battles, okB := r.Int("battles")
	wr, okWR := r.Float("winrate")
	if !okID || !okB || !okWR {
		return TankPerf{}, false
	}
	p := TankPerf{TankID: id, Name: r.String("name"), Class: r.String("class"), Battles: battles, WinRate: wr}
	p.Tier, _ = r.Int("tier")
	p.Premium, _ = r.Bool("isPrem")
	p.PlayersWinRate, _ = r.Float("player_winrate")
	p.Differential, _ = r.Float("winrate_differential")
	p.WN8, _ = r.Int("wn8")
	p.PlayersWN8, _ = r.Int("player_wn8")
	p.Damage, _ = r.Int("damage")
	p.SniperDamage, _ = r.Int("sniper_damage")
	p.Frags, _ = r.Float("frags")
	p.Spots, _ = r.Float("spots")
	p.SpottingAssist, _ = r.Int("spotting_assist")
	p.TrackingAssist, _ = r.Int("tracking_assist")
	p.DamageBlocked, _ = r.Int("damage_blocked")
	p.DamageReceived, _ = r.Int("damage_received")
	p.HitRate, _ = r.Float("hit_rate")
	p.PenRate, _ = r.Float("pen_rate")
	p.Survival, _ = r.Float("survival")
	p.LifeTime, _ = r.Int("life_time")
	p.BaseXP, _ = r.Int("base_xp")
	return p, true
}
