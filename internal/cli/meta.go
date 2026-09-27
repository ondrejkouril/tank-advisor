package cli

import (
	"context"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/query"
)

// moeClient is replaced in tests so they never touch the network.
var moeClient = func() *meta.MoEClient { return &meta.MoEClient{} }

// runMetaMoE fetches tomato.gg's MoE table and reports one vehicle's
// thresholds. The page is fetched on every call and never stored: it is
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

// moe fetches the table and answers for one vehicle; shared with the MCP server.
// With meta.moe_fetch off, nothing is fetched and the answer carries the
// player's own marks without thresholds.
func (e *Env) moe(ctx context.Context, s *query.Service, ref string) (query.Envelope, error) {
	if !e.Config.MoEFetchEnabled() {
		return s.MoE(ctx, ref, nil)
	}
	table, err := moeClient().FetchMoE(ctx, meta.Server(e.Config.Account.Realm))
	if err != nil {
		return query.Envelope{}, err
	}
	return s.MoE(ctx, ref, &table)
}
