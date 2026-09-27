package query

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// GarageFilter narrows `query garage`.
type GarageFilter struct {
	Tier  int    // 0 for any
	Class string // canonical API class, "" for any
}

// Garage is `query garage`.
type Garage struct {
	// Count is the number of vehicles matching the filter; the whole garage
	// when unfiltered. Only identified vehicles count (spec section 3.1).
	Count   int            `json:"count"`
	ByTier  map[string]int `json:"by_tier"`
	ByClass map[string]int `json:"by_class"`
	Tanks   []GarageTank   `json:"tanks"`
}

// GarageTank is one owned vehicle with its lifetime random-battle line.
type GarageTank struct {
	TankID    int    `json:"tank_id"`
	Name      string `json:"name"`
	Tier      int    `json:"tier"`
	Class     string `json:"class"`
	Nation    string `json:"nation"`
	IsPremium bool   `json:"is_premium"`
	// Mastery is 0 none, 1 third class, 2 second, 3 first, 4 Ace Tanker.
	Mastery int      `json:"mastery"`
	Stats   StatLine `json:"stats"`
}

// Garage lists owned vehicles, optionally filtered by tier and class.
func (s *Service) Garage(ctx context.Context, f GarageFilter) (Envelope, error) {
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcAccount, srcVehicles, srcTankStats, srcMastery)

	garage, err := s.DB.Garage(ctx)
	if errors.Is(err, store.ErrNotFound) {
		r.caveat("no garage synced")
		return r.finish(nil), nil
	}
	if err != nil {
		return Envelope{}, err
	}
	if n := len(garage.Unresolved); n > 0 {
		r.caveat("the API reported %d further garage id(s) the encyclopedia does not describe; "+
			"they do not appear in the client and are not counted", n)
	}

	vehicles, err := s.DB.AllVehicles(ctx)
	if err != nil {
		return Envelope{}, err
	}
	stats, err := r.latestRandom()
	if err != nil {
		return Envelope{}, err
	}
	expected := r.expected()
	r.caveat("%s", AssistCaveat)

	// The API counts event rentals as garage vehicles; the game client says
	// which ones they are.
	keep := make(map[int]bool, len(garage.Identified))
	names := make(map[int]string, len(garage.Identified))
	for _, id := range garage.Identified {
		keep[id], names[id] = true, vehicles[id].Name
	}
	r.dropRentals(keep, names)

	out := Garage{ByTier: map[string]int{}, ByClass: map[string]int{}, Tanks: []GarageTank{}}
	for _, id := range garage.Identified {
		if !keep[id] {
			continue
		}
		v := vehicles[id]
		if f.Tier != 0 && v.Tier != f.Tier {
			continue
		}
		if f.Class != "" && v.Type != f.Class {
			continue
		}
		t := GarageTank{
			TankID: id, Name: v.Name, Tier: v.Tier, Class: v.Type, Nation: v.Nation,
			IsPremium: v.IsPremium,
		}
		if row, ok := stats[id]; ok {
			t.Mastery = row.MarkOfMastery
			t.Stats = s.line([]store.TankStats{row}, expected)
		} else {
			t.Stats = s.line(nil, nil)
		}
		out.Tanks = append(out.Tanks, t)
		out.ByTier[fmt.Sprint(v.Tier)]++
		out.ByClass[v.Type]++
	}
	out.Count = len(out.Tanks)

	sort.Slice(out.Tanks, func(i, j int) bool {
		a, b := out.Tanks[i], out.Tanks[j]
		if a.Tier != b.Tier {
			return a.Tier > b.Tier
		}
		return a.Name < b.Name
	})
	return r.finish(out), nil
}

// latestRandom returns the newest random-battle row per tank.
func (r *run) latestRandom() (map[int]store.TankStats, error) {
	rows, err := r.s.DB.LatestTankStats(r.ctx, wg.ModeRandom)
	if errors.Is(err, store.ErrNotFound) {
		r.caveat("no tank statistics synced")
		return map[int]store.TankStats{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(map[int]store.TankStats, len(rows))
	for _, row := range rows {
		out[row.TankID] = row
	}
	return out, nil
}
