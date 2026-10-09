package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/query"
)

// sharedFetcher is one per process, so its cache spans an MCP session: a
// conversation about several tanks downloads each page once.
var sharedFetcher = &meta.Fetcher{}

var sharedTomato = &meta.Tomato{Fetch: sharedFetcher}

// tomatoClient, metaFetcher and newsURL are replaced in tests so they never
// touch the network.
var (
	tomatoClient = func() *meta.Tomato { return sharedTomato }
	metaFetcher  = func() *meta.Fetcher { return sharedFetcher }
	newsURL      = meta.NewsURL
)

// runMetaMoE fetches tomato.gg's MoE table and reports one vehicle's
// thresholds. The page is fetched at question time and never stored: it is
// server-wide data on Wargaming's schedule, honest only with its own date.
func runMetaMoE(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "meta moe", args, func(flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			if len(rest) == 0 {
				return query.Envelope{}, usageErr(env, "meta moe needs a tank name or tank_id")
			}
			return env.moe(ctx, s, strings.Join(rest, " "))
		}
	})
}

// runMetaPerf reports how a tank does across the server, beside the player's
// own figures on it, or ranks a tier's, a class's or the garage's tanks.
func runMetaPerf(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "meta perf", args, func(fs flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		days := fs.Int("days", 30, "window: 7, 30 or 60 days")
		tier := fs.Int("tier", 0, "rank this tier's tanks")
		class := fs.String("class", "", "only this class (LT, MT, HT, TD, SPG)")
		owned := fs.Bool("owned", false, "rank the garage's tanks")
		minBattles := fs.Int("min-battles", defaultMinServerBattles, "leave out tanks with fewer server battles in the window")
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			in := serverPerfInput{Tank: strings.Join(rest, " "), Days: *days, Tier: *tier, Class: *class, Owned: *owned, MinBattles: *minBattles}
			out, err := env.serverPerf(ctx, s, in)
			if errors.Is(err, errMetaArgs) {
				return out, usageErr(env, err.Error())
			}
			return out, err
		}
	})
}

// defaultMinServerBattles keeps a ranked list to tanks with enough battles
// that a win rate means something across the server.
const defaultMinServerBattles = 1000

// serverPerfInput is `meta perf`'s arguments, shared with wot_server_perf.
type serverPerfInput struct {
	Tank       string `json:"tank,omitempty" jsonschema:"vehicle name or tank_id; omit to rank a tier or the garage instead"`
	Days       int    `json:"days,omitempty" jsonschema:"window: 7, 30 (default) or 60 days"`
	Tier       int    `json:"tier,omitempty" jsonschema:"without a tank: rank this tier's tanks"`
	Class      string `json:"class,omitempty" jsonschema:"without a tank: only this class (LT, MT, HT, TD, SPG)"`
	Owned      bool   `json:"owned,omitempty" jsonschema:"without a tank: rank the player's garage"`
	MinBattles int    `json:"min_battles,omitempty" jsonschema:"without a tank: leave out tanks with fewer server battles in the window (default 1000)"`
}

// errMetaArgs marks an argument error in a meta command, which the CLI
// reports as usage.
var errMetaArgs = errors.New("meta")

// serverPerf validates the input, fetches the table unless tomato.gg is
// switched off, and answers; shared with the MCP server.
func (e *Env) serverPerf(ctx context.Context, s *query.Service, in serverPerfInput) (query.Envelope, error) {
	if in.Days == 0 {
		in.Days = 30
	}
	if in.MinBattles == 0 {
		in.MinBattles = defaultMinServerBattles
	}
	ref := strings.TrimSpace(in.Tank)
	if !slices.Contains(meta.PerfWindows, in.Days) {
		return query.Envelope{}, fmt.Errorf("%w: --days %d: want 7, 30 or 60", errMetaArgs, in.Days)
	}
	f, err := garageFilter(in.Tier, in.Class)
	if err != nil {
		return query.Envelope{}, fmt.Errorf("%w: %v", errMetaArgs, err)
	}
	if ref == "" && in.Tier == 0 && !in.Owned {
		return query.Envelope{}, fmt.Errorf("%w: name a tank, or rank a tier (--tier) or the garage (--owned)", errMetaArgs)
	}
	if ref != "" && (in.Tier != 0 || in.Class != "" || in.Owned) {
		return query.Envelope{}, fmt.Errorf("%w: a tank name and --tier/--class/--owned do not combine", errMetaArgs)
	}

	if !e.Config.TomatoFetchEnabled() {
		if ref == "" {
			return query.Envelope{}, errors.New("ranking needs tomato.gg's table, and fetching it is switched off (meta.tomato_fetch in config.yaml)")
		}
		return s.ServerPerf(ctx, ref, nil, "meta.tomato_fetch")
	}
	table, err := tomatoClient().Performance(ctx, meta.Server(e.Config.Account.Realm), in.Days)
	if err != nil {
		return query.Envelope{}, err
	}
	if ref != "" {
		return s.ServerPerf(ctx, ref, &table, "")
	}
	return s.ServerPerfRanked(ctx, query.ServerPerfFilter{Tier: f.Tier, Class: f.Class, Owned: in.Owned, MinBattles: in.MinBattles}, &table)
}

// runMetaEcon reports a tank's credit figures, or ranks a tier's or the
// garage's tanks by profit.
func runMetaEcon(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "meta econ", args, func(fs flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		days := fs.Int("days", 30, "window: 14, 30 or 60 days")
		account := fs.String("account", "all", "battles on all, premium or standard accounts")
		tier := fs.Int("tier", 0, "rank this tier's tanks")
		class := fs.String("class", "", "only this class (LT, MT, HT, TD, SPG)")
		owned := fs.Bool("owned", false, "rank the garage's tanks")
		premium := fs.Bool("premium-tanks", false, "only premium vehicles")
		perMinute := fs.Bool("per-minute", false, "rank by profit per minute instead of per battle")
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			in := econInput{Tank: strings.Join(rest, " "), Days: *days, Account: *account, Tier: *tier, Class: *class,
				Owned: *owned, PremiumTanks: *premium, PerMinute: *perMinute}
			out, err := env.econ(ctx, s, in)
			if errors.Is(err, errMetaArgs) {
				return out, usageErr(env, err.Error())
			}
			return out, err
		}
	})
}

// econInput is `meta econ`'s arguments, shared with wot_economics.
type econInput struct {
	Tank         string `json:"tank,omitempty" jsonschema:"vehicle name or tank_id; omit to rank a tier or the garage instead"`
	Days         int    `json:"days,omitempty" jsonschema:"window: 14, 30 (default) or 60 days"`
	Account      string `json:"account,omitempty" jsonschema:"battles on all (default), premium or standard accounts; match the player's own account type"`
	Tier         int    `json:"tier,omitempty" jsonschema:"without a tank: rank this tier's tanks"`
	Class        string `json:"class,omitempty" jsonschema:"without a tank: only this class (LT, MT, HT, TD, SPG)"`
	Owned        bool   `json:"owned,omitempty" jsonschema:"without a tank: rank the player's garage"`
	PremiumTanks bool   `json:"premium_tanks,omitempty" jsonschema:"without a tank: only premium vehicles"`
	PerMinute    bool   `json:"per_minute,omitempty" jsonschema:"without a tank: rank by profit per minute instead of per battle"`
}

// econ validates the input, fetches the table and answers; shared with the
// MCP server.
func (e *Env) econ(ctx context.Context, s *query.Service, in econInput) (query.Envelope, error) {
	if in.Days == 0 {
		in.Days = 30
	}
	if in.Account == "" {
		in.Account = "all"
	}
	ref := strings.TrimSpace(in.Tank)
	if !slices.Contains(meta.EconWindows, in.Days) {
		return query.Envelope{}, fmt.Errorf("%w: --days %d: want 14, 30 or 60", errMetaArgs, in.Days)
	}
	if !slices.Contains(meta.EconAccounts, in.Account) {
		return query.Envelope{}, fmt.Errorf("%w: --account %q: want all, premium or standard", errMetaArgs, in.Account)
	}
	f, err := garageFilter(in.Tier, in.Class)
	if err != nil {
		return query.Envelope{}, fmt.Errorf("%w: %v", errMetaArgs, err)
	}
	listing := in.Tier != 0 || in.Class != "" || in.Owned || in.PremiumTanks || in.PerMinute
	if ref == "" && in.Tier == 0 && !in.Owned {
		return query.Envelope{}, fmt.Errorf("%w: name a tank, or rank a tier (--tier) or the garage (--owned)", errMetaArgs)
	}
	if ref != "" && listing {
		return query.Envelope{}, fmt.Errorf("%w: a tank name and the list options do not combine", errMetaArgs)
	}
	if !e.Config.TomatoFetchEnabled() {
		return query.Envelope{}, errors.New("credit figures come only from tomato.gg, and fetching it is switched off (meta.tomato_fetch in config.yaml)")
	}
	table, err := tomatoClient().Economics(ctx, in.Account, in.Days)
	if err != nil {
		return query.Envelope{}, err
	}
	if ref != "" {
		return s.Econ(ctx, ref, &table)
	}
	return s.EconRanked(ctx, query.EconFilter{Tier: f.Tier, Class: f.Class, Owned: in.Owned,
		PremiumTanks: in.PremiumTanks, PerMinute: in.PerMinute}, &table)
}

// runMetaOnslaught reports a tank's Onslaught season, or ranks the season's
// tanks, with the rating distribution.
func runMetaOnslaught(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "meta onslaught", args, func(fs flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		class := fs.String("class", "", "only this class (LT, MT, HT, TD, SPG)")
		owned := fs.Bool("owned", false, "only the garage's tanks")
		minBattles := fs.Int("min-battles", defaultMinServerBattles, "leave out tanks with fewer Onslaught battles this season")
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			in := onslaughtInput{Tank: strings.Join(rest, " "), Class: *class, Owned: *owned, MinBattles: *minBattles}
			out, err := env.onslaught(ctx, s, in)
			if errors.Is(err, errMetaArgs) {
				return out, usageErr(env, err.Error())
			}
			return out, err
		}
	})
}

// onslaughtInput is `meta onslaught`'s arguments, shared with wot_onslaught.
type onslaughtInput struct {
	Tank       string `json:"tank,omitempty" jsonschema:"vehicle name or tank_id; omit to rank the season's tanks"`
	Class      string `json:"class,omitempty" jsonschema:"without a tank: only this class (LT, MT, HT, TD, SPG)"`
	Owned      bool   `json:"owned,omitempty" jsonschema:"without a tank: only the player's garage"`
	MinBattles int    `json:"min_battles,omitempty" jsonschema:"without a tank: leave out tanks with fewer Onslaught battles this season (default 1000)"`
}

// onslaught validates the input, fetches the season and answers; shared
// with the MCP server.
func (e *Env) onslaught(ctx context.Context, s *query.Service, in onslaughtInput) (query.Envelope, error) {
	if in.MinBattles == 0 {
		in.MinBattles = defaultMinServerBattles
	}
	ref := strings.TrimSpace(in.Tank)
	f, err := garageFilter(0, in.Class)
	if err != nil {
		return query.Envelope{}, fmt.Errorf("%w: %v", errMetaArgs, err)
	}
	if ref != "" && (in.Class != "" || in.Owned) {
		return query.Envelope{}, fmt.Errorf("%w: a tank name and --class/--owned do not combine", errMetaArgs)
	}
	if !e.Config.TomatoFetchEnabled() {
		return query.Envelope{}, errors.New("Onslaught figures come only from tomato.gg, and fetching it is switched off (meta.tomato_fetch in config.yaml)")
	}
	table, err := tomatoClient().Onslaught(ctx, meta.Server(e.Config.Account.Realm))
	if err != nil {
		return query.Envelope{}, err
	}
	return s.Onslaught(ctx, ref, query.OnslaughtFilter{Class: f.Class, Owned: in.Owned, MinBattles: in.MinBattles}, &table)
}

// runMetaNews lists Wargaming's latest news, or only its updates.
func runMetaNews(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "meta news", args, func(fs flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		since := fs.String("since", "", "only articles published on or after this date (YYYY-MM-DD)")
		updates := fs.Bool("updates", false, "only game updates, preloads and micropatches")
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			if len(rest) > 0 {
				return query.Envelope{}, usageErr(env, "meta news takes no arguments")
			}
			out, err := env.news(ctx, s, newsInput{Since: *since, Updates: *updates})
			if errors.Is(err, errMetaArgs) {
				return out, usageErr(env, err.Error())
			}
			return out, err
		}
	})
}

// newsInput is `meta news`'s arguments, shared with wot_news.
type newsInput struct {
	Since   string `json:"since,omitempty" jsonschema:"only articles published on or after this date (YYYY-MM-DD)"`
	Updates bool   `json:"updates,omitempty" jsonschema:"only game updates, preloads and micropatches"`
}

// news fetches the realm's feed and answers; shared with the MCP server.
func (e *Env) news(ctx context.Context, s *query.Service, in newsInput) (query.Envelope, error) {
	var since time.Time
	if in.Since != "" {
		t, err := time.Parse("2006-01-02", in.Since)
		if err != nil {
			return query.Envelope{}, fmt.Errorf("%w: --since %q: want YYYY-MM-DD", errMetaArgs, in.Since)
		}
		since = t
	}
	feed, err := metaFetcher().News(ctx, newsURL(e.Config.Account.Realm))
	if err != nil {
		return query.Envelope{}, err
	}
	return s.News(ctx, &feed, since, in.Updates)
}

// profileSource is the Wargaming client `meta profile` asks; replaced in tests.
var profileSource = func(e *Env) (meta.ProfileSource, error) { return e.wgClient() }

// runMetaProfile reports a vehicle's stock and top configurations.
func runMetaProfile(ctx context.Context, env *Env, args []string) error {
	return runQuery(ctx, env, "meta profile", args, func(flagSet) func(context.Context, *query.Service, []string) (query.Envelope, error) {
		return func(ctx context.Context, s *query.Service, rest []string) (query.Envelope, error) {
			if len(rest) == 0 {
				return query.Envelope{}, usageErr(env, "meta profile needs a tank name or tank_id")
			}
			return env.profile(ctx, s, strings.Join(rest, " "))
		}
	})
}

// profile fetches the configurations from Wargaming and answers; shared
// with the MCP server.
func (e *Env) profile(ctx context.Context, s *query.Service, ref string) (query.Envelope, error) {
	src, err := profileSource(e)
	if err != nil {
		return query.Envelope{}, err
	}
	return s.Profile(ctx, ref, func(tankID int) (meta.Configs, error) {
		return meta.Configurations(ctx, src, tankID)
	})
}

// moe fetches the table and answers for one vehicle; shared with the MCP server.
// With the fetch switched off (meta.tomato_fetch or meta.moe_fetch), nothing
// is fetched and the answer carries the player's own marks without thresholds.
func (e *Env) moe(ctx context.Context, s *query.Service, ref string) (query.Envelope, error) {
	if !e.Config.MoEFetchEnabled() {
		return s.MoE(ctx, ref, nil)
	}
	table, err := tomatoClient().MoE(ctx, meta.Server(e.Config.Account.Realm))
	if err != nil {
		return query.Envelope{}, err
	}
	return s.MoE(ctx, ref, &table)
}
