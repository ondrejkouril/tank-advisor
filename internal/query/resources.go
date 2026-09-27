package query

import (
	"context"
	"errors"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// Resources is `query resources`.
type Resources struct {
	ObservedAt time.Time `json:"observed_at"`
	Credits    int       `json:"credits"`
	Gold       int       `json:"gold"`
	Bonds      int       `json:"bonds"`
	FreeXP     int       `json:"free_xp"`

	// Premium comes from the game client's dump, or else the overlay; never
	// the API, whose flag is wrong (spec section 3.1). A nil field means
	// unknown, which is not the same as false.
	Premium Premium `json:"premium"`

	// Reserves summarises Personal Reserves. The API identifies each kind only
	// by a numeric id with no name, so inactive stock is counted rather than
	// listed; active ones are listed because they change earnings right now.
	Reserves Reserves `json:"reserves"`

	// LastBattle is when the account last played, per the API.
	LastBattle *time.Time `json:"last_battle,omitempty"`
}

// Premium is the account's premium status. Source is "client mod" (the
// game's own state, from the mod's dump), "overlay" or "unknown".
type Premium struct {
	PremiumAccount *bool  `json:"premium_account"`
	WoTPlus        *bool  `json:"wot_plus"`
	Source         string `json:"source"`
	// ExpiresAt is when the Premium Account runs out, from the client.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Reserves is the Personal Reserve summary.
type Reserves struct {
	Kinds  int       `json:"kinds"`
	Count  int       `json:"count"`
	Active []Booster `json:"active"`
}

// Booster is one Personal Reserve.
type Booster struct {
	Kind      string     `json:"kind"`
	Count     int        `json:"count"`
	State     string     `json:"state"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Resources returns credits, gold, bonds, free XP, premium and reserves.
func (s *Service) Resources(ctx context.Context) (Envelope, error) {
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcAccount)

	state, err := s.DB.LatestAccountState(ctx)
	if errors.Is(err, store.ErrNotFound) {
		r.caveat("no account state synced, so resources are unknown")
		return r.finish(nil), nil
	}
	if err != nil {
		return Envelope{}, err
	}

	out := Resources{
		ObservedAt: state.ObservedAt,
		Credits:    state.Credits,
		Gold:       state.Gold,
		Bonds:      state.Bonds,
		FreeXP:     state.FreeXP,
		Premium:    r.premium(),
		Reserves:   Reserves{Active: []Booster{}},
	}
	if !state.LastBattleTime.IsZero() {
		t := state.LastBattleTime
		out.LastBattle = &t
	}
	if state.Credits == 0 && state.Gold == 0 && state.FreeXP == 0 {
		r.caveat("resources read as zero, which usually means the private block was missing (no valid access token at sync)")
	}

	boosters, err := s.DB.LatestBoosters(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Envelope{}, err
	}
	for _, b := range boosters {
		booster := Booster{Kind: b.Kind, Count: b.Count, State: b.State}
		if !b.ExpiresAt.IsZero() {
			t := b.ExpiresAt
			booster.ExpiresAt = &t
		}
		out.Reserves.Kinds++
		out.Reserves.Count += b.Count
		if b.State == "ACTIVE" {
			out.Reserves.Active = append(out.Reserves.Active, booster)
		}
	}

	return r.finish(out), nil
}

// premium reports premium status from the game client, then the overlay,
// then unknown. The client's is the game's own state; the overlay is the
// hand-kept backup for when there is no dump.
func (r *run) premium() Premium {
	if c := r.client(); c != nil && c.account.Premium != nil {
		active := *c.account.Premium
		p := Premium{Source: "client mod", WoTPlus: c.account.WotPlus}
		if exp := c.account.PremiumExpiresAt; !exp.IsZero() {
			// The dump may be days old; a Premium Account that has run out
			// since is not active, whatever the dump said then.
			active = active && exp.After(r.now)
			p.ExpiresAt = &exp
		}
		p.PremiumAccount = &active
		return p
	}
	p := Premium{Source: "overlay"}
	o := r.loadOverlay()
	if o == nil || o.Premium == nil {
		p.Source = "unknown"
		r.caveat("premium status is unknown; the API's is_premium is wrong for this account and is never reported")
		return p
	}
	p.PremiumAccount = o.Premium.PremiumAccount
	p.WoTPlus = o.Premium.WoTPlus
	return p
}
