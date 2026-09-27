package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/overlay"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

func runOverlayPath(_ context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "overlay path")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if env.ConfigErr != nil {
		return env.ConfigErr
	}

	path := env.Config.OverlayFile(env.Paths)
	// stdout carries only the path, so the command composes with other tools.
	fmt.Fprintln(env.Stdout, path)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(env.Stderr, "(does not exist yet; set overlay_path in "+env.Paths.ConfigFile+" to use a versioned copy)")
	}
	return nil
}

func runOverlayValidate(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "overlay validate")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if env.ConfigErr != nil {
		return env.ConfigErr
	}

	result, err := validateOverlay(ctx, env)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no overlay at %s; create it (schema: docs/spec.md section 5) or set overlay_path in %s",
			result.Path, env.Paths.ConfigFile)
	}
	if err != nil {
		return err
	}

	printOverlay(env.Stdout, result)
	printOverlayBackup(ctx, env)
	if !result.Valid() {
		return fmt.Errorf("overlay is invalid: %d error(s)", result.Count(overlay.SeverityError))
	}
	return nil
}

// validateOverlay checks the configured overlay against the cache, if there is
// one. It never creates the cache: validating a file must not have side
// effects on a machine that has not synced yet.
func validateOverlay(ctx context.Context, env *Env) (overlay.Result, error) {
	path := env.Config.OverlayFile(env.Paths)

	var cat overlay.Catalog
	if env.HasStore() {
		db, err := env.OpenStore(ctx)
		if err != nil {
			return overlay.Result{Path: path}, err
		}
		defer db.Close()
		cat = db
	}
	return overlay.Validate(ctx, path, cat)
}

// printOverlay shows what the overlay resolved to, so a person can see that
// "Obj 277" became Object 277 rather than trusting that it did.
func printOverlay(w io.Writer, r overlay.Result) {
	fmt.Fprintf(w, "overlay: %s\n", r.Path)

	if o := r.Overlay; o != nil {
		if !o.UpdatedAt.IsZero() {
			fmt.Fprintf(w, "updated: %s\n", o.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
		}
		fmt.Fprintf(w, "premium: %s\n", premiumSummary(o.Premium))

		fmt.Fprintf(w, "researched, not bought: %d\n", len(o.ResearchedNotBought))
		for _, ref := range o.ResearchedNotBought {
			fmt.Fprintf(w, "  %s\n", describeRef(ref))
		}

		fmt.Fprintf(w, "xp goals: %d\n", len(o.XPGoals))
		for _, g := range o.XPGoals {
			line := "  " + describeRef(g.Target)
			if !g.Via.IsZero() {
				line += " via " + g.Via.String()
			}
			line += "; required " + optionalXP(g.XPRequired)
			if g.APIXPCost != nil {
				line += fmt.Sprintf(" (tech tree: %d)", *g.APIXPCost)
			}
			line += "; banked " + optionalXP(g.XPBanked)
			fmt.Fprintln(w, line)
		}

		if len(o.ResearchPaths) > 0 {
			fmt.Fprintf(w, "research paths: %d\n", len(o.ResearchPaths))
			for _, p := range o.ResearchPaths {
				fmt.Fprintf(w, "  %s -> %s: %d XP\n", p.From.String(), p.To.String(), p.XPCost)
			}
		}
		if len(o.Preferences.AvoidClasses) > 0 {
			fmt.Fprintf(w, "avoid classes: %s\n", strings.Join(o.Preferences.AvoidClasses, ", "))
		}
	}

	if len(r.Findings) > 0 {
		fmt.Fprintln(w)
		for _, f := range r.Findings {
			fmt.Fprintln(w, f.String())
		}
	}

	fmt.Fprintln(w)
	errs, warns := r.Count(overlay.SeverityError), r.Count(overlay.SeverityWarning)
	if r.Valid() {
		fmt.Fprintf(w, "valid, %d warning(s)\n", warns)
	} else {
		fmt.Fprintf(w, "invalid: %d error(s), %d warning(s)\n", errs, warns)
	}
}

func describeRef(ref overlay.TankRef) string {
	if !ref.Resolved {
		return ref.Input + " (unresolved)"
	}
	s := fmt.Sprintf("%s (tank_id %d, tier %d %s)", ref.Name, ref.ID, ref.Tier, ref.Type)
	if !strings.EqualFold(ref.Input, ref.Name) {
		s = fmt.Sprintf("%s -> %s", ref.Input, s)
	}
	if ref.Owned {
		s += " [in garage]"
	}
	return s
}

func optionalXP(v *int) string {
	if v == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *v)
}

func premiumSummary(p *overlay.Premium) string {
	if p == nil {
		return "unknown"
	}
	yesNo := func(b *bool) string {
		switch {
		case b == nil:
			return "unknown"
		case *b:
			return "yes"
		default:
			return "no"
		}
	}
	return fmt.Sprintf("Premium Account %s, WoT Plus %s", yesNo(p.PremiumAccount), yesNo(p.WoTPlus))
}

// checkOverlay is doctor's view of the overlay. Every problem warns rather than
// fails: a broken overlay degrades answers - premium status becomes unknown,
// goals disappear - but it does not stop the tool working.
func checkOverlay(ctx context.Context, env *Env) check {
	const name = "overlay"

	if env.ConfigErr != nil {
		return check{Name: name, Status: statusUnknown, Detail: "config unresolved"}
	}

	result, err := validateOverlay(ctx, env)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return check{
			Name:   name,
			Status: statusWarn,
			Detail: "no overlay at " + result.Path + "; premium status is unknown and there are no goals",
			Fix:    "create it (schema: docs/spec.md section 5) or set overlay_path in " + env.Paths.ConfigFile,
		}
	case err != nil:
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}

	if !result.Valid() {
		first := ""
		for _, f := range result.Findings {
			if f.Severity == overlay.SeverityError {
				first = f.String()
				break
			}
		}
		return check{
			Name:   name,
			Status: statusWarn,
			Detail: fmt.Sprintf("%s is invalid (%d error(s)), so it is not used; first: %s",
				result.Path, result.Count(overlay.SeverityError), first),
			Fix: "wotctx overlay validate",
		}
	}

	o := result.Overlay
	stale := 0
	for _, ref := range o.ResearchedNotBought {
		if ref.Owned {
			stale++
		}
	}
	for _, g := range o.XPGoals {
		if g.Target.Owned {
			stale++
		}
	}

	detail := fmt.Sprintf("updated %s; premium: %s; %d researched-not-bought, %d goal(s)",
		o.UpdatedAt.Format("2006-01-02"), premiumSummary(o.Premium),
		len(o.ResearchedNotBought), len(o.XPGoals))
	if stale > 0 {
		detail += fmt.Sprintf("; %d stale (already in the garage)", stale)
	}
	if !result.Coverage.NamesChecked {
		detail += "; names not checked (no vehicle list)"
	} else if !result.Coverage.GarageChecked {
		detail += "; staleness not checked (no garage)"
	}

	c := check{Name: name, Status: statusOK, Detail: detail}
	if warnings := result.Count(overlay.SeverityWarning); warnings > 0 {
		c.Status = statusWarn
		c.Detail += fmt.Sprintf("; %d warning(s)", warnings)
		c.Fix = "wotctx overlay validate"
	}
	return c
}

// loadOverlayEdges replaces the overlay-sourced tech-tree edges with the
// overlay's current research_paths (docs/spec.md section 5). It runs as part
// of sync because it writes to the cache, which `overlay validate` must not.
//
// It returns a caveat for the sync report, or "" when there is nothing to say.
func loadOverlayEdges(ctx context.Context, env *Env, db *store.DB) (string, error) {
	path := env.Config.OverlayFile(env.Paths)

	result, err := overlay.Validate(ctx, path, db)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// No overlay, so no overlay edges - including any left by an earlier one.
		_, err := db.ReplaceEdgesFromSource(ctx, wg.EdgeSourceOverlay, nil)
		return "", err
	case err != nil:
		return "", err
	}

	if !result.Valid() {
		// The edges already loaded came from an overlay that was valid, so they
		// are kept rather than cleared on a typo.
		return "overlay: invalid, research_paths not reloaded (run: wotctx overlay validate)", nil
	}
	if !result.Coverage.NamesChecked {
		return "", nil
	}
	_, err = db.ReplaceEdgesFromSource(ctx, wg.EdgeSourceOverlay, result.Overlay.Edges())
	return "", err
}

// printOverlayBackup says which overlay fields the client mod's dump has
// taken over. They stay in the file as the backup for when there is no dump,
// and are ignored while there is one.
func printOverlayBackup(ctx context.Context, env *Env) {
	if !env.HasStore() {
		return
	}
	db, err := env.OpenStore(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	acct, err := db.LatestModAccount(ctx)
	if err != nil {
		return
	}
	fields := "premium, xp_banked"
	if _, ok, err := db.ModUnlocked(ctx, acct.SnapshotID); err == nil && ok {
		fields += ", researched_not_bought"
	}
	fmt.Fprintf(env.Stdout, "\nbackup only: the client mod's dump of %s supplies what the overlay's %s say, "+
		"so those are used only when there is no dump; preferences, goals and notes are still read from here\n",
		acct.CapturedAt.UTC().Format("2006-01-02 15:04 UTC"), fields)
}
