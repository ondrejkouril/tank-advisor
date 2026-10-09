package query

import (
	"context"
	"strings"
	"testing"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/testseed"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

func TestProfileStockAndTop(t *testing.T) {
	stock := wg.Profile{ProfileID: "1-2", HP: 1700, Weight: 40000}
	stock.Gun.Name, stock.Gun.FireRate = "stock gun", 6
	stock.Engine.Power = 600
	stock.Ammo = []wg.Shell{{Type: "ARMOR_PIERCING", Damage: []int{180, 240, 300}, Penetration: []int{150, 200, 250}}}
	top := stock
	top.ProfileID, top.Gun.Name, top.Engine.Power = "3-4", "top gun", 800

	var asked int
	env, err := seed(t).Profile(context.Background(), "selma", func(tankID int) (meta.Configs, error) {
		asked = tankID
		return meta.Configs{Stock: stock, Top: &top, XPToTop: 70000,
			Research: []wg.Module{{Type: "vehicleGun", Name: "top gun", PriceXP: 70000, PriceCredit: 300000}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked != testseed.Selma {
		t.Errorf("fetched tank %d, want the Šelma", asked)
	}
	p := env.Data.(VehicleProfile)
	if p.Stock.Gun != "stock gun" || p.Stock.DPM != 1440 || p.Stock.PowerToWeight != 15 || p.Top == nil || p.Top.EnginePower != 800 ||
		p.XPToTop != 70000 || len(p.Research) != 1 || len(p.Stock.Shells) != 1 || p.Stock.Shells[0].Penetration != 200 {
		t.Errorf("profile = %+v / stock %+v / top %+v", p, p.Stock, p.Top)
	}
	if c := strings.Join(env.Meta.Caveats, "\n"); !strings.Contains(c, "nominal figures") || !strings.Contains(c, "already researched is not known") {
		t.Errorf("caveats = %q", c)
	}
}

func TestProfileNothingToResearch(t *testing.T) {
	env, err := seed(t).Profile(context.Background(), "tesak", func(int) (meta.Configs, error) {
		return meta.Configs{Stock: wg.Profile{ProfileID: "1-2"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if p := env.Data.(VehicleProfile); p.Top != nil || p.XPToTop != 0 {
		t.Errorf("profile = %+v", p)
	}
	if c := strings.Join(env.Meta.Caveats, "\n"); !strings.Contains(c, "nothing to research") {
		t.Errorf("caveats = %q", c)
	}
}
