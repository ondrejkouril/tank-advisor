package query

import (
	"context"
	"strings"
	"testing"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/testseed"
)

func onslaughtTable() *meta.OnslaughtTable {
	tank := func(id int, name, class string, battles int, wr float64) meta.OnslaughtTank {
		return meta.OnslaughtTank{TankID: id, Name: name, Tier: 10, Class: class, Battles: battles, WinRate: wr, Damage: 2500}
	}
	return &meta.OnslaughtTable{
		URL: "https://tomato.gg/onslaught/EU", Updated: testseed.Now, Season: "azure_phoenix",
		ByTank: map[int]meta.OnslaughtTank{
			testseed.Tesak:     tank(testseed.Tesak, "Tesák", "LT", 200000, 50.5),
			testseed.AMX13105:  tank(testseed.AMX13105, "AMX 13 105", "LT", 300000, 49.0),
			testseed.Kranvagn:  tank(testseed.Kranvagn, "Kranvagn", "HT", 400000, 51.5),
			testseed.ConquerGC: tank(testseed.ConquerGC, "Conqueror GC", "SPG", 500, 55.0),
		},
		// 100 players: 40 at 0, 30 at 1000, 20 at 2000, 9 at 3000, 1 at 5000.
		Ratings: []meta.RatingBucket{
			{Rating: 0, Players: 40}, {Rating: 1000, Players: 30}, {Rating: 2000, Players: 20},
			{Rating: 3000, Players: 9}, {Rating: 5000, Players: 1},
		},
	}
}

func TestOnslaughtRanked(t *testing.T) {
	env, err := seed(t).Onslaught(context.Background(), "", OnslaughtFilter{MinBattles: 1000}, onslaughtTable())
	if err != nil {
		t.Fatal(err)
	}
	o := env.Data.(Onslaught)
	var names []string
	for _, r := range o.Rows {
		names = append(names, r.Name)
	}
	if got := strings.Join(names, ", "); got != "Kranvagn, Vz. 71 Tesák, AMX 13 105" || o.Dropped != 1 || o.Season != "azure_phoenix" {
		t.Errorf("ranked = %s, dropped %d, season %q", got, o.Dropped, o.Season)
	}
	want := map[string]int{"50": 1000, "25": 2000, "10": 3000, "1": 5000}
	if o.Ratings == nil || o.Ratings.Players != 100 {
		t.Fatalf("ratings = %+v", o.Ratings)
	}
	for k, v := range want {
		if o.Ratings.TopPercent[k] != v {
			t.Errorf("top %s%% from %d, want %d", k, o.Ratings.TopPercent[k], v)
		}
	}
	if c := strings.Join(env.Meta.Caveats, "\n"); !strings.Contains(c, "no Onslaught statistics") {
		t.Errorf("caveats = %q", c)
	}

	env, err = seed(t).Onslaught(context.Background(), "", OnslaughtFilter{Class: "lightTank", MinBattles: 1000}, onslaughtTable())
	if err != nil {
		t.Fatal(err)
	}
	if rows := env.Data.(Onslaught).Rows; len(rows) != 2 || rows[0].TankID != testseed.Tesak {
		t.Errorf("lights = %+v", rows)
	}
}

func TestOnslaughtOneTank(t *testing.T) {
	env, err := seed(t).Onslaught(context.Background(), "tesak", OnslaughtFilter{}, onslaughtTable())
	if err != nil {
		t.Fatal(err)
	}
	if o := env.Data.(Onslaught); o.Tank == nil || o.Tank.Battles != 200000 || o.Tank.WinRateMargin == 0 || o.Rows != nil {
		t.Errorf("tank = %+v", o)
	}
	env, err = seed(t).Onslaught(context.Background(), "blesk", OnslaughtFilter{}, onslaughtTable())
	if err != nil {
		t.Fatal(err)
	}
	if o := env.Data.(Onslaught); o.Tank != nil || !strings.Contains(strings.Join(env.Meta.Caveats, "\n"), "no row in this season") {
		t.Errorf("a tank not in Onslaught = %+v, %q", o.Tank, env.Meta.Caveats)
	}
}
