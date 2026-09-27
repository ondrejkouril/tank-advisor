package cli

import (
	"fmt"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/overlay"
	"github.com/ondrejkouril/tank-advisor/internal/query"
)

// Argument validation shared by the CLI and the MCP server. Each function takes
// the decoded values and returns the query's input or an error phrased for the
// person (or model) who supplied them, so the two surfaces cannot drift apart.

func garageFilter(tier int, class string) (query.GarageFilter, error) {
	f := query.GarageFilter{Tier: tier}
	if class != "" {
		if f.Class = overlay.CanonicalClass(class); f.Class == "" {
			return f, fmt.Errorf("unknown class %q; want one of %s", class, strings.Join(overlay.Classes, ", "))
		}
	}
	if tier < 0 || tier > 11 {
		return f, fmt.Errorf("tier %d out of range 1-11", tier)
	}
	return f, nil
}

func performanceArgs(by, window string, minTier int) (query.Window, error) {
	w, err := query.ParseWindow(window)
	if err != nil {
		return w, err
	}
	switch by {
	case query.ByClass, query.ByTier, query.ByNation:
	default:
		return w, fmt.Errorf("--by %q: want class, tier or nation", by)
	}
	if minTier < 0 || minTier > 11 {
		return w, fmt.Errorf("--min-tier %d out of range 1-11", minTier)
	}
	return w, nil
}

func sessionsWindow(window string) (query.Window, error) {
	w, err := query.ParseWindow(window)
	if err != nil || w.Lifetime {
		return w, fmt.Errorf("window %q: want a number of days such as 7d", window)
	}
	return w, nil
}

func checkBudget(budget int) error {
	if budget < 0 {
		return fmt.Errorf("--budget-credits must not be negative")
	}
	return nil
}

func checkMissionsArgs(operation string, open bool) error {
	if open && operation == "" {
		return fmt.Errorf("--open needs --operation")
	}
	return nil
}

func checkMaxChars(maxChars int) error {
	if maxChars < 2000 {
		// Below this the sections are cut to their headings, which is a brief
		// in name only.
		return fmt.Errorf("--max-chars %d is too small to be useful; use at least 2000", maxChars)
	}
	return nil
}

// briefTitle names the account at the top of the brief.
func (e *Env) briefTitle() string {
	return fmt.Sprintf("%s (%s)", e.Config.Account.Nickname, strings.ToUpper(e.Config.Account.Realm))
}
