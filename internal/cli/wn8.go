package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
	"github.com/ondrejkouril/tank-advisor/internal/wn8"
	"github.com/ondrejkouril/tank-advisor/internal/xvm"
)

// staleExpectedAfter is how old XVM's version stamp may get before doctor
// warns. XVM regenerates nightly; a stamp months old means the source has
// stopped updating, which is exactly how the pre-split file failed - it froze
// in September 2024 and went on serving HTTP 200.
const staleExpectedAfter = 60 * 24 * time.Hour

// checkWN8 reports which vintage of expected values is stored and the account
// rating they produce, so the figure can be compared against a public stats
// site.
func checkWN8(ctx context.Context, env *Env) check {
	const name = "wn8"

	if env.ConfigErr != nil {
		return check{Name: name, Status: statusUnknown, Detail: "config unresolved"}
	}
	if !env.HasStore() {
		return check{Name: name, Status: statusWarn, Detail: "no cache yet", Fix: "wotctx sync"}
	}

	db, err := env.OpenStore(ctx)
	if err != nil {
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}
	defer db.Close()

	expected, err := db.LoadWN8Expected(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return check{
			Name:   name,
			Status: statusWarn,
			Detail: "no WN8 expected values synced, so no WN8 can be computed",
			Fix:    "wotctx sync --only wn8",
		}
	case err != nil:
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}

	now := env.now()
	result := check{Name: name, Status: statusOK}
	detail := fmt.Sprintf("XVM expected values %s (%d vehicles, fetched %s)",
		expected.Version, len(expected.ByTank), humanizeAge(int64(now.Sub(expected.SyncedAt).Seconds())))

	if v, err := time.Parse("2006-01-02", expected.Version); err == nil && now.Sub(v) > staleExpectedAfter {
		result.Status = statusWarn
		detail += fmt.Sprintf("; the version is %d days old, so the source may have stopped updating",
			int(now.Sub(v).Hours()/24))
		result.Fix = "check that " + xvm.DefaultURL + " still updates"
	}

	rows, err := db.LatestTankStats(ctx, wg.ModeRandom)
	switch {
	case errors.Is(err, store.ErrNotFound):
		result.Detail = detail + "; no tank statistics synced yet"
		return result
	case err != nil:
		return check{Name: name, Status: statusFail, Detail: env.Redactor().RedactError(err)}
	}

	rating := wn8.Compute(rows, expected.ByTank)
	detail += fmt.Sprintf("; account WN8 %.0f over %d random battles on %d tanks",
		rating.WN8, rating.Battles, len(rating.Tanks))

	if len(rating.Excluded) > 0 {
		byReason := map[string]int{}
		for _, e := range rating.Excluded {
			byReason[e.Reason]++
		}
		detail += fmt.Sprintf("; %d tank(s) unrated (%d battles)", len(rating.Excluded), rating.ExcludedBattles)
		if n := byReason[wn8.ReasonNoExpected]; n > 0 {
			detail += fmt.Sprintf(", %d with %s", n, wn8.ReasonNoExpected)
		}
		if n := byReason[wn8.ReasonOldParser]; n > 0 {
			detail += fmt.Sprintf(", %d whose %s", n, wn8.ReasonOldParser)
			result.Status = statusWarn
			result.Fix = "wotctx sync"
		}
	}
	result.Detail = detail
	return result
}
