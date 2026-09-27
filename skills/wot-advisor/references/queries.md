# `wotctx` command cookbook

Every `query` prints one line of compact JSON: `{"data": …, "meta": …}`. Add `--pretty` only
when a person will read it. `meta.sources` lists each source used with `observed_at` and
`age_seconds`; `meta.caveats` lists known gaps; `meta.overlay_version` is the overlay's
`updated_at`. Queries never create the cache: with nothing synced they fail with "run: wotctx
sync".

## Health and data

| Command | Use it for |
|---|---|
| `wotctx doctor --json` | The freshness gate. Checks: `build`, `config`, `secrets`, `wg-auth`, `data-age`, `vehicles`, `overlay`, `wn8`, `mod-data`. Each has `status` (ok/warn/fail/unknown), `detail`, and `fix` — the exact command to run |
| `wotctx sync` | Refresh everything that is past its TTL (account and stats hourly, reference data weekly). Safe to run often; fresh sources are skipped |
| `wotctx sync --only wg` / `--only wn8` | One source family |
| `wotctx brief` | The one-page orientation. Read once per conversation |
| `wotctx overlay validate` | After editing the overlay; shows what each tank name resolved to |
| `wotctx overlay path` | Where the overlay file is |

## Queries

### `wotctx query resources`
Credits, gold, bonds, free XP, `premium` (from the overlay: `premium_account`, `wot_plus`,
`source`), Personal Reserves as counts (`reserves.kinds`, `reserves.count`, `reserves.active`),
`last_battle`. **Questions:** "can I afford…", "how much free XP…", "am I premium".

### `wotctx query garage [--tier N] [--class X]`
Owned vehicles only (the 37 real ones; the API's extra ids are a caveat, not tanks). Per tank:
tier, class, nation, premium flag, `mastery` (0–4), and a lifetime random-battle `stats` line.
`--class` accepts `LT`/`MT`/`HT`/`TD`/`SPG` or API names. **Questions:** "what tier X do I
have", "my best mediums", strongest/weakest by class.

### `wotctx query tank <name|id>`
Everything about one vehicle, owned or not. Names are forgiving — case, punctuation and
accents are ignored (`Tesak`, `obj 277`); an ambiguous name errors with the candidates' ids,
an unknown one with suggestions. Fields: vehicle facts and price; `owned`; `lifetime` stats
line; `all_battles` (all modes: `assist`, `avg_blocked` — the figures the in-game service
record shows); `recent` (30 d and 60 d, each `available` with a `span`, or a `reason`);
`researched_from` / `unlocks` with XP costs; `overlay` (researched-not-bought, goals).
**Questions:** "how am I doing in X", "is X underperforming", "what does X lead to".

### `wotctx query performance [--by class|tier|nation] [--window lifetime|30d|60d] [--min-tier N]`
Rollups with a `total` line. **Use `--min-tier 8` for current play** (framework §3.2); the
all-tier total mixes in old low-tier games. A window without a baseline returns
`available: false` and a `reason`; one whose baseline is older than asked reports its real
`span` and says so in a caveat. **Questions:** "am I better in mediums or heavies", "which
nation suits me", "am I improving" (windowed).

### `wotctx query sessions [--window 7d]`
One `interval` per pair of consecutive syncs: `from`, `to`, a stats line, and `tanks` played.
There is no battle log, so an interval is not a play session; quote its span. `complete` is
false when history does not reach the window start. **Questions:** "how did my week go",
"how was yesterday".

### `wotctx query candidates [--budget-credits N]`
What to buy or research next: overlay researched-not-bought ∪ one step from owned tanks ∪
the overlay goal path; SPGs excluded (`excluded_by_preference`). Per candidate: `origin`,
`researched` (true or null — unknown), `via` (owned parents), `xp_cost`, `xp_remaining` (when
the overlay records banked XP), `path_xp` (for a goal behind an unowned tank: the rest of that
step plus its own), `free_xp_allowed` / `free_xp_covers` (policy-aware), `price_credit`,
`affordable` / `credits_short` (**buffer already included**), `class_rank`, `goal`
(target/step). Top level: `credits`, `free_xp`, `credit_buffer`, `free_xp_max_tier`,
`improvement_focus`. **Questions:** "what should I get next", "can I afford the Tesák".

### `wotctx query missions [--operation X [--open]]`
Classic personal-mission progress (Campaign 1 only — the API describes no other): per
operation `missions`, `done`, `done_with_honors`, `not_done`, and `open_by_class` (count and
the lowest-numbered open mission with its `primary` and `secondary` conditions and tier
range). `--operation` (id or name, e.g. `"T 55A"`) adds `detail` with every mission and its
status; add `--open` to list only the missions not yet done. `undescribed_statuses` counts
statuses from later campaigns. Not the Dravec / Fossa /
Black Rock chains — those are not in the API. **Questions:** "which personal missions are
left", "what does HT-12 need", "should I start the Object 260".

## Question-time fetches (never cached)

### `wotctx meta moe <name|id>`
Mark of Excellence thresholds for one tank (tiers V–XI) from tomato.gg, fetched now:
`thresholds` (combined damage for 65/85/95/100 %), `change_30d`, and the player's
combined-damage **range** — `lifetime` and `recent_30d` (`low`, `high`, `battles`,
`confidence`), or `recent_reason`. The first source in `meta.sources` is the page itself,
aged from its own "updated" stamp. **Questions:** "can I mark X", "how far is the second mark".

## Recipes

| Question | Calls |
|---|---|
| What should I get next? | `brief`, `query candidates`, then `query tank` for the two or three that fit best; server-wide strength from meta sources only as a tie-breaker |
| Is my X underperforming? | `query tank X`, `query performance --by class --min-tier 8`; then the tank's server-wide DPG |
| How is my light-tank play going? | `query performance --by class --min-tier 8` (lifetime and 30d), `query garage --class LT` |
| How did my week go? | `query sessions --window 7d` |
| Can I mark X? | `meta moe X`: thresholds with the page date, and the player's marks, % and moving average from the client mod's dump (else a combined-damage range, and ask for the %) |
| Should I spend free XP on…? | `query resources`, `query candidates` — `free_xp_allowed` settles it |
| Which missions are left? | `query missions`; `--operation X --open` for the open ones with conditions; the new chains from the player |
| What's fitted on X? | `query tank X` → `client`: equipment, shells, consumables, directives, crew skills, from the client mod's dump; ask the player only without one |
| How fresh is my data? | `doctor --json`; relay `data-age`, `wn8` and any warnings |
