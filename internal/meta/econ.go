package meta

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// EconWindows are the windows tomato.gg's economics page serves.
var EconWindows = []int{14, 30, 60}

// EconAccounts are its account filters: every battle, or only those played on
// a premium or a standard account. An unknown filter is not an error there -
// the page silently serves "all" - so it is checked here.
var EconAccounts = []string{"all", "premium", "standard"}

// TankEcon is one vehicle's average credit figures per battle.
//
// The page is not split by server and says only "Min. 100 battles". Credits
// per battle are in no API, so the battles are most likely those tomato.gg's
// mod records (measured 2026-10-09: far fewer than its server-wide tables).
type TankEcon struct {
	TankID  int
	Name    string
	Tier    int
	Class   string // tomato.gg's: LT, MT, HT, TD, SPG
	Premium bool

	Battles int
	// Earnings is gross credits per battle; Profit is after ammunition,
	// repairs and consumables.
	Earnings, Profit                   int
	AmmoCost, CostPerShot              int
	RepairCost, ConsumablesCost        int
	EarningsPerMinute, ProfitPerMinute int
}

// EconTable is a fetched economics table with its provenance.
type EconTable struct {
	URL       string
	FetchedAt time.Time
	Updated   time.Time
	Days      int
	Account   string
	ByTank    map[int]TankEcon
}

// Economics fetches the table for an account filter and a window.
func (t *Tomato) Economics(ctx context.Context, account string, days int) (EconTable, error) {
	if !slices.Contains(EconAccounts, account) {
		return EconTable{}, fmt.Errorf("tomato.gg's economics filters are %v, not %q", EconAccounts, account)
	}
	if !slices.Contains(EconWindows, days) {
		return EconTable{}, fmt.Errorf("tomato.gg's economics windows are %v days, not %d", EconWindows, days)
	}
	p, err := t.page(ctx, fmt.Sprintf("/economics/%s/%d", account, days), "tank_id")
	if err != nil {
		return EconTable{}, err
	}
	table := EconTable{URL: p.URL, FetchedAt: p.FetchedAt, Updated: p.Updated, Days: days, Account: account, ByTank: map[int]TankEcon{}}
	for _, r := range p.Records {
		if e, ok := tankEcon(r); ok {
			table.ByTank[e.TankID] = e
		}
	}
	if len(table.ByTank) == 0 {
		return EconTable{}, fmt.Errorf("%s: no tank rows with battles and earnings; %w", p.URL, ErrFormat)
	}
	return table, nil
}

func tankEcon(r Record) (TankEcon, bool) {
	id, okID := r.Int("tank_id")
	battles, okB := r.Int("battles")
	earnings, okE := r.Int("avg_earnings")
	if !okID || !okB || !okE {
		return TankEcon{}, false
	}
	e := TankEcon{TankID: id, Name: r.String("name"), Class: r.String("class"), Battles: battles, Earnings: earnings}
	e.Tier, _ = r.Int("tier")
	e.Premium, _ = r.Bool("isPrem")
	e.Profit, _ = r.Int("avg_profit")
	e.AmmoCost, _ = r.Int("avg_ammo_cost")
	e.CostPerShot, _ = r.Int("cost_per_shot")
	e.RepairCost, _ = r.Int("avg_repair_cost")
	e.ConsumablesCost, _ = r.Int("avg_consumables_cost")
	e.EarningsPerMinute, _ = r.Int("earnings_per_minute")
	e.ProfitPerMinute, _ = r.Int("profit_per_minute")
	return e, true
}
