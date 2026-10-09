package meta

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// ProfileSource is the part of the Wargaming client `meta profile` uses.
type ProfileSource interface {
	ModulesTree(ctx context.Context, tankID int) (map[int]wg.Module, error)
	VehicleProfiles(ctx context.Context, tankID int) ([]wg.ProfileRef, error)
	VehicleProfile(ctx context.Context, tankID int, profileID string) (wg.Profile, error)
}

// Configs is a vehicle's stock and top configurations, and the research
// between them.
type Configs struct {
	Stock wg.Profile
	// Top is nil when the stock configuration is the only one: since update
	// 2.0 most tier X and every tier XI vehicle comes fully equipped
	// (measured 2026-10-09: 5 of 70 tier X tech-tree vehicles still have
	// modules to research, every one from tier I to IX does).
	Top *wg.Profile
	// Research is every module between the two, in research order by type.
	Research []wg.Module
	// XPToTop is their total research cost.
	XPToTop int
}

// Configurations fetches a vehicle's stock and top configurations: three
// requests, four when there is research between them.
//
// The top configuration is the valid combination whose modules cost the most
// XP to research, which picks the end of every module line; the research to it
// is each of its modules not fitted as stock, with the modules that unlock
// them.
func Configurations(ctx context.Context, src ProfileSource, tankID int) (Configs, error) {
	tree, err := src.ModulesTree(ctx, tankID)
	if err != nil {
		return Configs{}, err
	}
	refs, err := src.VehicleProfiles(ctx, tankID)
	if err != nil {
		return Configs{}, err
	}
	stock, err := src.VehicleProfile(ctx, tankID, "")
	if err != nil {
		return Configs{}, err
	}
	out := Configs{Stock: stock}

	best, bestXP := "", -1
	for _, ref := range refs {
		xp := 0
		for _, id := range profileModules(ref.ProfileID) {
			xp += tree[id].PriceXP
		}
		if xp > bestXP || (xp == bestXP && ref.ProfileID < best) {
			best, bestXP = ref.ProfileID, xp
		}
	}
	if best == "" || best == stock.ProfileID || bestXP == 0 {
		return out, nil
	}
	top, err := src.VehicleProfile(ctx, tankID, best)
	if err != nil {
		return Configs{}, err
	}
	out.Top = &top

	// parents maps each module to the modules whose research unlocks it.
	parents := map[int][]int{}
	for id, m := range tree {
		for _, next := range m.NextModules {
			parents[next] = append(parents[next], id)
		}
	}
	needed := map[int]bool{}
	var walk func(id int)
	walk = func(id int) {
		m, ok := tree[id]
		if !ok || m.IsDefault || needed[id] {
			return
		}
		needed[id] = true
		for _, p := range parents[id] {
			walk(p)
		}
	}
	for _, id := range profileModules(best) {
		walk(id)
	}
	for id := range needed {
		out.Research = append(out.Research, tree[id])
		out.XPToTop += tree[id].PriceXP
	}
	order := map[string]int{"vehicleChassis": 0, "vehicleTurret": 1, "vehicleGun": 2, "vehicleEngine": 3, "vehicleRadio": 4}
	sort.Slice(out.Research, func(i, j int) bool {
		a, b := out.Research[i], out.Research[j]
		if order[a.Type] != order[b.Type] {
			return order[a.Type] < order[b.Type]
		}
		return a.PriceXP < b.PriceXP
	})
	return out, nil
}

// profileModules reads the module ids out of a profile id, which Wargaming
// writes as the ids joined by dashes ("567-2610-2611-513333-514100").
func profileModules(profileID string) []int {
	var ids []int
	for _, part := range strings.Split(profileID, "-") {
		if id, err := strconv.Atoi(part); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// DPM is damage per minute with the first (standard) shell, as Wargaming's
// rate of fire gives it.
func DPM(p wg.Profile) int {
	if len(p.Ammo) == 0 || len(p.Ammo[0].Damage) < 2 {
		return 0
	}
	return int(p.Gun.FireRate*float64(p.Ammo[0].Damage[1]) + 0.5)
}

// PowerToWeight is horsepower per tonne.
func PowerToWeight(p wg.Profile) float64 {
	if p.Weight <= 0 {
		return 0
	}
	return float64(int(float64(p.Engine.Power)/(float64(p.Weight)/1000)*10+0.5)) / 10
}
