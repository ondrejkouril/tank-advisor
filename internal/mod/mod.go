// Package mod reads the garage dump written by the wotctx client mod
// (mod/mod_wotctx.py; docs/spec.md section 3.6).
//
// The dump is what the game client knows and no API does: per-vehicle XP,
// Marks of Excellence and their progress, loadouts, crew, and the real premium
// state. It is only as fresh as the last time the game sat in the garage, so
// every reader has to carry its capture time.
//
// Fields the mod could not read after a game update arrive as JSON null, which
// is why most of them are pointers here: a missing figure must not become a
// zero.
package mod

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Schema is the dump format this package reads. The mod writes it as
// "schema"; a change to the format bumps both.
const Schema = 1

// Dump is the whole file.
type Dump struct {
	Schema      int       `json:"schema"`
	ModVersion  string    `json:"mod_version"`
	CapturedAt  time.Time `json:"captured_at"`
	GameVersion string    `json:"game_version"`
	AccountID   int       `json:"account_id"`
	Resources   Resources `json:"resources"`
	Premium     Premium   `json:"premium"`
	Vehicles    []Vehicle `json:"vehicles"`
	// Unlocked lists every researched vehicle's tank_id, owned or not. It is
	// nil in a dump from mod 0.2.x, which did not write it, and empty (not
	// nil) when the list was written with nothing in it.
	Unlocked []int `json:"unlocked"`
	// Errors names the parts the mod failed to read, capped at 20.
	Errors []string `json:"errors"`
}

// Resources are the account's money and free XP, as the client shows them.
type Resources struct {
	Credits *int `json:"credits"`
	Gold    *int `json:"gold"`
	Bonds   *int `json:"bonds"`
	FreeXP  *int `json:"free_xp"`
}

// Premium is the client's own premium state. The API's is wrong for this
// account (docs/spec.md section 3.1).
type Premium struct {
	Premium     *bool      `json:"premium"`
	PremiumType *int       `json:"premium_type"`
	ExpiresAt   *time.Time `json:"premium_expires_at"`
	WotPlus     *bool      `json:"wot_plus"`
}

// Vehicle is one vehicle in the garage, rentals included.
type Vehicle struct {
	TankID int `json:"tank_id"`
	// Name is the client's technical name ("ussr:R97_Object_140"), the same
	// in every language; UserName is what the garage shows, in the client's
	// language.
	Name     string `json:"name"`
	UserName string `json:"user_name"`
	Tier     int    `json:"tier"`
	XP       *int   `json:"xp"`
	Elite    *bool  `json:"elite"`
	Rented   *bool  `json:"rented"`
	Marks    *Marks `json:"marks"`
	// The fitted items, one entry per slot; nil for an empty slot. The whole
	// list is nil when the mod could not read it.
	Equipment   []*Item    `json:"equipment"`
	Shells      []*Shell   `json:"shells"`
	Consumables []*Item    `json:"consumables"`
	Directives  []*Item    `json:"directives"`
	Crew        []*Tankman `json:"crew"`
}

// Marks are the Marks of Excellence and the progress towards the next.
type Marks struct {
	Marks int `json:"marks"`
	// Percent is the figure the garage shows, e.g. 75.15.
	Percent float64 `json:"percent"`
	// MovingAvgDamage is the average the mark is computed from: damage plus
	// the larger of spotting and tracking assist.
	MovingAvgDamage int `json:"moving_avg_damage"`
}

// Item is one fitted piece of equipment, consumable or directive.
type Item struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	UserName string `json:"user_name"`
}

// Shell is one ammunition type and how many are loaded.
type Shell struct {
	Item
	Kind  string `json:"kind"` // ARMOR_PIERCING, HOLLOW_CHARGE, HIGH_EXPLOSIVE, ...
	Count *int   `json:"count"`
}

// Tankman is one crew seat. Role is nil for an empty seat.
type Tankman struct {
	Slot        int                 `json:"slot"`
	Role        *string             `json:"role"`
	Skills      []Skill             `json:"skills"`
	BonusSkills map[string][]string `json:"bonus_skills"`
}

// Skill is a crew skill and its training, 0-100.
type Skill struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
}

// Parse reads a dump and checks what wotctx relies on.
func Parse(raw []byte) (Dump, error) {
	var d Dump
	if err := json.Unmarshal(raw, &d); err != nil {
		return Dump{}, fmt.Errorf("reading the mod dump: %w", err)
	}
	if d.Schema != Schema {
		return Dump{}, fmt.Errorf("the mod dump has schema %d and this wotctx reads %d; update whichever is older", d.Schema, Schema)
	}
	if d.CapturedAt.IsZero() {
		return Dump{}, errors.New("the mod dump has no captured_at")
	}
	if d.AccountID == 0 {
		return Dump{}, errors.New("the mod dump names no account")
	}
	return d, nil
}

// Owned reports whether the vehicle is the player's own, as opposed to an
// event rental. Steel Hunter vehicles are owned in the client's sense but
// belong to a separate mode; their tier I is how the client lists them.
func (v Vehicle) Owned() bool {
	return v.Rented == nil || !*v.Rented
}
