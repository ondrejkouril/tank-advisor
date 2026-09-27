package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/ondrejkouril/tank-advisor/internal/overlay"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/yamledit"
)

// TankChoice is a tank reference as the Goals page holds it: what the file
// says (a name, or a tank_id for a name several vehicles share), and the
// vehicle it resolves to.
type TankChoice struct {
	Input string `json:"input"`
	// ID is true when Input is a tank_id.
	ID    bool   `json:"id"`
	Label string `json:"label"`
	// TankID is the vehicle it resolves to, or 0: how the page matches a
	// reference written "Selma" to the tech tree's "Šelma".
	TankID int `json:"tankId"`
}

// GoalSetting is one XP goal.
type GoalSetting struct {
	Target     TankChoice `json:"target"`
	Via        TankChoice `json:"via"`
	XPRequired *int       `json:"xpRequired"`
	XPBanked   *int       `json:"xpBanked"`
}

// GoalsPage is the Goals page (docs/spec-desktop.md section 5.3).
type GoalsPage struct {
	Path    string        `json:"path"`
	Version string        `json:"version"`
	Problem string        `json:"problem,omitempty"`
	Goals   []GoalSetting `json:"goals"`
	// HasDump is true when the client mod's dump exists. It is then the
	// source for premium status, researched tanks and banked XP, and the page
	// hides those backup fields (the dump wins, docs/spec.md section 5).
	HasDump             bool         `json:"hasDump"`
	PremiumAccount      *bool        `json:"premiumAccount"`
	WoTPlus             *bool        `json:"wotPlus"`
	ResearchedNotBought []TankChoice `json:"researchedNotBought"`
	// Warnings are the overlay's findings that do not block a save.
	Warnings []string `json:"warnings"`
}

// ViaChoice is a vehicle the target is researched from, with its cost.
type ViaChoice struct {
	Tank   TankChoice `json:"tank"`
	XPCost int        `json:"xpCost"`
}

func tankLabel(v store.Vehicle) string {
	class := map[string]string{"lightTank": "light", "mediumTank": "medium", "heavyTank": "heavy", "AT-SPG": "TD", "SPG": "SPG"}[v.Type]
	return fmt.Sprintf("%s (tier %s %s, %s)", v.Name, roman(v.Tier), class, v.Nation)
}

func roman(n int) string {
	numerals := []string{"", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI"}
	if n > 0 && n < len(numerals) {
		return numerals[n]
	}
	return strconv.Itoa(n)
}

func refChoice(r overlay.TankRef) TankChoice {
	c := TankChoice{Input: r.Input, ID: r.IsID()}
	if r.Resolved {
		c.Label = fmt.Sprintf("%s (tier %s)", r.Name, roman(r.Tier))
		c.TankID = r.ID
	}
	return c
}

// yamlValue is how the file writes a reference: an unquoted integer for a
// tank_id, a string for a name.
func (c TankChoice) yamlValue() any {
	if c.ID {
		if id, err := strconv.Atoi(c.Input); err == nil {
			return id
		}
	}
	return strings.TrimSpace(c.Input)
}

// goalYAML is one goal as the file writes it, keys in the file's order.
type goalYAML struct {
	Target     any  `yaml:"target"`
	Via        any  `yaml:"via,omitempty"`
	XPRequired *int `yaml:"xp_required,omitempty"`
	XPBanked   *int `yaml:"xp_banked,omitempty"`
}

// GoalsPage loads the Goals page, with every tank resolved against the
// synced vehicles.
func (s *Service) GoalsPage(ctx context.Context) GoalsPage {
	env, f := s.loadOverlay()
	page := GoalsPage{Path: f.path, Version: f.version, Problem: f.problem,
		Goals: []GoalSetting{}, ResearchedNotBought: []TankChoice{}, Warnings: []string{}}
	o := f.parsed
	if env.HasStore() {
		if db, err := env.OpenStore(ctx); err == nil {
			if findings, _, err := o.Resolve(ctx, db); err == nil {
				for _, fd := range findings {
					page.Warnings = append(page.Warnings, fd.String())
				}
			}
			if _, err := db.LatestModAccount(ctx); err == nil {
				page.HasDump = true
			}
			db.Close()
		}
	}
	for _, g := range o.XPGoals {
		page.Goals = append(page.Goals, GoalSetting{Target: refChoice(g.Target), Via: refChoice(g.Via), XPRequired: g.XPRequired, XPBanked: g.XPBanked})
	}
	if o.Premium != nil {
		page.PremiumAccount, page.WoTPlus = o.Premium.PremiumAccount, o.Premium.WoTPlus
	}
	for _, r := range o.ResearchedNotBought {
		page.ResearchedNotBought = append(page.ResearchedNotBought, refChoice(r))
	}
	return page
}

// FindTanks offers vehicles matching what the player typed. A name several
// vehicles share is offered once per vehicle, by tank_id, so an ambiguous
// name can never be saved (docs/spec.md section 5).
func (s *Service) FindTanks(ctx context.Context, query string) []TankChoice {
	out := []TankChoice{}
	env, _ := s.loadOverlay()
	if !env.HasStore() || strings.TrimSpace(query) == "" {
		return out
	}
	db, err := env.OpenStore(ctx)
	if err != nil {
		return out
	}
	defer db.Close()
	// First what the file's own rules would resolve the text to (they fold
	// accents and punctuation: "tesak" is the Vz. 71 Tesák), then names
	// that contain it.
	names := []string{query}
	_, exactErr := db.VehicleByName(ctx, query)
	var amb *store.AmbiguousNameError
	resolved := exactErr == nil || errors.As(exactErr, &amb)
	if more, err := db.SuggestVehicleNames(ctx, query, 8); err == nil {
		for _, name := range more {
			// Once the text names a vehicle, the store's "did you mean"
			// guesses only add noise; names containing it still help.
			if !resolved || strings.Contains(looseKey(name), looseKey(query)) {
				names = append(names, name)
			}
		}
	}
	seen := map[int]bool{}
	for _, name := range names {
		v, err := db.VehicleByName(ctx, name)
		var amb *store.AmbiguousNameError
		switch {
		case errors.As(err, &amb):
			for _, c := range amb.Candidates {
				if !seen[c.TankID] {
					seen[c.TankID] = true
					out = append(out, TankChoice{Input: strconv.Itoa(c.TankID), ID: true, Label: tankLabel(c), TankID: c.TankID})
				}
			}
		case err == nil && !seen[v.TankID]:
			seen[v.TankID] = true
			out = append(out, TankChoice{Input: v.Name, Label: tankLabel(v), TankID: v.TankID})
		}
	}
	return out
}

// ResearchedFrom lists the vehicles target is researched from, with the XP
// each step costs: the goal's "via", which fills in xp_required.
func (s *Service) ResearchedFrom(ctx context.Context, target TankChoice) []ViaChoice {
	out := []ViaChoice{}
	env, _ := s.loadOverlay()
	if !env.HasStore() {
		return out
	}
	db, err := env.OpenStore(ctx)
	if err != nil {
		return out
	}
	defer db.Close()
	v, err := resolveChoice(ctx, db, target)
	if err != nil {
		return out
	}
	edges, err := db.PreviousVehicles(ctx, v.TankID)
	if err != nil {
		return out
	}
	for _, e := range edges {
		from, err := db.VehicleByID(ctx, e.From)
		if err != nil {
			continue
		}
		choice := TankChoice{Input: from.Name, Label: tankLabel(from), TankID: from.TankID}
		// A shared name is written as its tank_id.
		if _, err := db.VehicleByName(ctx, from.Name); err != nil {
			choice = TankChoice{Input: strconv.Itoa(from.TankID), ID: true, Label: tankLabel(from), TankID: from.TankID}
		}
		out = append(out, ViaChoice{Tank: choice, XPCost: e.XPCost})
	}
	return out
}

func resolveChoice(ctx context.Context, db *store.DB, c TankChoice) (store.Vehicle, error) {
	if c.ID {
		id, err := strconv.Atoi(c.Input)
		if err != nil {
			return store.Vehicle{}, err
		}
		return db.VehicleByID(ctx, id)
	}
	return db.VehicleByName(ctx, c.Input)
}

// saveGoalsPage is the page's Save. arg is {"version": ..., "page": GoalsPage}.
func (s *Service) saveGoalsPage(ctx context.Context, arg string) Result {
	var req struct {
		Version string    `json:"version"`
		Page    GoalsPage `json:"page"`
	}
	if err := json.Unmarshal([]byte(arg), &req); err != nil {
		return Result{Message: "unreadable goals: " + err.Error()}
	}
	var goals []goalYAML
	for i, g := range req.Page.Goals {
		if strings.TrimSpace(g.Target.Input) == "" {
			return Result{Message: fmt.Sprintf("Goal %d has no tank.", i+1)}
		}
		y := goalYAML{Target: g.Target.yamlValue(), XPRequired: g.XPRequired, XPBanked: g.XPBanked}
		if strings.TrimSpace(g.Via.Input) != "" {
			y.Via = g.Via.yamlValue()
		}
		goals = append(goals, y)
	}
	var researched []any
	for _, r := range req.Page.ResearchedNotBought {
		if strings.TrimSpace(r.Input) != "" {
			researched = append(researched, r.yamlValue())
		}
	}
	orNil := func(v any, empty bool) any {
		if empty {
			return nil
		}
		return v
	}
	updates := []yamledit.Update{
		{Key: "xp_goals", Value: orNil(goals, len(goals) == 0)},
		{Key: "researched_not_bought", Value: orNil(researched, len(researched) == 0)},
		{Key: "premium.premium_account", Value: orNil(req.Page.PremiumAccount, req.Page.PremiumAccount == nil)},
		{Key: "premium.wot_plus", Value: orNil(req.Page.WoTPlus, req.Page.WoTPlus == nil)},
	}
	// Every tank must name exactly one vehicle, which only the synced
	// vehicles can tell.
	check := func(ctx context.Context, o *overlay.Overlay) []string {
		env, _ := s.loadOverlay()
		if !env.HasStore() {
			return nil
		}
		db, err := env.OpenStore(ctx)
		if err != nil {
			return nil
		}
		defer db.Close()
		findings, _, err := o.Resolve(ctx, db)
		if err != nil {
			return []string{err.Error()}
		}
		return errorFindings(findings)
	}
	return s.saveOverlay(ctx, req.Version, updates, check)
}

// looseKey is a name reduced to its letters and digits, without accents:
// how the overlay's names are matched (internal/store).
func looseKey(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Mn, r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
