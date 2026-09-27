package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/query"
)

// queryService opens the cache for a query. A query never creates the cache:
// with nothing synced there is nothing to answer, and saying so beats an empty
// database appearing as a side effect.
func (e *Env) queryService(ctx context.Context) (*query.Service, func(), error) {
	if e.ConfigErr != nil {
		return nil, nil, e.ConfigErr
	}
	if !e.HasStore() {
		return nil, nil, fmt.Errorf("no cache yet at %s; run: wotctx sync", e.Paths.DBFile)
	}
	db, err := e.OpenStore(ctx)
	if err != nil {
		return nil, nil, err
	}
	return &query.Service{
		DB:          db,
		Config:      e.Config,
		OverlayPath: e.Config.OverlayFile(e.Paths),
		Now:         e.now,
	}, func() { db.Close() }, nil
}

// printEnvelope writes a query result. Queries always emit JSON (spec 7), and
// compact by default: the reader is usually the skill, for whom indentation is
// only tokens. --pretty is for people.
func printEnvelope(env *Env, result query.Envelope, pretty bool) error {
	enc := json.NewEncoder(env.Stdout)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("encoding result: %w", err)
	}
	return nil
}

// runQuery handles the shared shape: parse flags, open the cache, run, print.
func runQuery(ctx context.Context, env *Env, name string, args []string,
	setup func(fs flagSet) func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error)) error {
	fs := newFlagSet(env, "query "+name)
	pretty := fs.Bool("pretty", false, "indent the JSON for reading")
	run := setup(fs)
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}

	s, closeDB, err := env.queryService(ctx)
	if err != nil {
		return err
	}
	defer closeDB()

	result, err := run(ctx, s, fs.Args())
	if err != nil {
		return err
	}
	return printEnvelope(env, result, *pretty)
}

// flagSet is the subset of *flag.FlagSet the query commands use.
type flagSet interface {
	Int(name string, value int, usage string) *int
	String(name, value, usage string) *string
	Bool(name string, value bool, usage string) *bool
}

func runQueryResources(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "resources", args, func(flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			if len(rest) > 0 {
				return query.Envelope{}, usageErr(env, "query resources takes no arguments")
			}
			return s.Resources(ctx)
		}
	})
}

func runQueryGarage(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "garage", args, func(fs flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		tier := fs.Int("tier", 0, "only vehicles of this tier")
		class := fs.String("class", "", "only vehicles of this class (lightTank, MT, TD, ...)")
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			if len(rest) > 0 {
				return query.Envelope{}, usageErr(env, "query garage takes no arguments")
			}
			f, err := garageFilter(*tier, *class)
			if err != nil {
				return query.Envelope{}, usageErr(env, err.Error())
			}
			return s.Garage(ctx, f)
		}
	})
}

func runQueryTank(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "tank", args, func(flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			if len(rest) == 0 {
				return query.Envelope{}, usageErr(env, "query tank needs a tank name or tank_id")
			}
			// Names contain spaces; accept them unquoted as well as quoted.
			return s.Tank(ctx, strings.Join(rest, " "))
		}
	})
}

func runQueryPerformance(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "performance", args, func(fs flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		by := fs.String("by", query.ByClass, "group by class, tier or nation")
		window := fs.String("window", "lifetime", "lifetime, or a number of days such as 30d")
		minTier := fs.Int("min-tier", 0, "leave out tanks below this tier (8 compares current play)")
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			if len(rest) > 0 {
				return query.Envelope{}, usageErr(env, "query performance takes no arguments")
			}
			w, err := performanceArgs(*by, *window, *minTier)
			if err != nil {
				return query.Envelope{}, usageErr(env, err.Error())
			}
			return s.Performance(ctx, *by, w, *minTier)
		}
	})
}

func runQuerySessions(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "sessions", args, func(fs flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		window := fs.String("window", "7d", "a number of days such as 7d")
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			if len(rest) > 0 {
				return query.Envelope{}, usageErr(env, "query sessions takes no arguments")
			}
			w, err := sessionsWindow(*window)
			if err != nil {
				return query.Envelope{}, usageErr(env, err.Error())
			}
			return s.Sessions(ctx, w)
		}
	})
}

func runQueryCandidates(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "candidates", args, func(fs flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		budget := fs.Int("budget-credits", 0, "judge affordability against this many credits instead of the account's")
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			if len(rest) > 0 {
				return query.Envelope{}, usageErr(env, "query candidates takes no arguments")
			}
			if err := checkBudget(*budget); err != nil {
				return query.Envelope{}, usageErr(env, err.Error())
			}
			return s.Candidates(ctx, *budget)
		}
	})
}

// usageErr writes a usage message and returns ErrUsage.
func usageErr(env *Env, msg string) error {
	fmt.Fprintln(env.Stderr, msg)
	return ErrUsage
}

func runQueryMissions(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "missions", args, func(fs flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		operation := fs.String("operation", "", "list every mission of one operation, by id or name (e.g. \"Object 260\")")
		open := fs.Bool("open", false, "with --operation, list only the missions not yet done")
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			if len(rest) > 0 {
				return query.Envelope{}, usageErr(env, "query missions takes no arguments; use --operation")
			}
			if err := checkMissionsArgs(*operation, *open); err != nil {
				return query.Envelope{}, usageErr(env, err.Error())
			}
			return s.Missions(ctx, *operation, *open)
		}
	})
}
