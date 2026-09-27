package query

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// recentWindows are the windows `query tank` reports alongside lifetime.
var recentWindows = []int{30, 60}

// Tank is `query tank`.
type Tank struct {
	TankID      int    `json:"tank_id"`
	Name        string `json:"name"`
	ShortName   string `json:"short_name"`
	Tier        int    `json:"tier"`
	Class       string `json:"class"`
	Nation      string `json:"nation"`
	IsPremium   bool   `json:"is_premium"`
	PriceCredit int    `json:"price_credit"`
	PriceGold   int    `json:"price_gold"`

	Owned   bool `json:"owned"`
	Mastery int  `json:"mastery"`

	// Lifetime is random battles since account creation; nil if never played.
	Lifetime *StatLine `json:"lifetime"`
	// AllBattles holds the figures that exist only across every battle type,
	// and which the in-game service record shows.
	AllBattles *AllBattles `json:"all_battles,omitempty"`
	// Recent is one entry per window, each either measured or explaining why
	// it cannot be yet.
	Recent []Recent `json:"recent"`

	ResearchedFrom []Link `json:"researched_from"`
	Unlocks        []Link `json:"unlocks"`

	Overlay TankOverlay `json:"overlay"`

	// Client is what the game client knows and no API does - the vehicle's
	// XP, marks, loadout and crew - from the client mod's dump. Absent
	// without a dump.
	Client *TankClient `json:"client,omitempty"`
}

// AllBattles is the all-battle-types block.
type AllBattles struct {
	Battles    int     `json:"battles"`
	Assist     float64 `json:"assist"`
	AvgBlocked float64 `json:"avg_blocked"`
}

// Recent is a windowed line, or the reason there is none.
type Recent struct {
	Window    string    `json:"window"`
	Available bool      `json:"available"`
	Span      *Span     `json:"span,omitempty"`
	Stats     *StatLine `json:"stats,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}

// Link is one tech-tree neighbour.
type Link struct {
	TankID int    `json:"tank_id"`
	Name   string `json:"name"`
	Tier   int    `json:"tier"`
	XPCost int    `json:"xp_cost"`
	Owned  bool   `json:"owned"`
}

// TankOverlay is what the overlay says about the vehicle.
type TankOverlay struct {
	ResearchedNotBought bool `json:"researched_not_bought"`
	// Goals lists every overlay goal the vehicle is the target or the step of.
	Goals []Goal `json:"goals,omitempty"`
}

// Goal is an overlay XP goal with its arithmetic done.
type Goal struct {
	TargetID   int    `json:"target_id"`
	Target     string `json:"target"`
	ViaID      int    `json:"via_id,omitempty"`
	Via        string `json:"via,omitempty"`
	XPRequired *int   `json:"xp_required"`
	// APIXPCost is the tech tree's figure for the same step.
	APIXPCost *int `json:"api_xp_cost"`
	// XPBanked is the XP on the via vehicle: the game client's figure when
	// the mod's dump has it, else the overlay's hand-kept one; nil means
	// unknown, because no API exposes per-vehicle XP.
	XPBanked *int `json:"xp_banked"`
	// XPBankedSource is "client mod" when the figure came from the game; it
	// is absent for the overlay's.
	XPBankedSource string `json:"xp_banked_source,omitempty"`
	// XPRemaining is set only when both required and banked are known.
	XPRemaining *int `json:"xp_remaining"`
}

// Tank returns everything known about one vehicle, by name or tank_id.
func (s *Service) Tank(ctx context.Context, ref string) (Envelope, error) {
	v, err := s.resolveTank(ctx, ref)
	if err != nil {
		return Envelope{}, err
	}
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcVehicles, srcAccount, srcTankStats, srcMastery)

	owned, err := r.ownedSet()
	if err != nil {
		return Envelope{}, err
	}
	out := Tank{
		TankID: v.TankID, Name: v.Name, ShortName: v.ShortName, Tier: v.Tier, Class: v.Type,
		Nation: v.Nation, IsPremium: v.IsPremium, PriceCredit: v.PriceCredit, PriceGold: v.PriceGold,
		Owned:          owned[v.TankID],
		Recent:         []Recent{},
		ResearchedFrom: []Link{},
		Unlocks:        []Link{},
	}

	expected := r.expected()
	random, err := s.DB.LatestTankStatsFor(ctx, v.TankID, wg.ModeRandom)
	switch {
	case errors.Is(err, store.ErrNotFound):
		r.caveat("no random battles recorded on this vehicle")
	case err != nil:
		return Envelope{}, err
	default:
		line := s.line([]store.TankStats{random}, expected)
		out.Lifetime = &line
		out.Mastery = random.MarkOfMastery
		r.caveat("%s", AssistCaveat)
	}

	if all, err := s.DB.LatestTankStatsFor(ctx, v.TankID, wg.ModeAll); err == nil {
		out.AllBattles = &AllBattles{
			Battles:    all.Battles,
			Assist:     round(all.AvgDamageAssisted, 1),
			AvgBlocked: round(all.AvgDamageBlocked, 1),
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return Envelope{}, err
	}

	if out.Lifetime != nil {
		for _, days := range recentWindows {
			rec, err := r.recentFor(v.TankID, days, expected)
			if err != nil {
				return Envelope{}, err
			}
			out.Recent = append(out.Recent, rec)
		}
	}

	if err := r.links(&out, owned); err != nil {
		return Envelope{}, err
	}
	if out.Client, err = r.tankClient(v.TankID); err != nil {
		return Envelope{}, err
	}
	r.tankOverlay(&out)
	return r.finish(out), nil
}

func (r *run) recentFor(tankID, days int, expected map[int]store.WN8Expected) (Recent, error) {
	w := Window{Days: days}
	rec := Recent{Window: w.String()}

	deltas, span, history, err := r.windowDeltas(w)
	switch {
	case errors.Is(err, store.ErrNoBaseline):
		rec.Reason = notYet(history, days)
		return rec, nil
	case err != nil:
		return rec, err
	}

	rec.Available = true
	rec.Span = span
	var rows []store.TankStats
	for _, d := range deltas {
		if d.TankID == tankID {
			rows = append(rows, d.Delta)
		}
	}
	line := r.s.line(rows, expected)
	rec.Stats = &line
	return rec, nil
}

// notYet is the honest answer to a window with no baseline.
func notYet(h *History, days int) string {
	if h == nil {
		return "no snapshot history yet"
	}
	return fmt.Sprintf("not yet: %.1f days of history (since %s, %d snapshots) against the %d requested",
		h.Days, h.FirstSnapshot.Format("2006-01-02"), h.Snapshots, days)
}

func (r *run) links(out *Tank, owned map[int]bool) error {
	vehicles, err := r.s.DB.AllVehicles(r.ctx)
	if err != nil {
		return err
	}
	edges, err := r.s.DB.AllEdges(r.ctx)
	if err != nil {
		return err
	}
	for _, e := range edges {
		switch out.TankID {
		case e.To:
			p := vehicles[e.From]
			out.ResearchedFrom = append(out.ResearchedFrom, Link{TankID: p.TankID, Name: p.Name, Tier: p.Tier, XPCost: e.XPCost, Owned: owned[p.TankID]})
		case e.From:
			c := vehicles[e.To]
			out.Unlocks = append(out.Unlocks, Link{TankID: c.TankID, Name: c.Name, Tier: c.Tier, XPCost: e.XPCost, Owned: owned[c.TankID]})
		}
	}
	sort.Slice(out.ResearchedFrom, func(i, j int) bool { return out.ResearchedFrom[i].TankID < out.ResearchedFrom[j].TankID })
	sort.Slice(out.Unlocks, func(i, j int) bool { return out.Unlocks[i].TankID < out.Unlocks[j].TankID })
	return nil
}

func (r *run) tankOverlay(out *Tank) {
	o := r.loadOverlay()
	if o == nil {
		return
	}
	for _, ref := range o.ResearchedNotBought {
		if ref.Resolved && ref.ID == out.TankID {
			out.Overlay.ResearchedNotBought = true
		}
	}
	for _, g := range r.goals() {
		if g.TargetID == out.TankID || g.ViaID == out.TankID {
			out.Overlay.Goals = append(out.Overlay.Goals, g)
			r.caveat("%s", r.goalXPCaveat())
		}
	}
}

// goals renders the overlay's XP goals.
func (r *run) goals() []Goal {
	o := r.loadOverlay()
	if o == nil {
		return nil
	}
	var out []Goal
	for _, g := range o.XPGoals {
		if !g.Target.Resolved {
			continue
		}
		goal := Goal{
			TargetID: g.Target.ID, Target: g.Target.Name,
			XPRequired: g.XPRequired, APIXPCost: g.APIXPCost, XPBanked: g.XPBanked,
		}
		if g.Via.Resolved {
			goal.ViaID, goal.Via = g.Via.ID, g.Via.Name
			// The game's own XP on the via vehicle replaces the hand-kept
			// figure; the overlay's stays as the backup.
			if c := r.client(); c != nil {
				if v, ok := c.vehicles[g.Via.ID]; ok && v.XP != nil && !v.Rented {
					xp := *v.XP
					goal.XPBanked, goal.XPBankedSource = &xp, "client mod"
					r.goalXPFromClient = true
				}
			}
		}
		required := g.XPRequired
		if required == nil {
			required = g.APIXPCost
		}
		if required != nil && goal.XPBanked != nil {
			rem := max(0, *required-*goal.XPBanked)
			goal.XPRemaining = &rem
		}
		out = append(out, goal)
	}
	return out
}

// ownedSet returns the identified garage as a set.
func (r *run) ownedSet() (map[int]bool, error) {
	garage, err := r.s.DB.Garage(r.ctx)
	if errors.Is(err, store.ErrNotFound) {
		r.caveat("no garage synced, so ownership is unknown")
		return map[int]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(map[int]bool, len(garage.Identified))
	for _, id := range garage.Identified {
		out[id] = true
	}
	r.dropRentals(out, nil)
	return out, nil
}

// goalXPCaveat accompanies any goal shown. Banked XP is typed in by hand and
// stops being true the moment the vehicle is played, so the caveat carries
// the date it was last written.
func (r *run) goalXPCaveat() string {
	if r.goalXPFromClient {
		return "goal XP banked is the via vehicle's own XP in the game client, from the client mod dump of " +
			r.client().account.CapturedAt.UTC().Format("2006-01-02 15:04 UTC") +
			"; part of it may yet go on that vehicle's own modules"
	}
	when := "an unknown date"
	if r.overlay != nil && !r.overlay.UpdatedAt.IsZero() {
		when = r.overlay.UpdatedAt.Format("2006-01-02")
	}
	return "goal XP banked is hand-maintained in the overlay (last updated " + when +
		"); no API exposes per-vehicle XP, so it is stale once the vehicle has been played since"
}
