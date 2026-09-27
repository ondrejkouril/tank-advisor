# Metrics

What each figure in `wotctx` output means, where it comes from, and how far to trust it. All
player figures are **random battles** unless a field says otherwise.

## Per-battle figures (every stats line)

| Field | Meaning | Notes |
|---|---|---|
| `battles` | random battles in the scope | always quote it beside any other figure |
| `confidence` | `very_low` < 30, `low` < 100, `moderate` < 300, `ok` ≥ 300 battles | gates what may be claimed (framework §2.2) |
| `win_rate` | wins ÷ battles, percent | swings widely on small samples |
| `dpg` | damage dealt ÷ battles | the core output measure |
| `frags`, `spots` | per battle | |
| `survival_rate` | battles survived ÷ battles, percent | key for light tanks |
| `avg_xp` | base battle XP ÷ battles | the rate used to cost a grind in battles |
| `assist` | radio + track + stun assist ÷ battles | **random only**, from the API's totals; see below |
| `assist_radio`, `assist_track`, `assist_stun` | the split | the game client never shows this split |
| `wn8` | battle-aggregate WN8, or null | null when no tank in scope has expected values |
| `wn8_excluded_battles` | battles on tanks WN8 could not rate | present when non-zero |

## WN8

The community's standard rating: damage, frags, spots, defence and wins, each divided by the
**expected** value for that tank, clamped, and combined (2026 formula in spec §6.2). 1,565 is
exactly "average for the tanks played"; above ~2,000 is very good.

- **Expected values** come from XVM (`xvm:wn8exp`, the Wargaming-server file); `meta.sources`
  carries their `version` date. Ratings are only comparable within one version.
- **Account or rollup WN8 is computed on the battle aggregate**, not averaged across tanks:
  a 20-battle tank does not weigh as much as a 2,000-battle one.
- **Unrated tanks are excluded, not scored as zero**: new, event or hidden vehicles without
  expected values. `wn8_excluded_battles` says how much that leaves out.
- **WN8 under-rewards passive spotting** — it has no assist term. For light tanks, read assist,
  spots and survival beside it.

## Assist: random-only, labelled

The API's random-battle block has assist only as totals, which `wotctx` divides by battles.
The in-game service record shows an **all-battles** figure with no radio/track split, so the
two differ slightly (AMX 13 90: 798 random vs 790 in the client, 2026-09-18). Every result
carrying assist has a caveat saying this; repeat it when quoting assist to the player.
`query tank` also gives `all_battles.assist`, which does match the client.

## Windows and spans

Everything from the API is cumulative since the account was created. Recent form is the
difference between the latest snapshot and the newest one at or before the window start.

- **No snapshot old enough → no answer.** `available: false`, reason "not yet: N days of
  history". Never fall back to lifetime or to a shorter window.
- **Baseline older than asked → the real span is reported.** A "30d" window built on a 40-day-old
  snapshot covers 40 days; the `span` and a caveat say so. Quote the real span.
- History began 2026-09-17. Each sync adds a point; days without a sync blur into one interval.

## Tier bands

Lifetime class totals include years-old low-tier games. Compare current play with
`--min-tier 8` (the brief does). On 2026-09-18 light tanks read 1,489 WN8 across all tiers and
1,924 at tier VIII+.

## Economy figures

| Field | Meaning |
|---|---|
| `credits`, `gold`, `bonds`, `free_xp` | from the last sync, private block (needs a valid token) |
| `price_credit` | tech-tree purchase price, without equipment |
| `affordable`, `credits_short` | against credits **minus the overlay's credit buffer** (500,000) |
| `xp_cost` | research cost from the cheapest owned parent |
| `xp_remaining` | `xp_cost` minus banked XP: the via tank's XP from the client mod's dump, else the overlay's |
| `path_xp` | for a goal behind an unowned tank: remaining XP to that tank plus the goal's own |
| `free_xp_allowed` | the player's free-XP tier limit (preferences.free_xp_max_tier; none by default) |

Vehicle XP is in no API. It comes from the client mod's dump (dated by its capture time) or,
without one, from the overlay, typed in by hand and dated by its `updated_at`.

## Elite

`elite` (from the client mod's dump) is true when every module **and every follow-on vehicle** of a
tank is researched. False therefore does not mean modules are still locked: a tank whose next
vehicle is not yet researched is not elite with every module unlocked. Nothing in the data shows
module-level research; ask the player.

## Mastery

`mastery` 0 none, 1 third class, 2 second class, 3 first class, 4 Ace Tanker — the best ever
earned on the tank, from `wg:account/tanks`. Marks of Excellence are a different thing and are
not in any data.
