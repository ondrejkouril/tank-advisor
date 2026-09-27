package query

import (
	"context"
	"errors"
	"sort"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// researchedMinTier is the lowest tier the game client's research list is
// offered from: the framework's current-play band (tier VIII+).
const researchedMinTier = 8

// Candidate origins.
const (
	OriginResearched   = "researched_not_bought" // researched and never bought: the game client's list, or the overlay's
	OriginNextResearch = "next_research"         // one tech-tree step from an owned vehicle
	OriginGoal         = "goal"                  // on an overlay goal's path, reachable or not
)

// Candidates is `query candidates`.
type Candidates struct {
	// Credits is what affordability is judged against: the account's credits,
	// or --budget-credits when given.
	Credits       int    `json:"credits"`
	CreditsSource string `json:"credits_source"`
	FreeXP        int    `json:"free_xp"`

	AvoidClasses []string `json:"avoid_classes"`
	// ExcludedByPreference counts candidates dropped by avoid_classes.
	ExcludedByPreference int `json:"excluded_by_preference"`

	// CreditBuffer is what must remain after a purchase (overlay
	// constraints.credit_buffer); affordable and credits_short include it.
	CreditBuffer int `json:"credit_buffer"`
	// FreeXPMaxTier is the highest tier free XP researches (overlay); 0 is
	// no limit.
	FreeXPMaxTier int `json:"free_xp_max_tier"`
	// ImprovementFocus names the classes the player is working on.
	ImprovementFocus []string `json:"improvement_focus"`

	Candidates []Candidate `json:"candidates"`
}

// Candidate is one vehicle that could be bought or researched next.
type Candidate struct {
	TankID int    `json:"tank_id"`
	Name   string `json:"name"`
	Tier   int    `json:"tier"`
	Class  string `json:"class"`
	Nation string `json:"nation"`
	Origin string `json:"origin"`

	// Researched comes from the game client when the mod's dump lists
	// research (true or false for every candidate); otherwise it is true for
	// overlay entries and null - unknown - for the rest, since no API has it.
	Researched *bool `json:"researched"`
	// Via lists the owned vehicles it is researched from.
	Via []Link `json:"via"`
	// XPCost is the research cost from the cheapest owned parent; 0 when
	// already researched.
	XPCost int `json:"xp_cost"`
	// XPRemaining is what is left after the XP banked towards it, when the
	// overlay records that; absent otherwise.
	XPRemaining *int `json:"xp_remaining,omitempty"`
	// FreeXPAllowed reports whether the player's policy lets free XP research
	// this tank (overlay free_xp_max_tier).
	FreeXPAllowed bool `json:"free_xp_allowed"`
	// FreeXPCovers reports whether free XP alone would pay what remains, and
	// is only ever true where FreeXPAllowed is.
	FreeXPCovers bool `json:"free_xp_covers"`
	// PathXP is the XP still needed from an owned vehicle when the way runs
	// through one not yet owned: the remainder of the step to it plus its own.
	PathXP int `json:"path_xp,omitempty"`
	// PlayedBefore is the random battles on record for a researched vehicle
	// not owned now: it was owned once and sold.
	PlayedBefore int `json:"played_before,omitempty"`

	PriceCredit  int  `json:"price_credit"`
	Affordable   bool `json:"affordable"`
	CreditsShort int  `json:"credits_short"`

	// ClassRank is the overlay's preference rank for the class, 1 most
	// preferred; 0 when unranked.
	ClassRank int `json:"class_rank,omitempty"`

	// Goal is "target" or "step" when the vehicle is on an overlay goal's path.
	Goal string `json:"goal,omitempty"`
}

// Candidates lists purchase and research options with their cost.
// budget, when positive, replaces the account's credits for affordability.
func (s *Service) Candidates(ctx context.Context, budget int) (Envelope, error) {
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcAccount, srcVehicles)

	out := Candidates{AvoidClasses: []string{}, ImprovementFocus: []string{}, Candidates: []Candidate{}, CreditsSource: "account"}
	state, err := s.DB.LatestAccountState(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		r.caveat("no account state synced, so credits and free XP are unknown and affordability is not judged")
	case err != nil:
		return Envelope{}, err
	default:
		out.Credits, out.FreeXP = state.Credits, state.FreeXP
	}
	if budget > 0 {
		out.Credits, out.CreditsSource = budget, "--budget-credits"
	}

	owned, err := r.ownedSet()
	if err != nil {
		return Envelope{}, err
	}
	vehicles, err := s.DB.AllVehicles(ctx)
	if err != nil {
		return Envelope{}, err
	}
	edges, err := s.DB.AllEdges(ctx)
	if err != nil {
		return Envelope{}, err
	}
	parents := map[int][]store.Edge{}
	for _, e := range edges {
		parents[e.To] = append(parents[e.To], e)
	}

	o := r.loadOverlay()
	avoid := map[string]bool{}
	if o != nil {
		for _, c := range o.Preferences.AvoidClasses {
			avoid[c] = true
			out.AvoidClasses = append(out.AvoidClasses, c)
		}
		out.CreditBuffer = o.Constraints.CreditBuffer
		out.FreeXPMaxTier = o.Preferences.FreeXPMaxTier
		out.ImprovementFocus = append(out.ImprovementFocus, o.Preferences.ImprovementFocus...)
	}

	byID := map[int]*Candidate{}
	add := func(id int, origin string) *Candidate {
		if c, ok := byID[id]; ok {
			return c
		}
		v, ok := vehicles[id]
		if !ok || owned[id] {
			return nil
		}
		c := &Candidate{
			TankID: id, Name: v.Name, Tier: v.Tier, Class: v.Type, Nation: v.Nation,
			Origin: origin, PriceCredit: v.PriceCredit, Via: []Link{},
		}
		if o != nil {
			c.ClassRank = o.Preferences.ClassRank[v.Type]
		}
		for _, e := range parents[id] {
			if owned[e.From] {
				p := vehicles[e.From]
				c.Via = append(c.Via, Link{TankID: p.TankID, Name: p.Name, Tier: p.Tier, XPCost: e.XPCost, Owned: true})
			}
		}
		byID[id] = c
		return c
	}

	// Researched-not-bought comes first, so its origin wins when the same
	// vehicle is also one step from an owned one. The game client's research
	// list is the source when the mod's dump has one; the overlay's hand-kept
	// list is the backup.
	var unlocked map[int]bool
	if c := r.client(); c != nil {
		unlocked = c.unlocked
	}
	if unlocked != nil {
		r.use(srcTankStats)
		played, err := r.latestRandom()
		if err != nil {
			return Envelope{}, err
		}
		yes := true
		// A sold tank whose next step was researched too was a step passed
		// through on the way up; one with nothing researched after it was a
		// destination the player left, and may be worth buying back.
		passedThrough := map[int]bool{}
		for _, e := range edges {
			if unlocked[e.To] {
				passedThrough[e.From] = true
			}
		}
		lower, steps := 0, 0
		for id := range unlocked {
			v, known := vehicles[id]
			// Premiums and gifts are bought, not researched.
			if !known || owned[id] || v.IsPremium || v.IsGift {
				continue
			}
			// A long account has researched through every tree it touched,
			// starters included: the live one lists 255 unowned vehicles, 200
			// of them below tier VIII. Only the framework's current-play band
			// (tier VIII+, §3.2) is a purchase question, and of the tanks sold
			// only those where the line stopped; the rest are counted.
			if v.Tier < researchedMinTier {
				lower++
				continue
			}
			n := played[id].Battles
			if n > 0 && passedThrough[id] {
				steps++
				continue
			}
			if c := add(id, OriginResearched); c != nil {
				c.Researched = &yes
				// Sold earlier, and can be bought back with no XP.
				c.PlayedBefore = n
			}
		}
		if lower+steps > 0 {
			r.caveat("%d more researched vehicle(s) are not owned and not listed: %d below tier %d, and %d sold "+
				"after their next step was researched", lower+steps, lower, researchedMinTier, steps)
		}
	} else if o != nil {
		yes := true
		for _, ref := range o.ResearchedNotBought {
			if ref.Resolved {
				if c := add(ref.ID, OriginResearched); c != nil {
					c.Researched = &yes
				}
			}
		}
	}
	for _, e := range edges {
		if owned[e.From] && !owned[e.To] && !vehicles[e.To].IsPremium && !vehicles[e.To].IsGift {
			add(e.To, OriginNextResearch)
		}
	}
	goals := r.goals()
	if len(goals) > 0 {
		r.caveat("%s", r.goalXPCaveat())
	}
	// Targets first, so a vehicle that is one goal's target and the next
	// goal's step is labelled as the target it is.
	for _, g := range goals {
		if c := add(g.TargetID, OriginGoal); c != nil {
			c.Goal = "target"
			c.XPRemaining = g.XPRemaining
			if len(c.Via) == 0 && g.ViaID != 0 && !owned[g.ViaID] {
				// Reachable only through a vehicle not yet owned: cost the
				// step from it, so the number is not missing.
				cost := 0
				if g.APIXPCost != nil {
					cost = *g.APIXPCost
				} else if g.XPRequired != nil {
					cost = *g.XPRequired
				}
				v := vehicles[g.ViaID]
				c.Via = append(c.Via, Link{TankID: v.TankID, Name: v.Name, Tier: v.Tier, XPCost: cost, Owned: false})
			}
		}
	}
	for _, g := range goals {
		if g.ViaID == 0 {
			continue
		}
		if c := add(g.ViaID, OriginGoal); c != nil && c.Goal == "" {
			c.Goal = "step"
		}
	}

	// With the client's list, every candidate's research state is known.
	if unlocked != nil {
		for _, c := range byID {
			researched := unlocked[c.TankID]
			c.Researched = &researched
		}
	}
	for _, c := range byID {
		if c.Researched == nil || !*c.Researched {
			c.XPCost = cheapest(c.Via)
			c.FreeXPAllowed = out.FreeXPMaxTier == 0 || c.Tier <= out.FreeXPMaxTier
			c.FreeXPCovers = c.FreeXPAllowed && out.FreeXP >= c.remaining() && c.remaining() > 0
		}
	}
	// A goal reached through a vehicle not yet owned costs both steps. Saying
	// only the last one would understate the grind by the whole first step.
	for _, c := range byID {
		if c.Goal != "target" || len(c.Via) != 1 || c.Via[0].Owned {
			continue
		}
		if step, ok := byID[c.Via[0].TankID]; ok && len(step.Via) > 0 && step.Via[0].Owned {
			c.PathXP = step.remaining() + c.remaining()
		} else {
			r.caveat("goal %s is reached through %s, which is not one step from any owned vehicle; path_xp is not computed",
				c.Name, c.Via[0].Name)
		}
	}

	for _, c := range byID {
		if avoid[c.Class] && c.Goal == "" {
			out.ExcludedByPreference++
			continue
		}
		if state.SnapshotID != 0 || budget > 0 {
			c.Affordable = out.Credits-out.CreditBuffer >= c.PriceCredit
			c.CreditsShort = max(0, c.PriceCredit+out.CreditBuffer-out.Credits)
		}
		out.Candidates = append(out.Candidates, *c)
	}

	order := map[string]int{OriginResearched: 0, OriginGoal: 1, OriginNextResearch: 2}
	sort.Slice(out.Candidates, func(i, j int) bool {
		a, b := out.Candidates[i], out.Candidates[j]
		if order[a.Origin] != order[b.Origin] {
			return order[a.Origin] < order[b.Origin]
		}
		if a.Tier != b.Tier {
			return a.Tier > b.Tier
		}
		return a.Name < b.Name
	})

	if unlocked != nil {
		r.caveat("researched comes from the game client (the client mod dump), for every candidate; " +
			"xp_cost is the full research cost of what is not yet researched, and xp_remaining subtracts the XP on the via vehicle")
	} else {
		r.caveat("research progress is not exposed by any API: researched is known only for overlay entries, " +
			"xp_cost is the full research cost, and xp_remaining subtracts banked XP only where the overlay records it")
	}
	return r.finish(out), nil
}

// remaining is the XP still needed: the goal's remainder where the overlay
// records banked XP, otherwise the full research cost.
func (c *Candidate) remaining() int {
	if c.XPRemaining != nil {
		return *c.XPRemaining
	}
	return c.XPCost
}

// cheapest returns the lowest XP cost among the links, or 0 with none.
func cheapest(links []Link) int {
	best := 0
	for i, l := range links {
		if i == 0 || l.XPCost < best {
			best = l.XPCost
		}
	}
	return best
}
