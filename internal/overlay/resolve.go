package overlay

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// Catalog is the synced data an overlay is checked against. *store.DB
// satisfies it.
type Catalog interface {
	VehicleCount(ctx context.Context) (int, error)
	VehicleByID(ctx context.Context, tankID int) (store.Vehicle, error)
	VehicleByName(ctx context.Context, name string) (store.Vehicle, error)
	SuggestVehicleNames(ctx context.Context, name string, limit int) ([]string, error)
	NextVehicles(ctx context.Context, tankID int) ([]store.Edge, error)
	Garage(ctx context.Context) (store.GarageBreakdown, error)
}

// Coverage records which checks Resolve could actually run. A check that was
// skipped for lack of data is not a check that passed, and callers have to be
// able to say which.
type Coverage struct {
	// NamesChecked is false when the vehicle list has not been synced.
	NamesChecked bool `json:"names_checked"`
	// GarageChecked is false when no garage has been synced.
	GarageChecked bool `json:"garage_checked"`
}

// Resolve checks every tank reference against the catalog, filling in the
// vehicle each one names, and flags entries the garage has overtaken.
//
// The returned error is for a failure to read the catalog; problems with the
// overlay itself are findings. A nil catalog skips resolution and says so.
func (o *Overlay) Resolve(ctx context.Context, cat Catalog) ([]Finding, Coverage, error) {
	var (
		findings []Finding
		coverage Coverage
	)
	add := func(sev Severity, field string, line int, format string, args ...any) {
		findings = append(findings, Finding{Severity: sev, Field: field, Line: line, Message: fmt.Sprintf(format, args...)})
	}

	if cat != nil {
		n, err := cat.VehicleCount(ctx)
		if err != nil {
			return nil, coverage, err
		}
		coverage.NamesChecked = n > 0
	}
	if !coverage.NamesChecked {
		add(SeverityWarning, "", 0, "the vehicle list has not been synced, so tank names were not checked (run: wotctx sync --only wg)")
		return findings, coverage, nil
	}

	resolve := func(ref *TankRef, field string) error {
		if ref.IsZero() {
			return nil // already an error from Parse where it is required
		}
		msg, err := resolveRef(ctx, cat, ref)
		if err != nil {
			return err
		}
		if msg != "" {
			add(SeverityError, field, ref.line, "%s", msg)
		}
		return nil
	}

	for i := range o.ResearchedNotBought {
		if err := resolve(&o.ResearchedNotBought[i], fmt.Sprintf("researched_not_bought[%d]", i)); err != nil {
			return nil, coverage, err
		}
	}
	for i := range o.XPGoals {
		field := fmt.Sprintf("xp_goals[%d]", i)
		if err := resolve(&o.XPGoals[i].Target, field+".target"); err != nil {
			return nil, coverage, err
		}
		if err := resolve(&o.XPGoals[i].Via, field+".via"); err != nil {
			return nil, coverage, err
		}
	}
	for i := range o.ResearchPaths {
		field := fmt.Sprintf("research_paths[%d]", i)
		if err := resolve(&o.ResearchPaths[i].From, field+".from"); err != nil {
			return nil, coverage, err
		}
		if err := resolve(&o.ResearchPaths[i].To, field+".to"); err != nil {
			return nil, coverage, err
		}
	}

	seen := map[int]int{}
	for i, ref := range o.ResearchedNotBought {
		if !ref.Resolved {
			continue
		}
		if first, dup := seen[ref.ID]; dup {
			add(SeverityWarning, fmt.Sprintf("researched_not_bought[%d]", i), ref.line,
				"%s is listed twice (first as entry %d)", ref.Name, first)
			continue
		}
		seen[ref.ID] = i
	}

	for i := range o.XPGoals {
		if err := o.checkGoalEdge(ctx, cat, i, add); err != nil {
			return nil, coverage, err
		}
	}
	for i, p := range o.ResearchPaths {
		if !p.From.Resolved || !p.To.Resolved {
			continue
		}
		field := fmt.Sprintf("research_paths[%d]", i)
		if p.From.ID == p.To.ID {
			add(SeverityError, field, p.From.line, "from and to are the same vehicle")
			continue
		}
		cost, ok, err := apiEdge(ctx, cat, p.From.ID, p.To.ID)
		if err != nil {
			return nil, coverage, err
		}
		if ok {
			add(SeverityWarning, field, p.From.line,
				"the API already supplies %s -> %s (%d XP); this entry is redundant", p.From.Name, p.To.Name, cost)
		}
	}

	garage, err := cat.Garage(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		add(SeverityWarning, "", 0, "no garage has been synced, so stale entries were not checked (run: wotctx sync --only wg)")
		return findings, coverage, nil
	case err != nil:
		return nil, coverage, err
	}
	coverage.GarageChecked = true

	owned := make(map[int]bool, len(garage.Identified))
	for _, id := range garage.Identified {
		owned[id] = true
	}
	mark := func(ref *TankRef) {
		ref.Owned = ref.Resolved && owned[ref.ID]
	}

	for i := range o.ResearchedNotBought {
		ref := &o.ResearchedNotBought[i]
		mark(ref)
		if ref.Owned {
			add(SeverityWarning, fmt.Sprintf("researched_not_bought[%d]", i), ref.line,
				"stale: %s is already in the garage", ref.Name)
		}
	}
	for i := range o.XPGoals {
		g := &o.XPGoals[i]
		mark(&g.Target)
		mark(&g.Via)
		if g.Target.Owned {
			add(SeverityWarning, fmt.Sprintf("xp_goals[%d].target", i), g.Target.line,
				"stale: %s is already in the garage, so the goal looks complete", g.Target.Name)
		}
	}
	for i := range o.ResearchPaths {
		mark(&o.ResearchPaths[i].From)
		mark(&o.ResearchPaths[i].To)
	}
	return findings, coverage, nil
}

// checkGoalEdge compares a goal's stated cost with the tech tree's.
func (o *Overlay) checkGoalEdge(ctx context.Context, cat Catalog, i int,
	add func(Severity, string, int, string, ...any)) error {
	g := &o.XPGoals[i]
	if !g.Target.Resolved || !g.Via.Resolved {
		return nil
	}
	field := fmt.Sprintf("xp_goals[%d]", i)

	cost, ok, err := apiEdge(ctx, cat, g.Via.ID, g.Target.ID)
	if err != nil {
		return err
	}
	if !ok {
		add(SeverityWarning, field+".via", g.Via.line,
			"the tech tree has no research step from %s to %s", g.Via.Name, g.Target.Name)
		return nil
	}
	g.APIXPCost = &cost
	if g.XPRequired != nil && *g.XPRequired != cost {
		add(SeverityWarning, field+".xp_required", 0,
			"%d, but the tech tree says %s -> %s costs %d", *g.XPRequired, g.Via.Name, g.Target.Name, cost)
	}
	return nil
}

// apiEdge looks up a tech-tree edge from the API, ignoring the overlay's own
// edges - comparing the overlay against itself would prove nothing.
func apiEdge(ctx context.Context, cat Catalog, from, to int) (int, bool, error) {
	edges, err := cat.NextVehicles(ctx, from)
	if err != nil {
		return 0, false, err
	}
	for _, e := range edges {
		if e.To == to && e.Source != wg.EdgeSourceOverlay {
			return e.XPCost, true, nil
		}
	}
	return 0, false, nil
}

// resolveRef fills in ref from the catalog. It returns a message for a
// reference that does not resolve, and an error only for a catalog failure.
func resolveRef(ctx context.Context, cat Catalog, ref *TankRef) (string, error) {
	var (
		v   store.Vehicle
		err error
	)
	if ref.numeric {
		v, err = cat.VehicleByID(ctx, ref.ID)
	} else {
		v, err = cat.VehicleByName(ctx, ref.Input)
	}

	var ambiguous *store.AmbiguousNameError
	switch {
	case err == nil:
		ref.Resolved = true
		ref.ID = v.TankID
		ref.Name = v.Name
		ref.Tier = v.Tier
		ref.Type = v.Type
		return "", nil

	case errors.As(err, &ambiguous):
		return fmt.Sprintf("%s; use the tank_id instead", ambiguous.Error()), nil

	case !errors.Is(err, store.ErrNotFound):
		return "", err

	case ref.numeric:
		msg := fmt.Sprintf("tank_id %d is not in the vehicle list", ref.ID)
		// Some vehicles have purely numeric names, such as the Chinese "121".
		if named, err := cat.VehicleByName(ctx, ref.Input); err == nil {
			msg += fmt.Sprintf("; to mean the vehicle named %q (tank_id %d), quote it", named.Name, named.TankID)
		}
		return msg, nil
	}

	suggestions, err := cat.SuggestVehicleNames(ctx, ref.Input, 3)
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("unknown tank %q", ref.Input)
	if len(suggestions) > 0 {
		msg += "; did you mean " + quoteJoin(suggestions) + "?"
	}
	return msg, nil
}

func quoteJoin(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}

// Result is a complete validation.
type Result struct {
	Path     string    `json:"path"`
	Overlay  *Overlay  `json:"-"`
	Findings []Finding `json:"findings"`
	Coverage Coverage  `json:"coverage"`
}

// Count returns how many findings have the given severity.
func (r Result) Count(sev Severity) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == sev {
			n++
		}
	}
	return n
}

// Valid reports whether the overlay parsed and has no errors. Warnings,
// including stale entries, do not make it invalid.
func (r Result) Valid() bool {
	return r.Overlay != nil && r.Count(SeverityError) == 0
}

// Validate loads the overlay at path and runs both passes. A missing file is
// returned as an error wrapping os.ErrNotExist. cat may be nil, in which case
// names are not checked and the result says so.
func Validate(ctx context.Context, path string, cat Catalog) (Result, error) {
	result := Result{Path: path}

	o, findings, err := Load(path)
	if err != nil {
		return result, err
	}
	result.Overlay = o
	result.Findings = findings
	if o == nil {
		return result, nil
	}

	more, coverage, err := o.Resolve(ctx, cat)
	if err != nil {
		return result, err
	}
	result.Findings = append(result.Findings, more...)
	result.Coverage = coverage
	return result, nil
}

// Edges returns the research paths that resolved, as overlay-sourced edges for
// the vehicle_edges table.
func (o *Overlay) Edges() []store.Edge {
	var edges []store.Edge
	for _, p := range o.ResearchPaths {
		if p.From.Resolved && p.To.Resolved && p.XPCost > 0 && p.From.ID != p.To.ID {
			edges = append(edges, store.Edge{
				From: p.From.ID, To: p.To.ID, XPCost: p.XPCost, Source: wg.EdgeSourceOverlay,
			})
		}
	}
	return edges
}
