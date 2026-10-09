package query

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/testseed"
)

func perfTable(updated time.Time) *meta.PerfTable {
	row := func(id int, name string, tier int, class string, battles int, wr, players, diff float64, dmg int) meta.TankPerf {
		return meta.TankPerf{TankID: id, Name: name, Tier: tier, Class: class, Battles: battles,
			WinRate: wr, PlayersWinRate: players, Differential: diff, Damage: dmg,
			SpottingAssist: 900, TrackingAssist: 100, WN8: 1700, PlayersWN8: 1600}
	}
	return &meta.PerfTable{
		URL: "https://tomato.gg/tank-performance/recent/EU/30", Updated: updated, Days: 30,
		ByTank: map[int]meta.TankPerf{
			testseed.Blesk:    row(testseed.Blesk, "Blesk", 8, "LT", 20000, 49.0, 51.0, -2.0, 1500),
			testseed.AMX1390:  row(testseed.AMX1390, "AMX 13 90", 9, "LT", 30000, 50.5, 50.0, 0.5, 1800),
			testseed.Tesak:    row(testseed.Tesak, "Tesák", 10, "LT", 40000, 51.0, 50.0, 1.0, 2300),
			testseed.Kranvagn: row(testseed.Kranvagn, "Kranvagn", 10, "HT", 50000, 50.0, 51.5, -1.5, 2600),
			testseed.Obj261:   row(testseed.Obj261, "Obj. 261", 10, "SPG", 400, 52.0, 50.0, 2.0, 1900),
		},
	}
}

func TestServerPerfBesideThePlayer(t *testing.T) {
	s := seed(t)
	table := perfTable(testseed.Now.Add(-12 * time.Hour))
	env, err := s.ServerPerf(context.Background(), "blesk", table, "")
	if err != nil {
		t.Fatal(err)
	}
	p := env.Data.(ServerPerf)
	if p.Server == nil || p.Server.WinRate != 49.0 || p.Server.WinRateDifferential != -2.0 || p.Server.Assist != 1000 {
		t.Errorf("server = %+v", p.Server)
	}
	if p.Lifetime == nil || p.Window != "30d" {
		t.Fatalf("lifetime = %+v, window %q", p.Lifetime, p.Window)
	}
	if len(p.Versus) == 0 || p.Versus[0].Basis != "lifetime" ||
		p.Versus[0].WinRate != round(p.Lifetime.WinRate-49.0, 2) || p.Versus[0].DPG != round(p.Lifetime.DPG-1500, 0) {
		t.Errorf("versus = %+v against lifetime %+v", p.Versus, p.Lifetime)
	}
	caveats := strings.Join(env.Meta.Caveats, "\n")
	if !strings.Contains(caveats, "win_rate_differential is the tank's win rate minus") {
		t.Errorf("caveats lack the reading of the server figures: %q", caveats)
	}
	if strings.Contains(caveats, "stopped refreshing") {
		t.Errorf("a 12-hour-old page is called stale: %q", caveats)
	}
	var page *Source
	for i := range env.Meta.Sources {
		if env.Meta.Sources[i].Name == table.URL {
			page = &env.Meta.Sources[i]
		}
	}
	if page == nil || page.AgeSeconds != 12*3600 {
		t.Errorf("sources = %+v, want the page aged from its own date", env.Meta.Sources)
	}
}

// TestServerPerfStalePage: a page not recomputed for weeks is still served,
// but says how old it is.
func TestServerPerfStalePage(t *testing.T) {
	env, err := seed(t).ServerPerf(context.Background(), "blesk", perfTable(testseed.Now.Add(-28*24*time.Hour)), "")
	if err != nil {
		t.Fatal(err)
	}
	if c := strings.Join(env.Meta.Caveats, "\n"); !strings.Contains(c, "28 days ago") {
		t.Errorf("caveats = %q, want the page's age stated", c)
	}
}

// TestServerPerfNoRow: a tank tomato.gg does not list still gets the
// player's figures, and says why the server's are missing.
func TestServerPerfNoRow(t *testing.T) {
	env, err := seed(t).ServerPerf(context.Background(), "leox", perfTable(testseed.Now), "")
	if err != nil {
		t.Fatal(err)
	}
	p := env.Data.(ServerPerf)
	if p.Server != nil || len(p.Versus) != 0 {
		t.Errorf("server = %+v, versus %+v; want none", p.Server, p.Versus)
	}
	if c := strings.Join(env.Meta.Caveats, "\n"); !strings.Contains(c, "no row for Leox") {
		t.Errorf("caveats = %q", c)
	}
}

func TestServerPerfSwitchedOff(t *testing.T) {
	env, err := seed(t).ServerPerf(context.Background(), "blesk", nil, "meta.tomato_fetch")
	if err != nil {
		t.Fatal(err)
	}
	if p := env.Data.(ServerPerf); p.Server != nil || p.Lifetime == nil {
		t.Errorf("perf = %+v, want the player's figures only", p)
	}
	if c := strings.Join(env.Meta.Caveats, "\n"); !strings.Contains(c, "meta.tomato_fetch") {
		t.Errorf("caveats = %q, want the switch named", c)
	}
	for _, src := range env.Meta.Sources {
		if strings.Contains(src.Name, "tomato.gg") {
			t.Errorf("sources name tomato.gg although nothing was fetched: %+v", src)
		}
	}
}

func TestServerPerfRanked(t *testing.T) {
	s := seed(t)
	ctx := context.Background()
	table := perfTable(testseed.Now)

	env, err := s.ServerPerfRanked(ctx, ServerPerfFilter{Tier: 10, MinBattles: 1000}, table)
	if err != nil {
		t.Fatal(err)
	}
	list := env.Data.(ServerPerfList)
	var names []string
	for _, r := range list.Rows {
		names = append(names, r.Name)
	}
	// Ranked by differential; the Obj. 261's 400 battles are too thin.
	if got := strings.Join(names, ", "); got != "Vz. 71 Tesák, Kranvagn" || list.Dropped != 1 {
		t.Errorf("tier X = %s (dropped %d)", got, list.Dropped)
	}

	env, err = s.ServerPerfRanked(ctx, ServerPerfFilter{Owned: true, Class: "lightTank", MinBattles: 1000}, table)
	if err != nil {
		t.Fatal(err)
	}
	list = env.Data.(ServerPerfList)
	if len(list.Rows) != 2 || list.Rows[0].TankID != testseed.AMX1390 || list.Rows[1].TankID != testseed.Blesk {
		t.Fatalf("owned lights = %+v", list.Rows)
	}
	if r := list.Rows[1]; !r.Owned || r.PlayerBattles != 54 || r.PlayerWinRate == nil {
		t.Errorf("Blesk row = %+v, want the player's own battles and win rate", r)
	}

	if _, err := s.ServerPerfRanked(ctx, ServerPerfFilter{}, table); err == nil {
		t.Error("an unfiltered list of every tank on the server was accepted")
	}
}

// TestWinRateMargin: 1,775 battles at 50 % carry about ±2.3 points of noise,
// enough to swamp a 1.4-point differential (seen live on 2026-10-09).
func TestWinRateMargin(t *testing.T) {
	for _, tc := range []struct {
		wr      float64
		battles int
		want    float64
	}{{50, 1775, 2.33}, {50, 307426, 0.18}, {50, 0, 0}} {
		if got := winRateMargin(tc.wr, tc.battles); got != tc.want {
			t.Errorf("winRateMargin(%v, %d) = %v, want %v", tc.wr, tc.battles, got, tc.want)
		}
	}
}
