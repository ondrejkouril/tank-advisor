# `wotctx` — specification

Status: v1, 2026-09-17; updated for phase 5 on 2026-09-27, as `docs/spec-desktop.md` §13 lists.
This document is the contract for `wotctx`. `docs/spec-desktop.md` is the contract for the Tank
Advisor app built around it, and `docs/plan.md` the schedule for building both.

## 1. Purpose

Give Claude grounded, current context about one World of Tanks PC account so that questions
like *"what tank should I get now?"*, *"which Tier X should I push for marks?"* and *"is my
light-tank play improving?"* are answered from real data, with stated freshness and explicit
sample-size caveats.

This is a single-account tool: one Wargaming account per Windows user, set up by the Tank
Advisor app, or by `wotctx auth wg --realm eu|com|asia`, which records the account that logs
in. Nothing names an account by default. It was built with, and validated on, one EU account,
whose account lines now live in its owner's `config.yaml`. No hosting, no multi-user, no web UI.

## 2. The three layers

| Layer | Artifact | Changes | Owner |
|---|---|---|---|
| **Reasoning** | `skills/wot-advisor/` — hand-written Claude skill | Rarely, deliberately, in git | Human |
| **Data** | `wotctx` Go CLI → SQLite cache → small JSON on demand | Every sync | Machine |
| **Overlay** | `wot-overlay.yaml` — hand-edited | When the account changes | Human |

The layers stay separate on purpose. The skill holds judgement and never hardcodes numbers.
The CLI holds numbers and never emits a verdict. The overlay holds what no API exposes.

**Non-negotiable:** cached data is never pasted wholesale into context. The CLI's job is to
answer narrow questions with small payloads.

## 3. Verified data sources

### 3.1 Wargaming Public API

Base: `https://api.worldoftanks.eu/wot/`. Docs: <https://developers.wargaming.net>.

App type **Standalone** → **10 requests/second per IP**; `wotctx` self-caps at 5 req/s, leaving
headroom for the game client on the same connection.

> The registration form labels the two types **Server** and **Mobile**, not "Server" and
> "Standalone". *Mobile is the standalone type.* Server applications allowlist up to five IP
> addresses and only accept requests from them, which is wrong for a personal tool on a home
> connection. For standalone applications "only the application_id is validated" — so no
> redirect URI is registered, and `redirect_uri` is a free parameter on `auth/login`. That is
> what makes the local-callback login flow in §3.1 work.

> The developer portal is a JavaScript SPA and cannot be fetched as text, and the public demo
> key is dead (`DEMO_APPLICATION_IS_BLOCKED`, code 407). The field lists below were verified
> against the generated types in `github.com/IceflowRE/go-wargaming/v4/wargaming/wot`
> (files `account_info.go`, `tanks_stats.go`, `account_tanks.go`, `encyclopedia_vehicles.go`).
> Treat this section as the reference, and re-verify against a live response in Step 6.

**Error convention.** WG returns **HTTP 200 with a body of `{"status":"error", "error":{...}}`**.
Clients MUST parse the body to detect failure. Known codes: `407 INVALID_APPLICATION_ID`,
`407 DEMO_APPLICATION_IS_BLOCKED`, `407 INVALID_ACCESS_TOKEN`, `504 SOURCE_NOT_AVAILABLE`.

**Conditional requests do not work.** Measured 2026-09-17 against `encyclopedia/vehicles`: the
API returns a weak, content-stable `ETag` (identical across repeated identical requests) but
**ignores `If-None-Match`**, answering 200 with the full body rather than 304. An earlier draft
of this spec claimed conditional requests were supported; that was inherited from the planning
brief and is wrong.

Consequences: **per-source TTLs are the only real defence for the request budget**, which makes
the values in §4's config the thing to get right. The client's 304 handling and the `http_cache`
table are kept because they are correct if Wargaming ever starts validating, and because the
stable ETag is still a usable content-change signal — but neither saves a request today.

#### `account/info`

`extra` accepts exactly:
`private.boosters`, `private.garage`, `private.grouped_contacts`, `private.personal_missions`,
`private.rented`, `statistics.epic`, `statistics.fallout`, `statistics.globalmap_absolute`,
`statistics.globalmap_champion`, `statistics.globalmap_middle`, `statistics.random`,
`statistics.ranked_10x10`, `statistics.ranked_15x15`, `statistics.ranked_battles`,
`statistics.ranked_battles_current`, `statistics.ranked_battles_previous`,
`statistics.ranked_season_1`, `statistics.ranked_season_2`, `statistics.ranked_season_3`.

Public: `account_id`, `nickname`, `clan_id`, `created_at`, `last_battle_time`, `logout_at`,
`global_rating`, `client_language`.

`private` (requires `access_token`): `credits`, `gold`, `bonds`, `free_xp`, `garage []int`,
`is_premium`, `premium_expires_at`, `battle_life_time`,
`boosters{count, expiration_time, state ∈ ACTIVE|INACTIVE|USED}`, `rented`,
`personal_missions`, `restrictions.chat_ban_time`, `ban_info`, `ban_time`, `is_bound_to_phone`,
`grouped_contacts`.

`wotctx` asks only for the fields it reads, through the `fields` parameter (spec-desktop §12.3):
`account_id`, `nickname`, `last_battle_time`, `global_rating`, the private resources, `garage`,
`boosters`, `personal_missions`, `statistics.random` and `statistics.all`. That leaves out
`ban_info`, `restrictions`, `is_bound_to_phone` and the per-tank frag counts, and it never asks
for `grouped_contacts`, which is other players' data. The response fell from 24.6 KB to 11.8 KB.

`private.personal_missions` is requested with every account/info call (no extra request) and
stored per snapshot in `pm_status`; names and conditions come from
`encyclopedia/personalmissions`, which describes Campaign 1 only (measured 2026-09-18).

**There is no vehicle-XP field anywhere in the API.** Per-vehicle banked XP must live in the
overlay and be maintained by hand. This is the single most important gap in the data layer.

#### `tanks/stats`

`extra`: `epic`, `fallout`, `random`, `ranked_10x10`, `ranked_battles`.
`in_garage=1|0` filters owned vs sold (requires a valid token for the account).
`tank_id[]` max 100 per call.

Per-tank, per-mode fields used by `wotctx`:
`battles`, `wins`, `losses`, `draws`, `survived_battles`, `damage_dealt`, `damage_received`,
`frags`, `spotted`, `xp`, `battle_avg_xp`, `hits_percents`, `piercings`, `shots`, `hits`,
`capture_points`, `dropped_capture_points`, `stun_number`, `max_damage`, `max_frags`, `max_xp`.

**The assist breakdown exists only in the `all` block, not in `random`.** Measured 2026-09-17:
a per-tank entry's `all` block carries 33 fields including `avg_damage_assisted`,
`avg_damage_assisted_radio`, `avg_damage_assisted_track`, `avg_damage_assisted_stun`,
`avg_damage_blocked` and `tanking_factor`; the `random` block carries 24 fields and **none of
them**. So assist figures are only available combined across every battle type.

~~Consequence: any answer quoting assist must source it from `all` and say so.~~ Superseded by
the correction below: the `avg_*` fields are absent from `random`, but assist totals are not.
The rule now in force is in §6.4.

> **Correction, measured 2026-09-18.** The `random` block lacks the `avg_*` fields but does
> carry assist as **totals**: `radio_assisted_damage`, `track_assisted_damage`,
> `stun_assisted_damage`, alongside `capture_points` and `dropped_capture_points`. These are
> stored (migration 4) and difference exactly under a delta, which the weighted-average
> reconstruction of §6.3 does not.
>
> Checked against the client for the AMX 13 90: the in-game service record shows **790**
> assist, a single figure with no radio/track split. That is the **`all`** figure — 540
> battles, radio 736.4 + track 53.9 = 790.4, equal to the API's `avg_damage_assisted` — not
> the random-only one (529 battles, 744.5 + 53.7 = 798.2). So the client confirms the totals
> mean what they say (the `all` totals divide back to the API's averages exactly), but it
> does not display the random-only figure, so that one rests on the API alone. The client also
> never shows the radio/track split; that exists only in the API.

**All WG per-tank data is cumulative since account creation.** There is no windowed view.
See §6.1 for how recent form is derived.

**`private.garage` over-reports.** Measured against the live account: it returned **48 ids
while the client showed 37 vehicles**, and the 11 extras are exactly the ids
`encyclopedia/vehicles` refuses to describe — the API returns the id as a key with a **null
body**, even when asked for it directly by `tank_id`. None had recorded battles. Since every
resolvable id corresponds to a vehicle the player can actually see, and no unresolvable one
does, the resolvable count is the truthful garage size.

`wotctx` therefore: skips null-bodied entries when building the reference list (a nameless
tier-0 row is worse than none); reports the garage as `Identified` vs `Unresolved`; and counts
only `Identified` when answering "how many tanks do I have". The unresolved ids are disclosed
as a data artefact, **not** as hidden vehicles.

> An earlier draft of this spec called these "owned vehicles absent from the encyclopedia" and
> put the garage at 48. That was wrong, and was caught only by counting the garage in the
> client. The ids are valid vehicle compact descriptors, so they are plausibly removed or
> test vehicles the account has some lingering record of — but the tool makes no claim about
> what they are, only that they are not tanks the player has.

**`private.is_premium` and `premium_expires_at` cannot be trusted.** Measured 2026-09-17: the
account holds both a Premium Account and a WoT Plus subscription, and the API reported
`is_premium: false` with `premium_expires_at` of **2018-05-16** — the last lapse of the legacy
time-based product. WoT Plus is a separate subscription (introduced in update 1.20.1) and the
API's premium fields evidently do not track it.

This is not cosmetic: premium status changes credit and XP earn rates, so it feeds any
recommendation about what to grind and whether a purchase is affordable. **Premium status comes
from the overlay** (§5), and no answer may state it from the API. The raw fields are still
stored, since they are what the API said, but they are labelled unreliable at the point of use.

#### `account/tanks`

`tank_id`, `mark_of_mastery` (0 none, 1 third class, 2 second, 3 first, 4 Ace Tanker),
`statistics{battles, wins}`. This is the only source of mastery badges.

**Marks of Excellence are not in the WG API at all** — neither the mark count nor the damage
requirement. The player's mark percentages are in no data source until the client mod (§3.6);
thresholds are fetched at question time with `wotctx meta moe` (§3.5).
**WN8 is likewise absent**, so it is computed locally from XVM's expected values (§3.3, §6.2).

#### `encyclopedia/vehicles`

Filters: `tank_id[]`, `tier[]`, `nation[]`, `type[] ∈ heavyTank|AT-SPG|mediumTank|lightTank|SPG`,
`limit` (max 100), `page_no`.

Tech-tree edges **are** exposed, which removes the need to hand-encode research paths:

- `next_tanks` — `map[researched_tank_id]xp_cost`
- `prices_xp` — `map[parent_tank_id]xp_cost`
- `modules_tree[*]` — `next_modules`, `next_tanks`, `price_credit`, `price_xp`, `type`

Also: `tank_id`, `name`, `short_name`, `tier`, `type`, `nation`, `tag`, `is_premium`, `is_gift`,
`is_premium_igr`, `is_wheeled`, `price_credit`, `price_gold`, `description`, `multination`,
module id lists (`engines`, `guns`, `turrets`, `radios`, `suspensions`, `provisions`), and
`default_profile` with full gun/armor/engine/ammo/stun characteristics.

**Open:** whether Tier XI vehicles (Update 2.0; Executor / Gorilla / Fauteur added in 2.2.1) are
returned. The `tier[]` filter documents no value whitelist, so tier 11 is expected to work, but
this is unverified without a live key. Resolved by the Step 5 acceptance test. If tier 11 is
absent, the overlay gains `research_paths:` and those edges are stored with `source='overlay'`
so provenance stays honest.

#### `auth/login` / `auth/prolongate`

OpenID. Tokens are valid ~2 weeks. CLI flow:

1. `GET auth/login` with `application_id`, `nofollow=1`, `redirect_uri=http://127.0.0.1:<port>/callback`
   → JSON containing the `location` to open rather than a redirect.
2. Open that URL in the user's browser; a one-shot local HTTP server captures the callback
   query: `status=ok`, `access_token`, `account_id`, `nickname`, `expires_at`.
3. `auth/prolongate` extends an unexpired token. `wotctx` prolongs automatically when fewer
   than 3 days remain.
4. `auth/logout` revokes the token (`wotctx auth wg --logout`, the app's *Log out*).

`auth/prolongate` and `auth/logout` read `access_token` only from a POST body. Sent in a GET
query, the token is ignored, and the call fails with `ACCESS_TOKEN_NOT_SPECIFIED`. This was
measured on 2026-09-27, after renewing had silently never worked. Every other endpoint accepts
GET.

### 3.2 Tomato.gg API — NOT USED

**The Tomato.gg API requires a paid subscription to issue a key.** That is a recurring cost for
a personal tool, and it was declined, so the API is out of the design entirely. Nothing in
`wotctx` sends a request to `api.tomato.gg`.

Tomato.gg's **public web pages remain in use** at question time (§3.4) — those need no key.

What that removed, and what replaced it:

| Was going to come from Tomato.gg | Replacement |
|---|---|
| WN8 | **Computed locally** (§6.2) from XVM's free expected-values table |
| Recent-form windows (1–120 d) | **Wargaming self-deltas** between local snapshots (§6.1) |
| Sessions by day/week/month | Deltas between consecutive snapshots (§6.1) |
| MoE progression | Nothing until the client mod (§3.5); requirement tables via §3.4 |
| Loadouts, battle logs (mod-captured) | Nothing until the client mod (§3.5) |
| Map stats, rankings, WNX | Dropped. WNX is proprietary; WN8 covers the need |

**The cost is real and is stated wherever it bites: there is no history before the first local
snapshot.** Wargaming's `ratings/dates` and `ratings/accounts` endpoints, which might have
backfilled account-level history, are discontinued — both return `RATINGS_NOT_FOUND` (measured
2026-09-17). So no free source can reconstruct the past. A 7-day window is unanswerable until
seven days of snapshots exist, and a 30-day window until thirty. §6.1 requires saying so rather
than quietly narrowing the window.

This also makes regular syncing load-bearing rather than a convenience, since a snapshot not
taken is history permanently lost. See §7.4.

For the record, the endpoints that were verified before the key requirement was discovered:

| Purpose | Path | Notes |
|---|---|---|
| Recent stats | `/api/player/recents/:server/:id` | `days`, `battles`, `cache` |
| Lifetime + per-tank | `/api/player/overall/:server/:id` | `cache` |
| Sessions | `/api/player/sessions/:server/:id` | `days`, `showTanks`; grouped day/week/month |
| Per-tank windows | `/api/player/tank-recents/:server/:id/:tankId` | fixed 1/3/7/14/30/60/120 d |
| Per-tank sessions | `/api/player/tank-sessions/:server/:id/:tankid` | |
| MoE progression | `/api/player/moe-progression/:id/:tankId` | **no `:server`** |
| Map stats | `/api/player/map-stats/:id` | **no `:server`**; `days`, `detailed`, `tiers`, `classes`, `nations`, `tankId` |
| Stat history | `/api/player/overall-tracker/:id` | by battle count |
| Rankings | `/api/player/rankings/:server/:id/:type` | `type ∈ overall|recent` |
| Onslaught | `/api/player/onslaught/:id` | last 60 days |
| **Loadouts (mod)** | `/api/player/equipment/:id` | equipment, consumables, shells, field mods, directives, crew |
| Battle log (mod) | `/api/player/battles/:id` | advanced; loadouts per battle |
| Battle log (basic) | `/api/player/common-battles/:id` | wider coverage, no loadouts |
| Battle log (merged) | `/api/player/combined-battles/:id` | page size capped at 10 |
| Mod sessions | `/api/player/mod-sessions/:id` | session = >6 h gap |
| Battle detail | `/api/player/battle-detail/:id` | `:id` is an arena id |
| Replay | `/api/player/battle-replay/:playerId/:arenaId` | |
| Bulk | `/api/player/bulk-stats/:server`, `/api/player/bulk-tank-stats/:server` | `ids=` |

There are no non-player endpoints. Base `https://api.tomato.gg`, header `x-api-key: tmgg_…`,
60 requests/minute, `:server ∈ com|eu|asia`. **None of this is used.**

### 3.3 WN8 expected values — XVM

WN8 is absent from the Wargaming API, and it is the figure the community expresses every
comparison in, so `wotctx` computes it (§6.2).

Source: `https://static.modxvm.com/wn8-data-exp/json/wg/wn8exp.json` — free, no key, ~100 KB,
regenerated nightly. Each entry carries a vehicle's expected damage, frags, spots, defence
points and win rate, derived by XVM from active players on Wargaming's servers, and the file
carries a `header.version` date.

> **Not** `…/json/wn8exp.json`, which an earlier draft named. Measured 2026-09-18, that file
> still answers HTTP 200 but is frozen at **version 2024-09-12**, when XVM split its data
> between Wargaming and Lesta: it has 862 vehicles and lacks everything released since,
> including all of Tier XI and three vehicles in this garage (Executor, Leox, Prototipo 6).
> The `wg/` file is version 2026-09-12 with 992 vehicles, including all three. Its figures
> match those served by an independent calculator (wn8-calculator.fpcstat.cz) exactly. A
> frozen source keeps returning 200, so `doctor` warns when the version stamp passes 60 days.

Treated as slow-changing reference data like the vehicle encyclopedia: a seven-day TTL, stored
in `wn8_expected` (the whole table replaced per fetch, never mixed across vintages), and
stamped with both XVM's version and the fetch time, so a WN8 figure can say which vintage of
expected values produced it.

### 3.4 Overlay — `wot-overlay.yaml`

Hand-edited, versioned in git, the only writable-by-human data source. Schema in §5.

### 3.5 Meta data — fetched at question time, never synced

Server-wide tank performance, economics, and MoE/mastery requirements have no API. They are
fetched as text by the skill when a question needs them, and the answer must quote the page's
own timestamp.

`https://tomato.gg/moe/<server>` carries the full MoE requirement table (tank, tier, class,
65/85/95/100 % thresholds, 30-day drift) and its own "updated" timestamp, but WebFetch sees
only the first of 16 pages, so `wotctx meta moe <tank>` fetches it at question time and
extracts the row, never storing it. Patch notes and news (worldoftanks.eu) are readable too;
tanks.gg and the web shop return app shells that cannot be read. The skill's
`references/meta-sources.md` keeps the checked list.

Rationale for not syncing: the data is server-wide rather than personal, it changes on WG's
schedule rather than the account's, and caching it would make freshness claims harder to
defend, not easier.

### 3.6 `wotctx` client mod — as built (phase 4)

Several things exist in no API, free or paid, and the game client is the only place they are
known. A small read-only client mod (`mod/mod_wotctx.py`, mod id `ondrejkouril.wotctx`) writes
them to a local file, and `wotctx` reads that file as a third source, `mod:garage`, beside `wg:`
and the overlay.

| Gap | Why the API cannot supply it | In the dump |
|---|---|---|
| Per-vehicle XP | no field anywhere in the API (§3.1) | `xp`, `elite` |
| Marks of Excellence and progress | absent from the API | `marks`: count, the garage's percentage, `moving_avg_damage` |
| Loadouts | never exposed | equipment, shells (kind and count), consumables, directives |
| Crew | never exposed | per seat: role, skills with training level, bonus skills |
| Premium / WoT Plus | the API's fields are wrong (§3.1) | the client's own state, with the expiry |
| Research | never exposed | `unlocked`: every researched vehicle's `tank_id` (mod 0.3.0) |
| The real garage | `private.garage` over-reports (§3.1) | `rented` per vehicle; Steel Hunter vehicles are tier I with `_SH` names |

**The mod.** Python 2.7, the client's version, compiled to `.pyc` (the loader accepts only
`mod_*.pyc` outside development builds). It is packed as an uncompressed `.wotmod` with
`meta.xml` (`make mod`), and copied into `mods/<game version>/` by `wotctx mod install`. The
Tank Advisor app puts it into each new `mods/<game version>/` after a game update by itself
(spec-desktop §6.2); the command remains for anyone without the app. It hooks `g_playerEvents.onAccountShowGUI` and
`IItemsCache.onSyncCompleted`, and writes three seconds after each burst of events. It writes
only in the lobby (`PlayerAccount`, never the in-battle `Avatar`), and only once the account
has synced. Everything it reads comes from the client's own objects, documented from the
decompiled client source for the installed build (`IzeBerg/wot-src`, branch EU).

**Design constraints, all load-bearing:**

- **One-way local dump, no network.** It writes `%LOCALAPPDATA%wotctxmodgarage.json`,
  atomically, and nothing else. A test checks that the mod imports no network module. This
  keeps the "WG private data never leaves this machine" guarantee (§11) trivially true.
- **Nothing in battle, no input, no UI.** Wargaming permits informational mods and prohibits
  unfair advantage and input automation. A read-only dump of one's own garage is in the
  permitted category.
- **A failure costs a field, not the dump.** Each vehicle part is read on its own. A part a
  game update breaks is left out, and named in the dump's `errors`, which `sync` turns into a
  caveat. Every exception is caught and logged as one line in the client's log (`game.log`).
- **A third source, never a silent substitute.** The snapshot's `requested_at` is the capture
  time. `doctor`'s `mod-data` check, and a caveat on every answer that uses the dump, say
  when battles were played after it (Wargaming's last battle time), when it predates the
  installed game version, and when a game update has removed the mod.

**Where it is used.** `query tank` shows a `client` block, `meta moe` shows the player's real
marks, and `query resources` takes premium from the client. Goals take the via tank's XP from
the game, `candidates` knows what is researched (researched, not owned, not premium, tier VIII and up; a sold tank only where its line stopped, with `played_before`), and rentals leave the garage. With no dump, every answer is
what it was before the mod. The overlay's `premium`, `xp_banked` and `researched_not_bought`
are a backup, used only without a dump.

**Not yet:** switchable setups beyond the fitted one, field modifications, and a per-battle log.
The last would give true session history with no cold start, but it touches the battle-results
flow.

## 4. Storage

Pure-Go `modernc.org/sqlite` (no cgo, so Windows and macOS/Linux cross-compile from one tree).
Database at `%LOCALAPPDATA%\wotctx\wotctx.db` on Windows, `$XDG_DATA_HOME/wotctx/wotctx.db`
otherwise. WAL mode.

Every HTTP response body is stored gzipped in `snapshots` before parsing. A parser bug or an
upstream schema change is therefore a re-parse, never a re-fetch — which matters when the
budget is 60 requests a minute.

```sql
-- provenance
CREATE TABLE snapshots(
  id INTEGER PRIMARY KEY, source TEXT, endpoint TEXT, requested_at TEXT,
  http_status INTEGER, wg_error TEXT, etag TEXT, not_modified INTEGER DEFAULT 0,
  raw_gz BLOB);
CREATE TABLE http_cache(url_key TEXT PRIMARY KEY, etag TEXT, last_modified TEXT, fetched_at TEXT);
CREATE TABLE sync_runs(id INTEGER PRIMARY KEY, started_at TEXT, finished_at TEXT,
  ok INTEGER, notes TEXT);

-- reference data
CREATE TABLE vehicles(tank_id INTEGER PRIMARY KEY, name TEXT, short_name TEXT,
  tier INTEGER, type TEXT, nation TEXT, tag TEXT, is_premium INTEGER, is_gift INTEGER,
  is_wheeled INTEGER, price_credit INTEGER, price_gold INTEGER, synced_at TEXT);
CREATE TABLE vehicle_edges(from_tank_id INTEGER, to_tank_id INTEGER, xp_cost INTEGER,
  source TEXT,                         -- 'next_tanks' | 'prices_xp' | 'overlay'
  PRIMARY KEY(from_tank_id, to_tank_id, source));

-- account state, one row per sync so resource history is a free by-product
CREATE TABLE account_snapshots(snapshot_id INTEGER PRIMARY KEY REFERENCES snapshots(id),
  observed_at TEXT, credits INTEGER, gold INTEGER, bonds INTEGER, free_xp INTEGER,
  is_premium INTEGER, premium_expires_at TEXT, global_rating INTEGER,
  last_battle_time TEXT, battle_life_time INTEGER);
CREATE TABLE garage(snapshot_id INTEGER, tank_id INTEGER, PRIMARY KEY(snapshot_id, tank_id));
CREATE TABLE boosters(snapshot_id INTEGER, kind TEXT, count INTEGER, state TEXT, expires_at TEXT);

-- per-tank cumulative, retained per snapshot so deltas are computable
CREATE TABLE tank_stats(snapshot_id INTEGER, tank_id INTEGER, mode TEXT,
  battles INTEGER, wins INTEGER, losses INTEGER, draws INTEGER, survived INTEGER,
  damage_dealt INTEGER, damage_received INTEGER, frags INTEGER, spotted INTEGER,
  xp INTEGER, battle_avg_xp INTEGER, hits_percents INTEGER,
  avg_damage_assisted REAL, avg_damage_assisted_radio REAL, avg_damage_assisted_track REAL,
  avg_damage_assisted_stun REAL, avg_damage_blocked REAL, tanking_factor REAL,
  mark_of_mastery INTEGER, PRIMARY KEY(snapshot_id, tank_id, mode));

-- Tomato.gg: dropped in migration 3 (§3.2); shown as the v1 record
CREATE TABLE tomato_recents(snapshot_id INTEGER, window_days INTEGER, window_battles INTEGER,
  battles INTEGER, wr REAL, dpg REAL, wn8 REAL, wnx REAL, kpg REAL, survival REAL,
  assist REAL, spots REAL, PRIMARY KEY(snapshot_id, window_days, window_battles));
CREATE TABLE tomato_tank_recents(snapshot_id INTEGER, tank_id INTEGER, window_days INTEGER,
  battles INTEGER, wr REAL, dpg REAL, wn8 REAL,
  PRIMARY KEY(snapshot_id, tank_id, window_days));
CREATE TABLE tomato_sessions(snapshot_id INTEGER, bucket TEXT, period_start TEXT,
  battles INTEGER, wr REAL, dpg REAL, wn8 REAL, PRIMARY KEY(snapshot_id, bucket, period_start));
CREATE TABLE moe_progression(snapshot_id INTEGER, tank_id INTEGER, observed_at TEXT,
  marks INTEGER, percent REAL, PRIMARY KEY(snapshot_id, tank_id, observed_at));
CREATE TABLE loadouts(snapshot_id INTEGER, tank_id INTEGER, slot_kind TEXT, slot_index INTEGER,
  item_name TEXT, item_id INTEGER,
  PRIMARY KEY(snapshot_id, tank_id, slot_kind, slot_index));
```

The block above is the v1 schema as designed. Later migrations changed it:

| Migration | Change |
|---|---|
| 2 | Achievements: `achievement_defs`, `account_achievements`, `tank_achievements` |
| 3 | Drops the three `tomato_*` tables (§3.2); adds `wn8_expected` |
| 4 | `tank_stats` gains `capture_points`, `dropped_capture_points`, the three `*_assisted_damage` totals and `parser_version`; `wn8_expected` gains `version` |
| 5 | Personal missions: `pm_missions` (reference), `pm_status` (per account snapshot) |

`moe_progression` and `loadouts` hold the client mod's marks and fitted items (§3.6), beside
`mod_account`, `mod_vehicles`, `mod_crew` and `mod_unlocked` (migrations 6 and 7). Migration 8
adds `snapshots.raw_pruned`: each sync drops raw bodies older than 90 days, keeping the newest
per source and every parsed row. A pruned snapshot stays usable but can no longer be re-parsed
(spec-desktop §12.3). The newest
snapshot per source is resolved by store functions (`LatestUsableSnapshot`, `LatestTankStats`,
`TankStatsSnapshots`), not by SQL views, so query code never hand-rolls `MAX(snapshot_id)`.

**Configuration.** `config.yaml` sits in `%APPDATA%\wotctx\`, or `$XDG_CONFIG_HOME/wotctx/`
elsewhere. `WOTCTX_CONFIG_DIR` and `WOTCTX_DATA_DIR` replace the two folders outright, for
evaluations run on a copy. Unknown keys are an error. The file is written by people, by
`wotctx auth wg`, and by the app, always through `yamledit`, which rewrites only the lines a
change touches.

| Key | Meaning |
|---|---|
| `account.realm`, `.account_id`, `.nickname` | the account; recorded by the first login, never typed |
| `overlay_path` | the overlay; default `<config dir>/wot-overlay.yaml` |
| `game_dir` | the game folder, when detection (spec-desktop §6.1) should not decide |
| `sync.auto_sync_after`, `sync.ttl` | when the skill syncs before answering, and per-source refetch intervals |
| `sync.retention` | unset keeps all history; set (at least `7d`), each sync deletes older snapshots and their rows (spec-desktop §12.4) |
| `meta.moe_fetch` | `false` stops the question-time fetch of tomato.gg's Mark of Excellence page (spec-desktop §12.4) |
| `confidence.*` | the battle-count thresholds of §6.5 |
| `consent`, `setup.completed`, `mod.managed`, `mod.placed`, `duties.paused` | the app's own records (spec-desktop §5); `wotctx` reads none of them |

## 5. Overlay schema

The overlay is the player's own file, kept by default at `<ConfigDir>/wot-overlay.yaml`, where
the Tank Advisor app creates and edits it. `overlay_path` in `config.yaml` points elsewhere,
for a player who keeps it in a repository of their own. The project's repository holds no
overlay; the owner's was taken out of it before publishing, and the example below shows the
shape.

```yaml
version: 1
updated_at: 2026-09-17T18:02:00Z      # required; becomes meta.overlay_version

# The API reports is_premium false with a 2018 expiry for an account that holds
# both, so premium status has to be maintained here (see section 3.1). It
# affects credit and XP earn rates, so it changes grind and purchase advice.
premium:
  premium_account: true
  wot_plus: true

researched_not_bought:                 # names resolved to tank_id at load
  - Type 71
  - WZ-113G FT
  - Kranvagn
  - T110E4
  - Obj 277

xp_goals:
  - target: Executor                   # tank name or tank_id
    via: Concept No. 5
    xp_required: 325000
    xp_banked: null                    # manual; no API exposes vehicle XP

research_paths: []                     # fallback only; used if the API omits a tier
                                       # - from: Concept No. 5
                                       #   to: Executor
                                       #   xp_cost: 325000

preferences:
  avoid_classes: [SPG]                 # Obj 261 and M40/M43 kept only for missions
  strengths: [medium tanks, autoloaders]
  class_rank:                          # 1 = most preferred; ties allowed; aliases accepted
    mediumTank: 1
    lightTank: 2
    AT-SPG: 3
    heavyTank: 3
  free_xp_max_tier: 8                  # free XP researches tanks up to this tier; 0 = no limit
  improvement_focus: [lightTank]       # classes the player is working to improve

constraints:
  playtime: limited                    # recommendations must be time-efficient
  credit_buffer: 500000                # credits that must remain after a purchase

notes: []
```

Validation rules:

- `version` must be 1 and `updated_at` required and parseable (RFC 3339 or a bare date). Unknown
  keys are errors, so a typo cannot silently drop a setting.
- `premium` is authoritative over the API's `is_premium` / `premium_expires_at`. When it is
  absent, premium status is reported as **unknown** rather than defaulting to false — an answer
  that assumed no premium would understate every credit and XP figure it reasoned about.
- Every tank reference must resolve against `vehicles` to **exactly one** vehicle; an unresolved
  name is an error with a nearest-match suggestion. Resolution is an exact case-insensitive
  match on `name` or `short_name`, then, only if that finds nothing, the same comparison with
  punctuation and spacing ignored (`Obj 277` → `Object 277`). The loose pass is a fallback
  because it conflates distinct vehicles (`T29`/`T-29`, `T34`/`T-34`), and the live list has
  exact collisions too (two tier VII `IS-2`s), so more than one match is an error asking for the
  `tank_id`. An unquoted integer is a `tank_id`; a numeric *name* such as `"121"` must be quoted.
- A `researched_not_bought` entry that is already in the garage is reported as **stale** — the
  overlay is meant to drift, and surfacing the drift is part of the job. So is an `xp_goals`
  target already in the garage.
- An `xp_goals` entry with `via` is checked against the tech tree: a missing edge, or an
  `xp_required` that differs from the API's cost, is a warning.
- `avoid_classes` values must be API vehicle types (`lightTank`, `mediumTank`, `heavyTank`,
  `AT-SPG`, `SPG`; `LT`/`MT`/`HT`/`TD`/`arty` are accepted as aliases). An unknown class is an
  error, because it would make the candidates filter silently do nothing.
- `class_rank`, `improvement_focus` and `avoid_classes` take API classes or the usual aliases
  (`LT`, `MT`, `HT`, `TD`, `arty`), canonicalised on load. A class both ranked and avoided is a
  warning; avoidance wins. `free_xp_max_tier` is 0–11; `credit_buffer` is non-negative.
- These preferences are applied by the tool, not left to the skill's arithmetic: `candidates`
  judges `affordable` / `credits_short` with `credit_buffer` included, sets `free_xp_allowed`
  per candidate from `free_xp_max_tier` (and `free_xp_covers` only where allowed), and carries
  each candidate's `class_rank`; the brief states the buffer, the free-XP limit and the focus.
  The judgement of how to weigh them lives in `skills/wot-advisor/references/framework.md`.
- `research_paths` entries are loaded into `vehicle_edges` with `source='overlay'` by `sync`,
  never by `overlay validate`, which has no side effects. An invalid overlay leaves the
  previously loaded edges in place; a deleted one removes them.
- Stale entries and unchecked names (no vehicle list or garage synced yet) are warnings, not
  errors: `overlay validate` exits non-zero only on errors, and doctor warns rather than fails.

**Phase 5: `profile` and `advice`.** Two optional blocks hold the settings each player gives the
advice (spec-desktop §8.3). `profile` has `session_minutes` (10–600), `battles_per_hour` (1–30)
and `experience` (`auto`, `new`, `returning` or `experienced`). `advice` has:
- `weights`, one level (`highest` to `ignore`) for each of the six factors of framework §4.1;
- `answer` (`length`, `format`, `explain_basics`, `also_worth_knowing`);
- `coaching`;
- `rules`: the player's own, at most 20, each at most 300 characters, with the date it was
  added.

Every key is optional, and an absent one means the default. `version` stays `1`. The Tank
Advisor app edits both blocks, and the goals, on its Goals and Advice pages, always through
`yamledit`, so a hand-formatted file keeps its layout.

## 6. Derived metrics

### 6.1 Recent form — local snapshot deltas, with an honest cold start

Wargaming data is cumulative since account creation, and there is no windowed view and no way
to backfill (§3.1, §3.2). So recent form is computed entirely from local snapshots:

```
recent = tank_stats@latest − tank_stats@(newest snapshot at or before the cutoff)
```

Rules, all enforced in `internal/store`:

- **No baseline, no claim.** If no snapshot predates the cutoff, the result is `ErrNoBaseline`
  and the caller must say the window cannot be answered yet. It must never fall back to the
  oldest available snapshot, because that silently answers a different question than the one
  asked.
- **Report the window the data spans, not the one requested.** A "30-day" delta built from a
  45-day-old baseline is reported as 45 days.
- **Averages are recomputed, never subtracted.** See §6.2.
- A tank with no battles in the window is omitted rather than shown as zeros.

**Day zero is the first sync.** Until enough history accrues, windowed questions answer "not
yet — I have N days of history". This is the price of dropping a paid history provider, and it
is stated rather than papered over.

### 6.2 WN8 — computed locally

WN8 is derived per tank from cumulative stats and XVM's expected values (§3.3):

```
rDAMAGE = (damage/battles) / expDamage      rWIN  = (wins/battles)  / expWinRate
rFRAG   = (frags/battles)  / expFrag        rSPOT = (spots/battles) / expSpot
rDEF    = (def/battles)    / expDef         -- def is dropped_capture_points

rWINc    = max(0, (rWIN    − 0.71) / (1 − 0.71))
rDAMAGEc = max(0, (rDAMAGE − 0.22) / (1 − 0.22))
rFRAGc   = max(0, min(rDAMAGEc + 0.2, (rFRAG − 0.12) / (1 − 0.12)))
rSPOTc   = max(0, min(rDAMAGEc + 0.1, (rSPOT − 0.38) / (1 − 0.38)))
rDEFc    = max(0, min(rDAMAGEc + 0.1, (rDEF  − 0.10) / (1 − 0.10)))

WN8 = 980·rDAMAGEc + 210·rDAMAGEc·rFRAGc + 155·rFRAGc·rSPOTc + 75·rDEFc·rFRAGc
      + 145·min(1.8, rWINc)
```

A player exactly at expected values scores 1565 (every clamped ratio is 1), which is the one
figure checkable by hand; the golden tests pin that and several others computed independently.
An earlier draft gave the combination as ending in `− 145` with no win-rate term, which is
wrong. Computed on random battles only, as the rating is defined.

Two requirements:

- **Account WN8 is computed on the battle-aggregate** — actual and `expected × battles` summed
  across every tank — *not* as an average of per-tank WN8. The two differ substantially.
- A tank with **no expected values** (new, hidden, or event vehicles — and §3.1 shows this
  account owns several) is **excluded from the aggregate and reported as such**, rather than
  being scored against zero. So is a row stored before defence points were parsed (its `def`
  is zero because it was never read); `sync` re-parses those from the stored raw bodies, keyed
  on `tank_stats.parser_version`, so in practice it clears on the next run.

Recent WN8 comes from the same formula applied to a delta (§6.1), which is why the delta
arithmetic has to be right.

### 6.3 Averages over an interval

The difference of two averages is meaningless, so an interval average is recovered from the
differenced totals. Where the API supplies only an average and no total — the assist breakdown —
the interval value is reconstructed by weighting each side by its battle count:

```
interval_avg = (latest_avg × latest_battles − baseline_avg × baseline_battles)
               / (latest_battles − baseline_battles)
```

Naive subtraction would be wrong by roughly a factor of twenty on realistic inputs; there is a
test pinning exactly that case. Ratios with no recoverable totals (`hits_percents`,
`tanking_factor`) are left unset rather than guessed.

### 6.4 Rollups

Per class, tier and nation: battles, win rate, DPG, assist (radio / track / stun kept
separate), survival rate, average XP and WN8 — lifetime and windowed.

**Assist is random battles only, from the assist totals, and always labelled** (decided
2026-09-18, superseding the earlier all-mode rule). The `random` block carries
`radio_assisted_damage` / `track_assisted_damage` / `stun_assisted_damage` as totals (§3.1),
so random-only assist is exact, differences exactly under a delta, and sits like-for-like beside
WN8 and every other random figure. Every result carrying it adds the caveat that the in-game
service record shows an **all-battles** figure with no radio/track split, so the two will not
match exactly — the AMX 13 90 reads 798.2 random against 790 in the client. `query tank` also
reports the all-battles assist and blocked damage in an `all_battles` block, for comparison
with the client.

### 6.5 Confidence

Every tank row and every rollup carries a `confidence` flag derived from battle count.
Defaults, configurable:

| Battles | Flag |
|---|---|
| < 30 | `very_low` |
| < 100 | `low` |
| < 300 | `moderate` |
| ≥ 300 | `ok` |

### 6.6 Division of responsibility

The query layer emits numbers and flags. It does not rank, recommend, or conclude. No verdict
may rest on a single metric — that constraint is enforced in the skill, where judgement lives.

## 7. CLI surface

```
wotctx auth wg [--realm eu|com|asia] [--application-id ID] [--relogin] [--prolong] [--logout] [--manual]
wotctx sync [--only wg|wn8] [--full] [--dry-run] [--quiet]
wotctx brief [--max-chars 10000]
wotctx query garage      [--tier N] [--class X]
wotctx query tank <name|id>
wotctx query performance --by class|tier|nation --window lifetime|30d|60d
wotctx query resources
wotctx query sessions    --window 7d
wotctx query candidates  [--budget-credits N]
wotctx query missions    [--operation X]
wotctx doctor [--json]
wotctx overlay validate | path
wotctx meta moe <name|id>                   # fetched at question time, never cached
wotctx version
wotctx mcp                                  # phase 2
wotctx export claude-ai [--out DIR]         # phase 3: brief + skill zip for claude.ai
wotctx mod install [--game-dir DIR] <pkg>   # phase 4: the client mod into mods/<game version>/
wotctx hook session-start                   # phase 3: the plugin's brief hook
wotctx guide [--topic X]                    # phase 5: core rules, the player's advice, the defaults
wotctx data delete [--yes]                  # phase 5: the cache, the dump, the token, the account
```

Phase 5's additions:
- `auth wg --realm` records the account that logs in; a login as a different account than the
  one recorded is refused, and its token is not stored. With no account recorded, a stored
  token does not count as a login.
- `auth wg --logout` revokes the token at Wargaming and removes it here, even when Wargaming
  cannot be reached.
- `guide` prints what `wot_guide` returns, byte for byte (§8).
- `data delete` is the app's *Delete my data*. It keeps the overlay and the player's own
  application id.

`query` always emits JSON — compact by default, since the reader is usually the skill and
indentation is only tokens to it; `--pretty` indents for people (it cut `garage` from 23 KB to
13 KB on the live account). `brief` emits Markdown. `doctor --json` is the staleness probe the
skill calls first. A query never creates the cache: with nothing synced it fails naming
`wotctx sync`.

What each query returns, beyond the shared envelope:

| Query | Data | Notes |
|---|---|---|
| `resources` | credits, gold, bonds, free XP, premium, reserves | Premium from the overlay or `unknown`, never the API. Reserves are counted, not listed — the API names them only by numeric id — except active ones |
| `garage` | count, by tier/class, one lifetime line per owned tank | Identified vehicles only; unresolved ids are a caveat |
| `tank <name\|id>` | vehicle, lifetime line, `all_battles`, 30/60 d windows, tech-tree links, overlay status | Name or id; names resolve as in §5 |
| `performance` | a line per class, tier or nation, plus a total | Windowed via §6.1 |
| `sessions` | one interval per pair of consecutive syncs | With no battle log, a sync interval is the finest grain available; each interval states its span |
| `candidates` | researched-not-bought ∪ next research steps ∪ goal path | XP and credit cost, `free_xp_covers`, affordability, `path_xp` for a goal behind an unowned vehicle; `avoid_classes` applied except to goals |

Every line carries `confidence` (§6.5). A window with no baseline returns `available: false`
and "not yet: N days of history", never a shorter window; a window whose baseline is older than
requested reports its real span and says so in a caveat.

### 7.1 The provenance envelope

`internal/query` is the only package that reads the database, and every result it returns is
wrapped. The CLI prints the envelope; phase-2 MCP tools return the same structure. No consumer
ever needs a second call to learn how fresh the data is.

```json
{
  "data": {  },
  "meta": {
    "generated_at": "2026-09-17T20:11:05Z",
    "sources": [
      {"name": "wg:account/info",  "observed_at": "2026-09-17T19:40:12Z", "age_seconds": 1853},
      {"name": "xvm:wn8exp",       "observed_at": "2026-09-17T19:40:15Z", "age_seconds": 1850, "version": "2026-09-12"}
    ],
    "overlay_version": "2026-09-17T18:02:00Z",
    "caveats": ["the nearest snapshot before the 30d cutoff is from 2026-08-09, so this 30d window actually covers 40.0 days"]
  }
}
```

`meta.sources` must be non-empty for any result derived from cached data.

### 7.2 `brief`

Compact Markdown, **hard limit 10,000 characters**, covering: resources; garage by tier and
class; strongest and weakest tanks (lifetime vs last 30/60 days); overlay goals; per-source
timestamps; and a **Known gaps** section. Sections have a deterministic character budget and
truncate with an explicit note rather than silently.

As built (`internal/brief`): every number comes from the query layer, and the brief carries
provenance by collecting each envelope's sources (newest per source) and caveats (each once).
Characters are counted as runes. Budgets are fixed shares of what the header leaves — data age
9 %, resources 7 %, garage 8 %, performance 16 %, tanks 24 %, goals 18 %, known gaps 14 % — so
the total cannot exceed the cap at any `--max-chars` (minimum 2,000). A section over budget is
cut at a line boundary with `_…N more line(s) cut to fit the brief; run \`<query>\` for all._`.

"Strongest and weakest" is a sort of owned tanks with at least `confidence.very_low_below`
battles (30) by lifetime WN8 — up to five each — with each tank's 30- and 60-day WN8 and its
battle count beside it, and it says in the brief that it is a sort, not a verdict. Known gaps
always includes what no query can raise because no API has it: per-vehicle XP, Marks of
Excellence, loadouts and crew. Measured on the live account: 4,558 characters.

### 7.3 Degradation

A per-endpoint failure is a caveat, not a crash. `sync` exits 0 when some sources succeeded,
records the failure in `sync_runs.notes`, and the caveat propagates into every affected query
envelope and into the brief's Known gaps. Exit non-zero only when nothing could be synced or
the database is unusable.

### 7.4 Syncing is load-bearing

Because recent form is built from local snapshots and nothing can backfill them (§3.2, §6.1), a
sync not taken is history permanently lost. Syncing is therefore not merely a freshness
convenience; it is how the tool acquires its second-most-valuable dataset.

Two mechanisms, deliberately overlapping:

1. **A `SessionStart` hook** runs `wotctx sync --quiet` when a Claude Code session begins,
   asynchronously, so the session never waits on Wargaming (§10). It must be
   `type: "command"`, because MCP servers are not available when `SessionStart` fires. Several
   sessions starting together, or a hook and `wot_sync`, take turns: `sync_runs` doubles as
   a lock (an unfinished run younger than five minutes holds off others, which wait up to a
   minute and then find their sources fresh).
2. **The skill's freshness gate** syncs before answering when `doctor` reports a source past
   both `sync.auto_sync_after` (6 h default) and that source's own TTL. Reference data on a
   seven-day TTL is not due at six hours and `sync` would skip it, so counting it would make
   the gate fire before every answer for nothing.

Neither guarantees regular coverage on days the tool is not used, which coarsens time
resolution: battles played on a Saturday and first synced on a Wednesday collapse into one
four-day delta. No data is lost — the totals are cumulative — but Saturday cannot be isolated.
A scheduled task is therefore offered as an optional third mechanism, and `doctor` reports the
longest gap in snapshot history so the coarsening is visible rather than assumed away.

## 8. The `wot-advisor` skill

Source of truth: `skills/wot-advisor/` in this repo. Since phase 3 it reaches Claude Code as
part of the `wot` plugin (§10), which loads it in place from the repository, so it is available
in every project while staying versioned here. (`make install-skill`, which copied it to
`~/.claude/skills/`, remains for a machine without the plugin.)

`SKILL.md` — under 500 lines, `allowed-tools: Bash(wotctx *) WebFetch`, `description` covering
garage, purchase, research, grind, marks, equipment and crew questions.

Procedure:

1. **Freshness gate** — `wotctx doctor --json`. If any source is older than 6 h, run
   `wotctx sync`. If auth is broken, stop and state the exact command to run.
2. **Orient** — `wotctx brief`.
3. **Narrow** — only the specific `query` calls the question needs. Never paste raw JSON into
   an answer.
4. **Meta at question time** — WebFetch from `references/meta-sources.md`; quote the page's own
   timestamp.
5. **Score** — performance by class and playstyle, overlay preferences and goals, credits/XP/
   bonds, patch and discount timing.
6. **Answer** — *Short answer / Why / Best setup / Watch-outs / Verdict*. Always state data age
   and sample-size caveats. Keep my stats, server-wide performance, and opinion clearly
   separated.

`references/`: `framework.md` (the existing hand-written WoT analysis framework),
`metrics.md` (metric definitions and thresholds), `queries.md` (command cookbook),
`meta-sources.md` (URLs for question-time fetches, with the "no API endpoint" note).

**Phase 5: generic, with the player's advice.** The skill no longer describes one player. The
guide has three layers (spec-desktop §8.2):
1. `core.md`: the grounding rules no setting can switch off;
2. *Your advice*: rendered from the overlay's `profile`, `preferences`, `constraints` and
   `advice` by `internal/advice`, which also flags a rule that conflicts with a core rule or a
   setting;
3. `framework.md`, as the defaults the player's settings override.

`SKILL.md` runs `wotctx guide` before its first judgement. `wot_guide` returns the same text,
and a test holds the two byte for byte. The claude.ai export carries the rendered guide as
`references/guide.md`.

## 9. Phase 2 — MCP

`wotctx mcp` serves over stdio using `github.com/modelcontextprotocol/go-sdk` v1.8.0. There is
no HTTP transport, so nothing listens on a port. The server lives in `internal/cli/mcp.go`
beside the commands it mirrors.

| Tool | CLI counterpart | Arguments |
|---|---|---|
| `wot_data_status` | `doctor --json` | — |
| `wot_brief` | `brief` | `max_chars` |
| `wot_resources` | `query resources` | — |
| `wot_garage` | `query garage` | `tier`, `class` |
| `wot_tank` | `query tank` | `tank` (name or id) |
| `wot_performance` | `query performance` | `by`, `window`, `min_tier` |
| `wot_sessions` | `query sessions` | `window` |
| `wot_candidates` | `query candidates` | `budget_credits` |
| `wot_missions` | `query missions` | `operation`, `open_only` |
| `wot_moe` | `meta moe` | `tank` |
| `wot_sync` | `sync` | `only`, `full` |
| `wot_guide` | — (the skill's text, embedded) | `topic`: `procedure` (default), `framework`, `metrics`, `queries`, `meta-sources` |

`wot_guide` was added in phase 3. It serves `skills/wot-advisor/`, embedded with `go:embed`
(`skills/skills.go`). The default topic is `SKILL.md` without its frontmatter plus
`references/framework.md`, preceded by a paragraph that maps each CLI command to its tool.
Each other topic is one reference file, verbatim. The instructions tell the model to read it
before the first judgement. In evaluation run 5, it did so for every recommendation question.

`wot_sync` is the only tool that writes, and `wot_moe` and `wot_sync` are the only ones that
reach the network; the tool annotations say so. `auth wg` has no tool, because it opens a
browser and stores credentials; `wot_data_status` names the command to run instead.

Each tool validates its arguments with the same function as the CLI flag and calls the same
code, and its result is the CLI's output as one text block: the compact JSON envelope (§7.1),
or Markdown for the brief. There is no `structuredContent`, since clients that show both would
double the tokens. There is no second implementation of the query layer, and a test holds the
two surfaces to byte-for-byte equality.

A bad argument, or an error such as "no cache yet; run: wotctx sync", is returned as a tool
result with `isError` set, redacted like any CLI error, so the model can read it and act on it.
Tool calls run one at a time. In `mcp` mode the process's stdout is the protocol stream, so
anything a command would print goes to stderr instead.

The server's `instructions` carry a compressed version of the skill's procedure (§8) for clients
that have no skill: check status first, sync when stale, state data age and confidence, never
judge from one metric, and say what no API has.

## 10. Phase 3 — packaging

As built (plan phase 3). One package per surface, all built from this repository:

| Surface | Package | What it brings |
|---|---|---|
| Claude Code | the `wot` plugin: the repository root | the `wot:wot-advisor` skill, the wotctx MCP server, `/wot:sync`, the SessionStart hooks |
| Claude Desktop | `dist/wotctx-<version>.mcpb` (`make bundle`) | the twelve tools, `wot_guide` carrying the guide; a binary until phase 5, now a launcher (below) |
| claude.ai (phone) | `wotctx export claude-ai` | `wot-brief.md` for a Project, `wot-advisor.zip` for Skills |

**The plugin is the repository root.** `.claude-plugin/plugin.json` names it `wot`, and
`.claude-plugin/marketplace.json` lists it with source `"."`, so `skills/wot-advisor/` stays
the only copy of the skill. A plugin in a subdirectory could not reach it: component paths may
not leave the plugin root. Installed from the local directory, the plugin loads in place, and an
edit takes effect at the next session. Its parts:

- **MCP server**, declared inline in `plugin.json` (`wotctx mcp`). A root `.mcp.json` would
  double as this repository's project config.
- **`commands/sync.md`** (`/wot:sync [--only wg|wn8] [--full]`). It runs `wotctx sync` before
  the model sees the prompt, and asks for a report of at most four lines.
- **`hooks/hooks.json`**, on `SessionStart` with matcher `startup`, holding two exec-form
  `command` hooks. `mcp_tool` hooks are skipped at launch, because MCP servers are not up yet.
  - `wotctx sync --quiet`, `async: true`. It syncs in the background and prints nothing (§7.4).
  - `wotctx hook session-start`. It prints the cached brief, with a line saying it was not
    synced for this session, but only if the `inject_brief` option (`userConfig`, default off)
    is on. The plugin runs in every session, so it adds nothing to the context by default. It
    never fails: an error prints nothing and exits 0.

**The plugin runs `wotctx` from the PATH** and ships no binary. `wotctx auth wg` needs a
terminal and the binary anyway, and a plugin `bin/` would add a second copy for the Bash tool
only.

**The Desktop bundle ships its own binary.** `manifest_version` is `"0.3"` and
`server.type` is `binary`, with `${__dirname}/server/wotctx.exe mcp`. It targets Windows only;
`make release` still cross-builds for macOS. Its `tools` list must match the server's (a test
checks it). That makes two binaries, possibly of different versions, over one cache, so a
binary refuses a database whose `user_version` is newer than its own last migration (§4).

**Phase 5: the bundle carries a launcher, not a binary** (spec-desktop §7.1). Its
`server/wotctx-launcher.exe` finds the installed `wotctx.exe`, through `HKCU\Software\Tank
Advisor\InstallDir`, the default folder or the `PATH`. It runs it as `wotctx mcp`, with
inherited stdio, in a job object that ends it with the launcher. With no `wotctx` it serves a
single `wot_data_status` that says to reinstall the app. An app update then reaches Claude
Desktop at its next start, with no reinstall, and only one binary reads the cache.

**The framework reaches clients with no skill through `wot_guide`** (§9), not an MCP prompt or
resource. The model reaches for a tool unprompted; in Desktop, the user has to pick a prompt
or attach a resource.

**claude.ai has no shell**, so the skill there answers from an exported brief. The brief states
its generation time, and its relative ages count from that time.

## 11. Non-functional requirements

- **Secrets** never appear in the brief, logs, fixtures, or git. The WG `application_id`,
  `access_token` and its expiry live in the OS keychain, with `WOTCTX_WG_APPLICATION_ID`,
  `WOTCTX_WG_ACCESS_TOKEN` and `WOTCTX_WG_TOKEN_EXPIRES_AT` as an env fallback. A release build
  also carries the project's own application id, injected at build time from a repository
  secret (`-X …/internal/wg.builtinApplicationID`), so it is in no source file. A stored id
  overrides it (spec-desktop §12.1). All logging passes through one redaction helper, which
  covers both ids (and still redacts `tmgg_…` keys, from the Tomato.gg design, as a harmless
  precaution).
- **Account data leaves this machine only to Wargaming, and to Claude when the player asks
  it.** When the player asks Claude about their account, Claude receives the figures it needs
  to answer, under the player's own agreement with Anthropic (spec-desktop §12.3). The only
  other requests are XVM's expected-values file and tomato.gg's MoE page. Both are plain GETs
  of public pages that carry no account id, token or statistic, and neither client takes any
  account input. The app's update check goes to `api.github.com` and downloads from
  `github.com`, and carries no account data either. Every question-time request sends a
  `User-Agent` naming the project and its repository.
- **Tests use recorded, scrubbed fixtures.** No live calls in CI. A test greps `testdata/` for
  `tmgg_`, token and `application_id` patterns.
- **Dependencies:** `modernc.org/sqlite`, `gopkg.in/yaml.v3`, `github.com/zalando/go-keyring`,
  plus stdlib. Phase 2 adds the MCP SDK. HTTP clients and subcommand dispatch are hand-written:
  only ~7 endpoints are touched, and no upstream wrapper can then block a new WG `extra` value
  or a new tier. Phase 5 adds Wails v3, pinned to beta.26, and, through its notifications,
  `go-toast`. Both are in `cmd/tankadvisor` only: a test fails if `wotctx`, the launcher or
  `internal/app` links Wails.
- **Build:** `make build | test | lint | release | install | bundle | app | installer |
  plugin-validate`. `windows/amd64` and `darwin/arm64` must both build `wotctx` from one tree.
  (`make` over `just` because `make` is already present on the development machine; recipes
  assume a POSIX shell.) Releases are built by `.github/workflows/release.yml` (spec-desktop
  §9.1).
