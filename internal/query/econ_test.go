package query

import (
	"context"
	"strings"
	"testing"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/testseed"
)

func econTable(account string) *meta.EconTable {
	row := func(id int, name string, tier int, class string, prem bool, profit, perMin int) meta.TankEcon {
		return meta.TankEcon{TankID: id, Name: name, Tier: tier, Class: class, Premium: prem, Battles: 5000,
			Earnings: profit + 20000, Profit: profit, ProfitPerMinute: perMin}
	}
	return &meta.EconTable{
		URL: "https://tomato.gg/economics/" + account + "/30", Updated: testseed.Now, Days: 30, Account: account,
		ByTank: map[int]meta.TankEcon{
			testseed.Leox:     row(testseed.Leox, "Leox", 9, "MT", true, 61000, 11000),
			testseed.AMX1390:  row(testseed.AMX1390, "AMX 13 90", 9, "LT", false, 12000, 2500),
			testseed.Blesk:    row(testseed.Blesk, "Blesk", 8, "LT", false, 25000, 2400),
			testseed.Tesak:    row(testseed.Tesak, "Tesák", 10, "LT", false, -9000, -1500),
			testseed.Kranvagn: row(testseed.Kranvagn, "Kranvagn", 10, "HT", false, -12000, -2000),
			// Not in the seed's vehicles: tomato.gg's own tier, class and flag are used.
			55937: row(55937, "Ambassador", 9, "MT", true, 58000, 10500),
		},
	}
}

func TestEconOneTank(t *testing.T) {
	env, err := seed(t).Econ(context.Background(), "leox", econTable("standard"))
	if err != nil {
		t.Fatal(err)
	}
	e := env.Data.(Econ)
	if e.Credits == nil || e.Credits.Profit != 61000 || e.Account != "standard" || e.Window != "30d" || !e.Owned {
		t.Errorf("econ = %+v / %+v", e, e.Credits)
	}
	c := strings.Join(env.Meta.Caveats, "\n")
	for _, want := range []string{"not split by server", "standard account only"} {
		if !strings.Contains(c, want) {
			t.Errorf("caveats lack %q: %q", want, c)
		}
	}

	env, err = seed(t).Econ(context.Background(), "obj 261", econTable("all"))
	if err != nil {
		t.Fatal(err)
	}
	if e := env.Data.(Econ); e.Credits != nil || !strings.Contains(strings.Join(env.Meta.Caveats, "\n"), "no row for Object 261") {
		t.Errorf("a tank missing from the table = %+v, %q", e, env.Meta.Caveats)
	}
}

func TestEconRanked(t *testing.T) {
	s := seed(t)
	ctx := context.Background()

	env, err := s.EconRanked(ctx, EconFilter{Owned: true}, econTable("all"))
	if err != nil {
		t.Fatal(err)
	}
	l := env.Data.(EconList)
	var names []string
	for _, r := range l.Rows {
		names = append(names, r.Name)
	}
	// The garage by profit per battle; the owned Obj. 261 has no row.
	if got := strings.Join(names, ", "); got != "Leox, Vz. 64 Blesk, AMX 13 90" {
		t.Errorf("garage by profit = %s", got)
	}
	if !strings.Contains(strings.Join(env.Meta.Caveats, "\n"), "1 garage tank(s) have no row") {
		t.Errorf("caveats = %q", env.Meta.Caveats)
	}

	env, err = s.EconRanked(ctx, EconFilter{Owned: true, PerMinute: true, Class: "lightTank"}, econTable("all"))
	if err != nil {
		t.Fatal(err)
	}
	if l := env.Data.(EconList); len(l.Rows) != 2 || l.Rows[0].TankID != testseed.AMX1390 || l.RankBy != "profit_per_minute" {
		t.Errorf("lights per minute = %+v", l)
	}

	env, err = s.EconRanked(ctx, EconFilter{Tier: 9, PremiumTanks: true}, econTable("all"))
	if err != nil {
		t.Fatal(err)
	}
	if l := env.Data.(EconList); len(l.Rows) != 1 || l.Rows[0].TankID != 55937 || l.Rows[0].Class != "mediumTank" {
		t.Errorf("tier IX premiums = %+v", l.Rows)
	}
}
