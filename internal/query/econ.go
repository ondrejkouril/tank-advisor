package query

import (
	"context"
	"errors"
	"sort"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/overlay"
)

// EconCaveat travels with every credit figure.
const EconCaveat = "credit figures are tomato.gg's averages per battle, not split by server, from at least 100 battles per tank; " +
	"credits per battle are in no API, so they most likely come from battles its mod records, a much smaller sample than its server tables. " +
	"profit is earnings minus ammunition, repairs and consumables. They describe the average player of the tank, " +
	"not this player: skill, loadout (premium ammunition, consumables) and boosters move both sides"

// EconAccountCaveat says which accounts a table covers; the account type
// roughly doubles earnings, so it matters more than any other difference.
var EconAccountCaveat = map[string]string{
	"all":      "account filter all: premium and standard accounts mixed, and premium accounts play most of the battles, so for a standard account the figures run high (use the standard filter)",
	"premium":  "account filter premium: battles on a premium account only",
	"standard": "account filter standard: battles on a standard account only; a smaller sample, with fewer tanks",
}

// EconLine is one tank's credit figures.
type EconLine struct {
	Battles           int `json:"battles"`
	Earnings          int `json:"earnings"`
	Profit            int `json:"profit"`
	AmmoCost          int `json:"ammo_cost"`
	CostPerShot       int `json:"cost_per_shot"`
	RepairCost        int `json:"repair_cost"`
	ConsumablesCost   int `json:"consumables_cost"`
	EarningsPerMinute int `json:"earnings_per_minute"`
	ProfitPerMinute   int `json:"profit_per_minute"`
}

func econLine(e meta.TankEcon) *EconLine {
	return &EconLine{
		Battles: e.Battles, Earnings: e.Earnings, Profit: e.Profit, AmmoCost: e.AmmoCost,
		CostPerShot: e.CostPerShot, RepairCost: e.RepairCost, ConsumablesCost: e.ConsumablesCost,
		EarningsPerMinute: e.EarningsPerMinute, ProfitPerMinute: e.ProfitPerMinute,
	}
}

// Econ is `meta econ <tank>`.
type Econ struct {
	TankID    int       `json:"tank_id"`
	Name      string    `json:"name"`
	Tier      int       `json:"tier"`
	Class     string    `json:"class"`
	IsPremium bool      `json:"is_premium"`
	Owned     bool      `json:"owned"`
	Window    string    `json:"window"`
	Account   string    `json:"account"`
	Credits   *EconLine `json:"credits"`
}

// Econ reports one tank's credit figures from a freshly fetched table.
func (s *Service) Econ(ctx context.Context, ref string, table *meta.EconTable) (Envelope, error) {
	v, err := s.resolveTank(ctx, ref)
	if err != nil {
		return Envelope{}, err
	}
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcVehicles)
	r.metaPage(table.URL, "economics table", table.Updated)
	owned, err := r.ownedSet()
	if err != nil {
		return Envelope{}, err
	}
	out := Econ{TankID: v.TankID, Name: v.Name, Tier: v.Tier, Class: v.Type, IsPremium: v.IsPremium, Owned: owned[v.TankID],
		Window: Window{Days: table.Days}.String(), Account: table.Account}
	if e, ok := table.ByTank[v.TankID]; ok {
		out.Credits = econLine(e)
		r.caveat("%s", EconCaveat)
		r.caveat("%s", EconAccountCaveat[table.Account])
	} else {
		r.caveat("tomato.gg's %d-day economics table (%s accounts) has no row for %s: under its 100-battle minimum, or too new",
			table.Days, table.Account, v.Name)
	}
	return r.finish(out), nil
}

// EconFilter selects the rows of `meta econ --tier/--class/--owned/--premium`.
type EconFilter struct {
	Tier  int
	Class string
	Owned bool
	// PremiumTanks keeps only premium vehicles, the usual credit earners.
	PremiumTanks bool
	// PerMinute ranks by profit per minute instead of per battle.
	PerMinute bool
}

// EconRow is one tank in a ranked list.
type EconRow struct {
	TankID    int    `json:"tank_id"`
	Name      string `json:"name"`
	Tier      int    `json:"tier"`
	Class     string `json:"class"`
	IsPremium bool   `json:"is_premium"`
	Owned     bool   `json:"owned"`
	EconLine
}

// EconList is the ranked list.
type EconList struct {
	Window  string    `json:"window"`
	Account string    `json:"account"`
	Filter  string    `json:"filter"`
	RankBy  string    `json:"rank_by"`
	Rows    []EconRow `json:"rows"`
}

// EconRanked lists a tier's, a class's or the garage's tanks by profit.
func (s *Service) EconRanked(ctx context.Context, f EconFilter, table *meta.EconTable) (Envelope, error) {
	if f.Tier == 0 && !f.Owned {
		return Envelope{}, errors.New("name a tank, or narrow the list with a tier or to the garage")
	}
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcVehicles)
	r.metaPage(table.URL, "economics table", table.Updated)
	r.caveat("%s", EconCaveat)
	r.caveat("%s", EconAccountCaveat[table.Account])

	vehicles, err := s.DB.AllVehicles(ctx)
	if err != nil {
		return Envelope{}, err
	}
	owned, err := r.ownedSet()
	if err != nil {
		return Envelope{}, err
	}

	rankBy := "profit"
	if f.PerMinute {
		rankBy = "profit_per_minute"
	}
	filter := describeFilter(ServerPerfFilter{Tier: f.Tier, Class: f.Class, Owned: f.Owned})
	if f.PremiumTanks {
		filter += ", premium tanks"
	}
	out := EconList{Window: Window{Days: table.Days}.String(), Account: table.Account, Filter: filter, RankBy: rankBy, Rows: []EconRow{}}
	missing := 0
	for id, e := range table.ByTank {
		v, known := vehicles[id]
		tier, class, name, premium := e.Tier, overlay.CanonicalClass(e.Class), e.Name, e.Premium
		if known {
			tier, class, name, premium = v.Tier, v.Type, v.Name, v.IsPremium
		}
		if (f.Tier != 0 && tier != f.Tier) || (f.Class != "" && class != f.Class) ||
			(f.Owned && !owned[id]) || (f.PremiumTanks && !premium) {
			continue
		}
		out.Rows = append(out.Rows, EconRow{TankID: id, Name: name, Tier: tier, Class: class, IsPremium: premium,
			Owned: owned[id], EconLine: *econLine(e)})
	}
	if f.Owned {
		for id := range owned {
			if _, ok := table.ByTank[id]; !ok {
				missing++
			}
		}
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i].Profit, out.Rows[j].Profit
		if f.PerMinute {
			a, b = out.Rows[i].ProfitPerMinute, out.Rows[j].ProfitPerMinute
		}
		if a != b {
			return a > b
		}
		return out.Rows[i].TankID < out.Rows[j].TankID
	})
	if missing > 0 {
		r.caveat("%d garage tank(s) have no row in the table (under tomato.gg's 100-battle minimum, or too new)", missing)
	}
	return r.finish(out), nil
}
