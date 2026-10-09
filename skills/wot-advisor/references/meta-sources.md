# Meta sources: fetched at question time, never synced

Server-wide performance, credits, Onslaught, MoE thresholds, patches and vehicle
characteristics are about the server, not the account. `wotctx meta …` fetches them when a
question needs them; **quote each figure's own date** (the source's `observed_at` in
`meta.sources`) and keep it apart from the player's figures (core §2.1). No date, no figure.

Checked 2026-10-09:

| Need | Command (MCP tool) | Source | Notes |
|---|---|---|---|
| How a tank does across the server; which tanks are strongest | `meta perf` (`wot_server_perf`) | tomato.gg tank performance, 7/30/60 days | Refreshed daily. `win_rate_differential` separates the tank from its players |
| Credits per tank | `meta econ` (`wot_economics`) | tomato.gg economics | Not split by server. The 30-day "all accounts" page refreshes; others have stalled, and a caveat gives their age |
| Onslaught | `meta onslaught` (`wot_onslaught`) | tomato.gg Onslaught | Current season. Wargaming has no Onslaught statistics at all |
| MoE thresholds | `meta moe` (`wot_moe`) | tomato.gg MoE table | Identical to Wargaming's own `tanks/mastery` figures (2026-10-09) |
| Patches, events, what changed | `meta news` (`wot_news`) | Wargaming's RSS feed | Latest 20 articles. For an update's contents, WebFetch its link |
| Stock vs top configuration; module research | `meta profile` (`wot_profile`) | Wargaming encyclopedia | Nominal characteristics |
| Update notes in detail | WebFetch the `meta news` item's link | worldoftanks.eu | Dated articles |
| Premium shop offers | — | <https://eu.wargaming.net/shop/wot/> is an app shell | Cannot be read; ask the player, or use news articles announcing sales |
| Bond shop stock | — | in the game client only | Ask the player |
| Tank characteristics beyond `meta profile` | — | tanks.gg is an app shell | Do not rely on it |
| WN8 expected values, XVM scales | synced (`xvm:wn8exp`, `xvm:scales`) | XVM | Never fetch; use `wotctx` |

Not used: WOTInspector's replay API (its terms allow only personal use and forbid showing its
content elsewhere without written consent), tomato.gg's paid API (spec §3.2).

## Rules

- Quote the source and its date: "tomato.gg tank performance, 30 days, updated 9 Oct 2026".
- Server-wide figures are **server-wide**: label them apart from the player's stats and from
  opinion (framework §2.1).
- A page that fails, is stale (a caveat gives its age) or lacks the row needed is reported as
  such. Never fill the gap from general knowledge presented as data.
- When an update is newer than the player's recent stats, check its notes for changes to the
  tanks in question before comparing old and new performance (framework §2.3).
- A meta command fails loudly when a page's format has changed. Say the source is unavailable;
  do not guess its figures.
