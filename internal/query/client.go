package query

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// srcMod is the client mod's garage dump (docs/spec.md section 3.6).
const srcMod = "mod:garage"

// clientDump is the latest dump, loaded once per query.
type clientDump struct {
	account  store.ModAccount
	vehicles map[int]store.ModVehicle
	marks    map[int]store.MarkProgress
	// unlocked is nil when the dump carries no research list (mod 0.2.x).
	unlocked map[int]bool
	// playedSince is true when Wargaming records a battle after the dump:
	// vehicle XP and marks have moved on since.
	playedSince bool
}

// client returns the latest client mod dump, or nil when there is none. The
// first call records the source and any staleness caveat; with no dump the
// query is exactly what it was before the mod existed.
func (r *run) client() *clientDump {
	if r.clientLoaded {
		return r.clientData
	}
	r.clientLoaded = true

	acct, err := r.s.DB.LatestModAccount(r.ctx)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			r.caveat("client mod dump unreadable (%s), so it was not used", oneLine(err.Error()))
		}
		return nil
	}
	c := &clientDump{account: acct, vehicles: map[int]store.ModVehicle{}}
	vehicles, err := r.s.DB.ModVehicles(r.ctx, acct.SnapshotID)
	if err != nil {
		r.caveat("client mod dump unreadable (%s), so it was not used", oneLine(err.Error()))
		return nil
	}
	for _, v := range vehicles {
		c.vehicles[v.TankID] = v
	}
	if c.marks, err = r.s.DB.ModMarks(r.ctx, acct.SnapshotID); err != nil {
		c.marks = map[int]store.MarkProgress{}
	}
	if unlocked, ok, err := r.s.DB.ModUnlocked(r.ctx, acct.SnapshotID); err == nil && ok {
		c.unlocked = unlocked
	}
	if state, err := r.s.DB.LatestAccountState(r.ctx); err == nil && state.LastBattleTime.After(acct.CapturedAt) {
		c.playedSince = true
	}

	r.use(srcMod)
	if c.playedSince {
		r.caveat("the client mod dump is from %s and battles were played after it, so vehicle XP and marks are "+
			"behind until the game next sits in the garage", acct.CapturedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if n := len(acct.Errors); n > 0 {
		r.caveat("the client mod could not read %d part(s) of the game, which are missing: %s",
			n, oneLine(acct.Errors[0]))
	}
	r.clientData = c
	return c
}

// rentals lists the vehicles the client marks as rented, which count as
// garage vehicles in the API but are not the player's.
func (c *clientDump) rentals() map[int]bool {
	out := map[int]bool{}
	for id, v := range c.vehicles {
		if v.Rented {
			out[id] = true
		}
	}
	return out
}

// dropRentals removes rentals from an owned set, with a caveat naming them.
func (r *run) dropRentals(owned map[int]bool, names map[int]string) {
	c := r.client()
	if c == nil {
		return
	}
	var dropped []string
	for id := range c.rentals() {
		if owned[id] {
			delete(owned, id)
			name := names[id]
			if name == "" {
				name = c.vehicles[id].UserName
			}
			dropped = append(dropped, name)
		}
	}
	if len(dropped) > 0 {
		sort.Strings(dropped)
		r.caveat("left out as rentals, per the game client: %s", strings.Join(dropped, ", "))
	}
}

// TankClient is what the game client knows about one vehicle and no API does.
type TankClient struct {
	// CapturedAt is when the game wrote the dump these figures come from.
	CapturedAt time.Time `json:"captured_at"`
	// XP is the vehicle's own unspent experience.
	XP    *int  `json:"xp"`
	Elite *bool `json:"elite"`
	// Researched is nil when the dump carries no research list.
	Researched  *bool         `json:"researched,omitempty"`
	Marks       *ClientMarks  `json:"marks,omitempty"`
	Equipment   []ClientItem  `json:"equipment"`
	Shells      []ClientShell `json:"shells"`
	Consumables []ClientItem  `json:"consumables"`
	Directives  []ClientItem  `json:"directives"`
	Crew        []ClientSeat  `json:"crew"`
}

// ClientMarks is a vehicle's Marks of Excellence as the garage shows them.
type ClientMarks struct {
	Marks int `json:"marks"`
	// Percent is the progress figure the garage shows, e.g. 75.15.
	Percent float64 `json:"percent"`
	// MovingAvgDamage is the average the marks are computed from: damage plus
	// the larger of spotting and tracking assist, weighted to recent battles.
	MovingAvgDamage *int `json:"moving_avg_damage"`
}

// ClientItem is a fitted item. Name is the client's technical name, the same
// in every language; UserName is the garage's, in the client's language.
type ClientItem struct {
	Name     string `json:"name"`
	UserName string `json:"user_name"`
}

// ClientShell is one shell type and how many are loaded.
type ClientShell struct {
	ClientItem
	Kind  string `json:"kind"`
	Count *int   `json:"count"`
}

// ClientSeat is a crew seat; an empty seat has no role.
type ClientSeat struct {
	Role        string              `json:"role"`
	Skills      []store.CrewSkill   `json:"skills"`
	BonusSkills map[string][]string `json:"bonus_skills,omitempty"`
}

// tankClient builds the client block for one vehicle, or nil when the dump
// does not have it.
func (r *run) tankClient(tankID int) (*TankClient, error) {
	c := r.client()
	if c == nil {
		return nil, nil
	}
	out := &TankClient{
		CapturedAt: c.account.CapturedAt,
		Equipment:  []ClientItem{}, Shells: []ClientShell{}, Consumables: []ClientItem{},
		Directives: []ClientItem{}, Crew: []ClientSeat{},
	}
	if c.unlocked != nil {
		researched := c.unlocked[tankID]
		out.Researched = &researched
	}
	v, ok := c.vehicles[tankID]
	if !ok {
		// Not in the garage: only the research flag applies.
		if out.Researched == nil {
			return nil, nil
		}
		return out, nil
	}
	out.XP, out.Elite = v.XP, v.Elite
	if v.Elite != nil && !*v.Elite {
		r.caveat("%s", NotEliteCaveat)
	}
	if m, ok := c.marks[tankID]; ok {
		out.Marks = &ClientMarks{Marks: m.Marks, Percent: m.Percent, MovingAvgDamage: v.MovingAvgDamage}
	}

	loadout, err := r.s.DB.ModLoadout(r.ctx, c.account.SnapshotID, tankID)
	if err != nil {
		return nil, err
	}
	for _, s := range loadout {
		item := ClientItem{Name: s.ItemName, UserName: s.UserName}
		switch s.Kind {
		case "equipment":
			out.Equipment = append(out.Equipment, item)
		case "shell":
			out.Shells = append(out.Shells, ClientShell{ClientItem: item, Kind: s.ShellKind, Count: s.Count})
		case "consumable":
			out.Consumables = append(out.Consumables, item)
		case "directive":
			out.Directives = append(out.Directives, item)
		}
	}
	crew, err := r.s.DB.ModCrew(r.ctx, c.account.SnapshotID, tankID)
	if err != nil {
		return nil, err
	}
	for _, seat := range crew {
		skills := seat.Skills
		if skills == nil {
			skills = []store.CrewSkill{}
		}
		out.Crew = append(out.Crew, ClientSeat{Role: seat.Role, Skills: skills, BonusSkills: nonEmpty(seat.BonusSkills)})
	}
	return out, nil
}

func nonEmpty(m map[string][]string) map[string][]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
