# `wotctx` — build plan

Companion to `docs/spec.md`, which is the contract. This is the schedule.

## Status — 2026-09-25

Phase 1 was built on `phase-1-mvp` (28 commits) and fast-forwarded into `main` at `932f10b`.

| Step | State |
|---|---|
| 0 — manual setup | **done** except the WoT client mod, which moved to phase 4 |
| 1 — docs, scaffold, Makefile | **done** |
| 2 — config, secrets, redaction | **done** |
| 3 — WG client, OpenID login | **done**, live login verified |
| 4 — store, migrations, snapshots | **done** |
| 5 — encyclopedia, Tier XI probe | **done** — Tier XI **is** exposed; fallback unneeded |
| 6 — account, tank stats, achievements | **done**, figures checked against the client |
| 7 — WN8 from XVM expected values | **done**; account WN8 1822 over 14,618 random battles, awaiting a cross-check |
| 8 — overlay | **done**; stale entries confirmed and removed 2026-09-18 (WZ-113G FT and the Executor are owned) |
| 9 — query layer | **done**; all six queries answer from the live cache |
| 10 — brief | **done**; 4,558 characters on the live account |
| 11 — skill | **done**; fresh headless sessions invoke it unprompted (evaluation runs) |
| 12 — evals | **done**; run 1: 6/8, three causes fixed; run 2: 10/10 (docs/evals.md) |
| phase 2 — MCP | **done**, built on `phase-2-mcp` and fast-forwarded into `main` at `bc33519`: server built and tested; evals over MCP 7/10 without the skill, 10/10 with it; the owner confirmed it working in Claude Desktop on 2026-09-25 |
| phase 3 — packaging | **done**, built on `phase-3-packaging` and fast-forwarded into `main`: plugin installed (run 6: 10/10), framework over MCP (run 5b: 10/10), bundle installed in Desktop and confirmed working by the owner 2026-09-25; the phone export is built, not yet tried |
| phase 4 — client mod | **done**, built on `phase-4-mod` and fast-forwarded into `main` at `b9a8b9d` (2026-09-26): the mod in the client (0.2.1, checked against it by the owner; 0.3.0 built), `wotctx` reading it, answers from it; runs 7 and 8c 10/10. mod 0.3.0 and the Desktop bundle installed by the owner |
| phase 5 — Tank Advisor | **released as v1.0.0** on 2026-09-27, from the public repository `ondrejkouril/tank-advisor`; owner checks and D10 still open. Earlier: **in progress** on `phase-5-desktop`, in the worktree `../tank-advisor-phase5`. The spec was written, Wargaming's terms read and decisions D1–D9 settled with the owner on 2026-09-26. Wargaming and tomato.gg are contacted only if they raise something first. D1–D5 are done, with owner checks open on D1, D4 and D5. The switch-over happened on 2026-09-27: this branch's `wotctx` is installed, and so is the 0.5.0 launcher extension. D6, D7 and D8 were built 2026-09-27, each with its owner check open. D9 was built and tested locally the same day; releasing waits on D0 and on the repository going public. **D11's docs are done**; SignPath wants proof of users, so the first releases go out unsigned. The owner's advice settings are not in the overlay the installed `wotctx` reads (see D7). D0 (the owner's accounts) has not started. Next: the owner's checks, the rest of D0, and D10; then the 1.0 release |

**Phase 1 (MVP) is complete.** Its done-criterion — *"What tank should I get now?"* gets a
grounded, data-cited answer — is met in docs/evals.md, runs 1 and 2, from fresh sessions in an
empty directory, and `phase-1-mvp` is merged into `main`. The owner picked phase 2 (`wotctx
mcp`) next, and then phase 3 (packaging), which builds on it. Phase 4 (the client mod) stays
independent.

To re-run the evaluation after changing the skill: for each question in docs/evals.md, in an
empty directory, `claude -p "<question>" --output-format stream-json --verbose --allowedTools
"Bash(wotctx *)" "PowerShell(wotctx *)" Read Grep WebFetch Skill`, then grade the transcript.

Follow-ups (2026-09-18):

- **Done:** `wotctx meta moe <tank>` reads tomato.gg's full MoE table at question time (794
  tanks), and `wotctx query missions` reports classic personal-mission progress, synced with
  account/info at no extra request plus a weekly encyclopedia fetch (Campaign 1 only — the API
  describes no other).
- **Open — does Onslaught count in `all`?** It needs Onslaught battles between two syncs. Then,
  for the tanks played, compare the growth of `all` with `random` between those snapshots
  (`TankStatsBetween` for both modes): `all` growing faster by the number of Onslaught
  battles answers yes. Until then the framework (§4.10) forbids using `all − random` as an
  Onslaught figure.

State of the machine: Go 1.27.0 installed; `make` present, `just` absent; WG credentials in the
Windows credential store with the **access token expiring 2026-10-01** (`wotctx auth wg
--prolong` renews it, and `doctor` warns from the 28th); a synced database at
`%LOCALAPPDATA%\wotctx\wotctx.db` holding 8 sources (7 Wargaming, 1 XVM); `%APPDATA%\wotctx\config.yaml` setting only
`overlay_path` to this repo's `wot-overlay.yaml`.

Two corrections worth not re-learning: the garage is **37** vehicles, not the 48 ids
`private.garage` returns, and the API's premium fields are wrong. Both were caught only by
checking output against the game client, which is worth continuing to do at each step that
touches account data.

Verify the tree is intact with `make lint && make test` (no network required).

## Phasing

1. **MVP** — auth, sync, overlay, brief, query, skill used from a Claude Code project.
   Done when *"What tank should I get now?"* gets a grounded, data-cited answer.
2. **MCP** — `wotctx mcp` (stdio) exposing the query layer as tools; tested in Claude Desktop.
3. **Packaging** — Claude Code plugin (skill + MCP + `/wot:sync` + SessionStart sync hook),
   `.mcpb` bundle for Desktop carrying the framework as a tool, brief and skill export for
   claude.ai. Done when each surface answers from its package alone.
4. **Client mod.** A read-only mod that dumps what no API has. Done when XP, marks and
   loadouts in answers match the game client.
5. **Tank Advisor** (`docs/spec-desktop.md`). An installer and a tray app for any player, with
   advice each player customises. Done when a player with no developer tools goes from
   download to a grounded answer in under ten minutes, with no terminal.

## Step 0 — manual setup (human)

Nothing below can be verified until these are done.

| Task | Where | Produces |
|---|---|---|
| ~~Install Go 1.23+~~ | done: `winget install GoLang.Go` → **go1.27.0** | `go version` works |
| ~~Build runner~~ | done: GNU make already present (chocolatey); `just` is not installed, so the repo uses a `Makefile` | `make --version` |
| Register a **Mobile** application (the form's name for Standalone) | <https://developers.wargaming.net> → My Applications | `application_id` |
| ~~Create a Tomato.gg account and API key~~ | dropped: the API needs a paid key (spec §3.2) | — |
| ~~Install the Tomato.gg mod~~ | dropped with it; loadouts move to the phase 4 client mod | — |
| ~~Check profile / loadout visibility~~ | dropped with it | — |

Pick **Mobile**, not Server. The form offers only those two labels; Mobile is the standalone
type. Server applications allowlist up to five IP addresses and reject requests from anywhere
else, which does not survive a home connection's changing address. Mobile asks for no IP and
registers no redirect URI — "only the application_id is validated" — so `redirect_uri` is a
free parameter on `auth/login` and the local-callback flow works without registration.
`wotctx auth wg --manual` remains the fallback for a machine whose browser cannot reach
`127.0.0.1:8731`.

*Acceptance:* `go version` succeeds; after Step 2, `wotctx doctor` reports both credentials
present (presence only, never values).

## Step 1 — docs, scaffold, build targets

`docs/spec.md` and `docs/plan.md` land first, as their own commit. Then `go.mod`, `Makefile`
(`build`, `test`, `lint`, `release`, `install-skill`), `cmd/wotctx` with `version` and a stub
`doctor`, and a `.gitignore` covering `*.db`, `*.db-wal`, `*.db-shm`, `wotctx.exe`, `.env`.

The command tree is declared in full from the start — including commands later steps
implement — so `wotctx --help` always describes the whole intended surface, and an
unimplemented command fails with the plan step that will deliver it rather than an "unknown
command". `doctor` follows the same principle: it registers every check by name immediately,
reporting `unknown` with the owning step until that step replaces it, so the skill's staleness
probe has a stable shape and doctor never claims health it cannot verify.

Package layout, as built (the plan had `internal/tomato/`, dropped with Tomato.gg, and
`internal/mcpsrv/`, which phase 2 put in `internal/cli` instead; see "Phase 2"):

```
cmd/wotctx/main.go          # entry point
internal/cli/               # command tree, flags, output
internal/config/            # paths, thresholds, TTLs
internal/secrets/           # go-keyring + env fallback, redaction
internal/wg/                # Wargaming client: auth, account, tanks, encyclopedia, missions
internal/xvm/               # WN8 expected values
internal/meta/              # question-time fetches (tomato.gg MoE), never stored
internal/store/             # sqlite, migrations, snapshots, deltas
internal/overlay/           # YAML load + validate + name resolution
internal/syncer/            # orchestration, TTLs, re-parse
internal/wn8/               # the WN8 formula
internal/query/             # the query layer and its provenance envelope
internal/brief/             # Markdown renderer, 10k budget
internal/testseed/          # the seeded account the query and brief tests share
skills/wot-advisor/         # skill source of truth
testdata/fixtures/          # scrubbed recorded responses
testdata/golden/            # pinned query and brief output
```

*Acceptance:* `make build` produces `wotctx.exe`; `wotctx --help` lists every command in
spec §7; `make test` is green.

## Step 2 — config and secrets

Per-OS config and data paths. `config.yaml` holding confidence thresholds (spec §6.3) and
per-source sync TTLs. `internal/secrets` over `go-keyring` with `WOTCTX_*` env fallback, and a
single redaction helper that every log path uses.

*Acceptance:* `wotctx doctor` prints config and DB paths plus *presence only* of each secret; a
unit test asserts that an `application_id`, an access token, and a `tmgg_` key are all redacted
from log output.

## Step 3 — WG client and OpenID auth

Rate limiter self-capped at 5 req/s against the documented 10. Retry with backoff. ETag
conditional requests. Error mapping that reads `{"status":"error"}` out of **HTTP 200** bodies
(spec §3.1). `wotctx auth wg` implements the `nofollow=1` + local-callback flow; tokens
auto-prolong when fewer than 3 days remain.

*Acceptance:* a real login stores a token and `doctor` shows its expiry; `--prolong` extends it;
fixture tests cover `407 INVALID_APPLICATION_ID`, `407 DEMO_APPLICATION_IS_BLOCKED`,
`407 INVALID_ACCESS_TOKEN`, `504 SOURCE_NOT_AVAILABLE`, and a `304`.

## Step 4 — store, migrations, snapshot capture

Migrations, WAL, gzipped raw bodies in `snapshots`, `sync_runs` bookkeeping.

*Acceptance:* `sync --dry-run` writes nothing; a real sync creates `snapshots` rows whose
`raw_gz` round-trips to the original bytes; re-running inside the TTL produces a
`not_modified=1` row and no duplicate parsed data; migrations are idempotent across two runs.

## Step 5 — encyclopedia sync and the Tier XI probe

Page through `encyclopedia/vehicles`; populate `vehicles` and `vehicle_edges` from `next_tanks`
and `prices_xp`.

*Acceptance:* `vehicles` has >600 rows and `vehicle_edges` >400; `doctor` reports
`vehicles_by_tier`. **If the tier-11 count is 0**, add `research_paths:` to the overlay schema
and load those edges with `source='overlay'`. Either way `doctor` states which tiers came from
the API. Executor and Concept No. 5 resolve by name, or are explicitly reported as overlay-only.

**Result (2026-09-17): Tier XI is exposed.** 1028 vehicles, 838 edges, tier counts
`1:12 2:54 3:66 4:57 5:89 6:91 7:87 8:264 9:158 10:122 11:28`. The overlay `research_paths:`
fallback is therefore **not needed** — it stays in the schema as a mechanism for any future gap,
but no Tier XI line depends on it. Executor (26705, tier 11) and Concept No. 5 (19281, tier 10)
both resolve, and the API's 325,000 XP edge between them matches the overlay seed's
`xp_required` exactly. `next_tanks` and `prices_xp` agree on every edge checked.

## Step 6 — account and tank stats sync

`account/info` with the private and `statistics.random` extras; `tanks/stats` with
`extra=random`; `account/tanks` for `mark_of_mastery`; `tanks/achievements`. Re-verify the
spec §3.1 field lists against the live response and correct the spec if they differ.

*Acceptance:* the garage row count matches the in-game count; `tank_stats` covers garage plus
previously-owned tanks; credits/gold/bonds/free XP match the client at sync time; a second sync
minutes later produces a second snapshot with near-zero deltas, exercising the delta machinery
without asserting a trend.

## Step 7 — WN8 computed locally

**Replaces the original "Tomato.gg client and sync".** Their API requires a paid subscription
to issue a key, which was declined, so it is out of the design entirely (spec §3.2). Migration 3
drops the three `tomato_*` tables; `moe_progression` and `loadouts` survive because the planned
client mod is a genuine source for both.

Sync XVM's expected values (`static.modxvm.com/wn8-data-exp/json/wn8exp.json`, free, no key)
into `wn8_expected` on a patch-day TTL. Implement WN8 per spec §6.2: per-tank from cumulative
stats, account-level on the **battle-aggregate** rather than as an average of per-tank values,
and recent WN8 from a delta.

*Acceptance:* `wn8_expected` populated; per-tank WN8 computed for every tank that has expected
values; a tank **without** expected values is excluded from the account aggregate and reported
as excluded, not scored against zero; golden tests pin the formula against hand-checked figures;
the account aggregate differs from the mean of per-tank values, proving the aggregate is what
was implemented.

**Result (2026-09-18): done.** Account WN8 **1822** over 14,618 random battles on 219 tanks,
none excluded, from XVM expected values version 2026-09-12 (992 vehicles). Three things were
wrong in the plan as written, all caught by looking at live data first:

- **The source URL was dead in all but name.** `…/json/wn8exp.json` answers 200 but froze at
  2024-09-12 when XVM split Wargaming from Lesta; the current file is `…/json/wg/wn8exp.json`.
  The old one lacked the Executor, Leox and Prototipo 6. `doctor` now warns on a version stamp
  over 60 days old, because a frozen source never stops returning 200.
- **Defence points were never parsed.** `dropped_capture_points` is WN8's `def` and was not in
  `tank_stats`. Migration 4 adds it with a `parser_version` column, and `sync` re-parses older
  rows from their stored raw bodies — 876 rows across 2 snapshots, zero requests — which is the
  "re-parse, never re-fetch" design paying off for the first time.
- **The spec's combination formula omitted the win-rate term.** Corrected in spec §6.2; the
  golden tests are pinned to figures computed by an independent transcription.

A side finding for step 9: the `random` block carries assist as totals after all (spec §3.1
correction). Stored, not yet used, pending a check against the in-game service record.

`doctor`'s data-age check also now honours per-source TTLs: the encyclopedia at nine hours
was warning against the 6 h threshold though `sync` would not refetch it for seven days — a
warning no command could clear.

### What was lost, and how it is covered

| Lost with Tomato.gg | Covered by |
|---|---|
| WN8 | this step |
| Recent-form windows, sessions | local snapshot deltas (step 4/6, spec §6.1) |
| MoE progression | nothing until the mod phase; requirement tables fetched at question time |
| Loadouts, battle logs | nothing until the mod phase |
| Map stats, rankings, WNX | dropped |

**No history can be backfilled.** Wargaming's `ratings/*` endpoints are discontinued
(`RATINGS_NOT_FOUND`, measured), so day zero is the first sync. Windowed questions must answer
"not yet — N days of history" until enough accrues. This makes syncing load-bearing (spec §7.4):
a `SessionStart` hook plus the skill's freshness gate, with a scheduled task offered as an
optional third mechanism.

## Phase 2 — `wotctx mcp`

Goal: Claude Desktop (and any other MCP client) answers from the same cache through the same
query layer, with nothing re-implemented. The phase is done when Claude Desktop, with the
server registered, gives *"What tank should I get now?"* the same grounded, data-cited answer
that phase 1 gives in Claude Code.

Decisions taken before building:

- **SDK:** `github.com/modelcontextprotocol/go-sdk` **v1.8.0** (spec §9 said v1.x). Stdio
  transport only. There is no HTTP server, so nothing listens on a port.
- **The server lives in `internal/cli`, not in a new `internal/mcpsrv`.** Every tool needs what
  `Env` already holds (config, paths, secrets, the clock, the redactor), and `doctor` and
  `sync` are assembled there. A separate package would need all of that passed in through an
  interface with only one implementation. `internal/query` stays the only reader of the
  database.
- **One code path per operation.** CLI flag parsing and MCP argument decoding both end in the
  same validation function and the same `query.Service` call, so the two surfaces cannot
  drift. Tests pin that each tool's text matches the CLI's output byte for byte.
- **Output is the CLI's compact JSON envelope as one text block**, not `structuredContent`.
  Clients that support both often show both, which doubles the tokens, and the envelope
  already carries its own provenance. `wot_brief` returns the Markdown brief.
- **Two tools beyond spec §9**, because phase 1 added their commands after the spec was
  written: `wot_missions` and `wot_moe`. That makes eleven in all.
- **Tool calls run one at a time**, behind one mutex. A sync and a query racing on SQLite
  would probably be fine under WAL, but "probably" is not worth throughput nobody needs.
- **Stdout belongs to the protocol.** In `mcp` mode `env.Stdout` points at stderr, so a stray
  print can only reach the client's log. It can never corrupt a JSON-RPC frame.

| Tool | Wraps | Hints |
|---|---|---|
| `wot_data_status` | `doctor --json` | read-only |
| `wot_brief` | `brief` | read-only |
| `wot_garage` | `query garage` | read-only |
| `wot_tank` | `query tank` | read-only |
| `wot_performance` | `query performance` | read-only |
| `wot_resources` | `query resources` | read-only |
| `wot_sessions` | `query sessions` | read-only |
| `wot_candidates` | `query candidates` | read-only |
| `wot_missions` | `query missions` | read-only |
| `wot_moe` | `meta moe` | read-only, open-world (fetches tomato.gg) |
| `wot_sync` | `sync` | **mutating**, open-world, idempotent within TTLs |

`auth wg` is deliberately **not** a tool. It opens a browser and stores credentials, which
belongs in a terminal the user is watching. `wot_data_status` names the command instead.

### Step M1 — share the code paths

Move argument validation out of the flag closures in `query.go` into plain functions. Split
`runSync` into a `sync` that returns a `syncer.Report` and a printer. The missing-token
warning becomes a report caveat instead of a bare stderr line, so an MCP client sees it too.

*Acceptance:* no other behaviour change. `make test` is green with the existing goldens
untouched.

### Step M2 — the server

`wotctx mcp` builds an `mcp.Server` with the eleven tools, typed inputs with `jsonschema`
descriptions, and the annotations in the table. Its `instructions` carry the rules that an
MCP client has no skill to learn from: check `wot_data_status` first, sync when stale, cite
data age and confidence, and never give a verdict from one metric.

*Acceptance:* an in-memory client lists exactly the eleven tools. Each read-only tool, called
against the seeded account, returns the same text as its CLI counterpart. Bad arguments come
back as tool errors (`isError`), not protocol errors. With no cache, every query tool's error
names `wotctx sync`. Nothing but protocol frames reaches stdout.

**Result (2026-09-18): M1 and M2 done.** `internal/cli/mcp.go` and six tests in
`mcp_test.go`, all green. Fifteen tool calls match their CLI commands byte for byte, and
`wot_data_status` matches `doctor --json` field for field. Fourteen bad-argument cases come back
as `isError`, including a schema-level type error (`tier: "nine"`). A smoke test of the real
binary over stdio against the live cache initialised, listed the tools, answered
`wot_resources` and `wot_data_status`, wrote nothing to stderr and exited 0 when stdin closed.
With every command now implemented, `pending()` and its test are gone. The SDK brought seven
indirect dependencies: jsonschema-go (which does the input validation), uritemplate, oauth2,
x/sync, x/time and segmentio's encoding and asm.

### Step M3 — run it for real

Register the server in Claude Desktop (`claude_desktop_config.json`: command is the installed
`wotctx.exe`, args `["mcp"]`) and in Claude Code (`claude mcp add wotctx -- wotctx mcp`).

*Acceptance:* both clients list the tools. `wot_data_status` and `wot_resources` show the live
account, with the figures checked against the game client (the standing rule).

### Step M4 — evaluation in Desktop

Desktop has no `wot-advisor` skill unless it is uploaded, so run docs/evals.md there twice:
once with only the server instructions, and once with the skill uploaded (Settings →
Capabilities → Skills). The difference shows whether phase 3 has to ship the framework
through MCP itself, as a prompt or resource, or whether the skill is enough.

*Acceptance:* at least 8 of 10 questions pass with the skill uploaded. The no-skill run is
recorded either way, and its failures are filed as input to phase 3.

### Step M5 — docs

Rewrite spec §9 to match what was built, add a Claude Desktop section to the README, and
record results in this section.

**Result (2026-09-18): M3–M5 done, apart from two checks only the owner can make.**

- **M3.** `make install` put the new binary on the PATH. The server is registered in Claude
  Code at user scope (`claude mcp list`: connected) and in `%APPDATA%\Claude\
  claude_desktop_config.json` (backed up first). Desktop reads that file only at launch, and
  was not restarted from inside a session that may itself be running in Desktop. **Owner:**
  restart Desktop and check that the wotctx tools appear. Then check the figures the tools
  returned against the game client: 1,381,031 credits, 3,373 gold, 11,561 bonds and 41,294
  free XP, as synced at 11:53 UTC.
- **M4.** Run 3 (MCP, no skill) scored 7/10 and run 4 (MCP plus the skill) scored 10/10; see
  docs/evals.md. They ran headless with only the MCP server loaded and no shell, which is
  Desktop's situation, rather than in Desktop's UI. The skill needed no change for MCP: the
  model mapped each CLI command it names to the matching tool. Without the skill, the data is
  right but the judgement is weaker (format, tier bands, one mark target). **Phase 3 input:**
  the `.mcpb` bundle must bring the framework with it, either as an MCP prompt or resource or
  as an instruction to upload the skill.
- **M5.** Spec §9 now describes what was built. The README has a Claude Desktop section and
  an updated status.

## Phase 3 — packaging

Goal: installing the advisor is a package, not a procedure. Today it is four hand steps
(`make install`, `make install-skill`, `claude mcp add`, editing `claude_desktop_config.json`),
and in Desktop, where there is no skill, the advice is weaker (run 3: 7/10). The phase is done
when:

- in Claude Code, the installed plugin alone gives *"What tank should I get now?"* a grounded,
  data-cited answer, and a sync happens at session start without anyone asking;
- in Claude Desktop, the installed `.mcpb` bundle alone does the same, with **no skill
  uploaded**, because the framework travels inside the server;
- on claude.ai (phone), an uploaded brief and skill answer from the brief and say how old it is.

Decisions taken before building (checked against the current Claude Code plugin reference,
hooks reference and MCPB manifest spec on 2026-09-25):

- **The repository root is the plugin.** `.claude-plugin/plugin.json` and
  `.claude-plugin/marketplace.json` sit at the root, and the marketplace entry's source is
  `"."`. That keeps `skills/wot-advisor/` the single source of truth: a plugin in a
  subdirectory could not reach it, since component paths may not leave the plugin root and
  symlinks out of it are rejected. The owner installs it as a **local-directory marketplace**
  (`claude plugin marketplace add C:\Git\tank-advisor`), which loads the plugin in
  place, so an edit to the skill takes effect at the next session with no version bump and no
  `make install-skill`. The same marketplace also works from GitHub, where it is copied into
  the plugin cache instead.
- **The plugin is named `wot`.** Components are namespaced under it: `/wot:sync`,
  `wot:wot-advisor`. The command file is `commands/sync.md` (spec §10's `wot-sync.md` would
  have made it `/wot:wot-sync`).
- **The plugin does not ship the binary.** It runs `wotctx` from the PATH, put there by `make
  install`. `wotctx auth wg` has to be run in a terminal anyway, so the binary on the PATH is a
  prerequisite whatever the plugin does. A plugin `bin/` would put a second copy on the Bash
  tool's PATH only, beside the first, writing to the same database. The plugin checks for
  the binary instead: the hook stays silent without it, and the skill already names the
  install command.
- **The MCP server is declared inline in `plugin.json`**, not in a root `.mcp.json`. At the
  repo root that file would double as this repository's project MCP config, and prompt for
  approval in every session opened here.
- **The Desktop bundle does ship the binary** (`server/wotctx.exe`, `manifest_version`
  `"0.3"`, `type: "binary"`), because a one-click install is the point of the bundle. Windows
  only: macOS still cross-builds (`make release`), but nobody runs it. Two binaries, of
  possibly different versions, then share one database. **An older binary must therefore
  refuse a newer schema** instead of reading tables it does not know (step P1).
- **The framework travels through MCP as a tool, `wot_guide`**, not as a prompt or a
  resource. In Desktop, the user has to pick a prompt from a menu and attach a resource by
  hand. A tool is the only thing the model reaches for unprompted, the way it reaches for a
  skill. `wot_guide` returns the skill's procedure and `references/framework.md`, embedded in
  the binary from `skills/wot-advisor/` with `go:embed` (from a `skills/skills.go`, since an
  embed may not reach above its package), so there is still one copy of the text. The
  framework is 22 KB, too much to put into the server's `instructions` in every Desktop
  conversation whether or not it is about tanks. The tool's own description says to call it
  before advising, because not every client shows `instructions` to the model.
- **The SessionStart hook syncs, in the background, and says nothing.** It is `async: true`,
  so session start never waits on Wargaming, and it matches `startup` only (not resume,
  clear, compact or fork). The plugin is installed user-wide, so the hook fires in every
  Claude Code session, including ones that have nothing to do with tanks, which is why it
  adds nothing to the context. Recording history is what the hook is for (spec §7.4); the
  skill's freshness gate still runs before any answer. Per-source TTLs keep repeat starts
  cheap: a start within the hour makes no request.
- **Injecting the cached brief at session start is available but off** (plugin `userConfig`
  `inject_brief`, default `false`), for the same reason: 4.5 KB in every coding session is the
  context bloat the brief's cap exists to prevent.
- **Syncs take a lock.** Two sessions starting together, or a hook and `wot_sync` at once,
  would otherwise each find the data stale, and both would fetch and store a snapshot. That
  leaves a zero-length interval in `query sessions`. The second sync waits briefly, then
  reports that another sync is running and exits 0.
- **Not in this phase:** a scheduled task (spec §7.4's optional third sync mechanism) is not
  packaging. It is offered as a follow-up once the hook's coverage has been seen.

### Step P1 — the binary, ready to be packaged

- `wot_guide` (read-only, no network): `topic` is `procedure` (the default: `SKILL.md`'s
  procedure plus the framework), `metrics`, `queries` or `meta-sources`. It prepends one
  paragraph that maps each `wotctx …` command the text names to its tool. The server's
  `instructions` gain step 0: call `wot_guide` before the first recommendation. That makes
  twelve tools.
- `wotctx sync --quiet`: on success, prints nothing; on failure, one line to stderr.
- A cross-process sync lock, taken in the store, so the MCP server and the CLI share it.
- `store.Open` refuses a database whose `user_version` is newer than the binary's last
  migration, and names the fix (update this `wotctx`).

*Acceptance:* `make test` green. The existing goldens are untouched, apart from the tool list.
An in-memory client lists twelve tools, and `wot_guide` returns the framework text byte for
byte from the embedded copy. Two concurrent syncs against one database store one snapshot per
source. A database stamped one version ahead fails to open with the named fix. `sync --quiet`
prints nothing on success.

**Result (2026-09-25): done.** The lock is the `sync_runs` row itself: `BeginSyncRun` inserts
only if no unfinished run younger than five minutes exists, in one statement, so there is no
new table. It compares with `julianday()`, because RFC 3339 with nanoseconds is not
fixed-width and does not sort as text. The test proves this: it fails with a string
comparison. A run that stops early, whether cancelled or failing to write, is now closed as not
ok instead of being left open, where it would block every sync for five minutes. A sync that
finds the lock taken waits up to a minute, then finds every source fresh. Checked against the
live cache: of two syncs started together, one fetched and the other waited and then skipped
all eight sources. Tests (mutation-checked: they fail with the lock disabled) cover the
overlap, giving up, cancellation and the newer-schema refusal. `wot_guide` returns 28.6 KB
from the embedded skill, and a stdio smoke test of the real binary lists twelve tools.

### Step P2 — the framework over MCP, evaluated

Run the ten questions of docs/evals.md again as **run 5**: MCP only, no skill, no shell, the
setup of run 3 (7/10). The framework now reaches the model only through `wot_guide`.

*Acceptance:* at least 9 of 10 pass, and every run-3 failure (answer format, tier bands, one
mark target) passes. If the model does not call `wot_guide` unprompted, the fix goes in the
tool's description and the instructions, not in the questions.

**Result (2026-09-25): done.** Run 5 scored 8/10. It was first graded 9; a second misreading
turned up later, while grading run 6. After the fixes, run 5b scored 10/10 (docs/evals.md). The
three run-3 failures pass. `wot_guide` was read unprompted before every recommendation, and
skipped for the factual questions. Both run-5 failures were factual answers that filled a gap
by guessing: an invented claim about what the overlay holds, and the Mastery Badge scale.
Both fixes went where every session sees them, the instructions and the tool descriptions. The criteria date from 2026-09-18, and the account has
moved on: the Šelma is bought, the Blesk sold, the Kranvagn owned. The overlay has not been
updated to match, and the sessions noticed. **Owner:** update `wot-overlay.yaml` (goals,
`researched_not_bought`, banked XP towards the Tesák).

### Step P3 — the Claude Code plugin

`.claude-plugin/plugin.json` (name, description, author, inline `mcpServers.wotctx` →
`wotctx mcp`, `userConfig.inject_brief`), `.claude-plugin/marketplace.json`, `hooks/hooks.json`
(the async sync, plus the brief injection, which checks `CLAUDE_PLUGIN_OPTION_INJECT_BRIEF`),
and `commands/sync.md` (runs `wotctx sync` and reports what changed, with
`allowed-tools: Bash(wotctx sync*)`). Makefile target `plugin-validate` runs
`claude plugin validate .`.

Then migrate this machine from the hand install to the plugin: add the local marketplace,
install `wot@tank-advisor` at user scope, then remove the user-scope `wotctx` MCP
registration and `~/.claude/skills/wot-advisor`. Left in place, each would sit beside the
plugin's copy, as a second skill and a second set of eleven tools. `make install-skill` is
retired from the README.

*Acceptance:* `claude plugin validate .` passes. `claude plugin list` shows `wot` enabled. A
fresh session lists `/wot:sync`, the `wot:wot-advisor` skill and the wotctx tools, each once.
Starting a session with stale data leaves a new `sync_runs` row, with nothing added to the
context. Run 6 is docs/evals.md asked in an empty directory with only the plugin
(`--plugin-dir`), and it passes at least 9 of 10.

**Result (2026-09-25): done.** `claude plugin validate` passes for both manifests. The one
warning, no `version`, is deliberate: an in-place plugin ignores it, and a GitHub install tracks
commits. The plugin is installed at user scope, and the hand-installed skill (backed up first,
identical but for the new claude.ai section) and the user-scope MCP registration are removed. A
fresh session lists the twelve tools, `/wot:sync` and `wot:wot-advisor` once each, and one MCP
server. Both SessionStart hooks start and exit 0, and the database was written during the
session: the background sync ran. `/wot:sync` pre-runs the command and makes no tool call.
Run 6 used exactly the plugin and scored 10/10.

What had to be added: `wotctx hook session-start` (the brief hook; exit 0 whatever happens) and
`sync --quiet`. Installing the new binary on Windows needed the running one renamed first,
because Desktop and the Claude sessions hold it open; the README says so.

### Step P4 — the Desktop bundle

`packaging/mcpb/manifest.json` (`manifest_version` `"0.3"`, `server.type` `binary`,
`entry_point` `server/wotctx.exe`, args `["mcp"]`, `compatibility.platforms` `["win32"]`, the
twelve tools declared statically, an icon). `make bundle` builds the Windows binary into a
staging directory and packs it with `npx @anthropic-ai/mcpb pack`, after `mcpb validate`, into
`dist/wotctx-<version>.mcpb`. The bundle's version comes from `BUNDLE_VERSION` (starting
`0.3.0`). `dist/` is already ignored.

*Acceptance:* `mcpb validate` passes, and the bundle unpacks to the manifest plus one binary
that answers `version`. **Owner:** install it in Desktop by double-clicking the file. Remove
the hand-written `wotctx` entry from `claude_desktop_config.json`, which has a backup, so the
tools appear once. Then ask *"What tank should I get now?"* with no skill uploaded.

**Result (2026-09-25): done.** The owner removed the hand-written config entry, installed the
bundle from Settings → Extensions, and confirmed it works in Desktop. Double-clicking does
nothing, because Windows has no file association for `.mcpb`. `make bundle` validates and packs `dist/wotctx-0.3.0.mcpb`, 6.0 MB: the manifest and a 14.4 MB binary. Unpacked, the
binary answers `version` and serves the twelve tools over stdio. A test holds the manifest's
tool list equal to the server's. Nothing in the bundle is new code: run 5b is the evaluation
of exactly this server without a skill, driven headless rather than in Desktop's window. No
icon yet: the manifest field is optional.

### Step P5 — claude.ai (phone)

The skill gains a short *no `wotctx`* branch. When the command cannot run (claude.ai,
mobile), it answers from the brief attached to the Project, states the brief's generation
time as the data age, and says which questions need the computer (anything beyond the brief).
`make claude-ai` writes `dist/claude-ai/wot-brief.md` (the brief, with its generation time
in the first line) and `dist/claude-ai/wot-advisor.zip` (the skill, for Settings → Capabilities
→ Skills).

Open question for the owner, before this step: a copy of the skill is already uploaded to
claude.ai. It also appears in Claude Code as `anthropic-skills:wot-advisor`, beside the local
copy, and will not follow edits made in this repo. Keep it and re-upload the zip after each
skill change, or remove it and rely on the plugin (Code) and `wot_guide` (Desktop)? The phone
needs it either way.

*Acceptance:* the zip contains `wot-advisor/SKILL.md` and its references, under claude.ai's
upload limits. **Owner:** in a claude.ai Project holding the brief, *"How fresh is my data?"*
answers with the brief's own timestamp, and *"What tank should I get now?"* answers from the
brief and names what it could not check.

**Result (2026-09-25): built; the claude.ai check is the owner's.** It became a command, not
a make target: `wotctx export claude-ai [--out DIR]` writes `wot-brief.md` and
`wot-advisor.zip`. The zip is built from the skill embedded in the binary, so it needs no zip
tool and works from any directory. The brief already opens with its generation time. The new
skill section tells the model that the brief's "(… ago)" ages count from that time, not from
now. Tests check the zip's layout and that the exported brief equals `wotctx brief`. The open
question above is still the owner's.

### Step P6 — result

**Done 2026-09-25.** Spec §10 describes what was built, §7.4 the async hook and the lock, §9
`wot_guide`, and §7 the new commands. The README covers the plugin, the bundle and the export,
and keeps the hand install as a fallback. docs/evals.md holds runs 5, 5b and 6.

### Step P6 — docs

Rewrite spec §10 to match what was built (and §7.4 for the async hook). Replace the README's
setup steps 1 and the Claude Desktop paragraph with the plugin and bundle installs, keeping the
hand install as a fallback. Record runs 5 and 6 in docs/evals.md and the results here.

## Phase 4 — `wotctx` client mod

Deferred until the MVP answers end to end, so nothing is blocked on it and a patch that breaks
the mod degrades the tool instead of stopping it.

A read-only Python 2.7 `.wotmod` that, on garage load, writes one JSON file locally: resources
including **real premium/WoT Plus status**, and per vehicle its **XP**, **marks**, loadout and
crew. No network, nothing in battle. `wotctx` reads it as a third source (`mod:`) with its own
capture timestamp and a staleness caveat.

This closes every remaining gap: per-vehicle XP and Marks of Excellence exist in no API at all,
the API's premium fields are wrong, and `private.garage` over-reports. As each field arrives,
the corresponding hand-maintained overlay field is retired.

The phase is done when *"Should I spend free XP on the Tesák?"*, *"What's fitted on my TVP
T 50/51?"* and *"Which Tier X should I push for marks?"* are answered from the player's real
XP, loadout and mark percentage, with each figure matching the game client.

### What the machine and the client say (checked 2026-09-25)

- **Client:** EU `v.2.4.0.1 #952`, at `C:\Games\World_of_Tanks_EU`, with Aslain's modpack,
  XVM and eight other `.wotmod` files in `mods\2.4.0.1\`. It embeds `python27.dll`, and it
  writes `python.log` in the game folder, where a mod's errors show up.
- **The loader** (`scripts/client/gui/mods/__init__.py`) imports only `mod_*.pyc` from
  `scripts/client/gui/mods/`. `.py` is accepted only in development builds. So the mod ships
  **compiled Python 2.7 bytecode**, and this machine has no Python at all.
- **Packaging**, as every installed mod does it: a zip with **no compression** (all "Stored"),
  holding `meta.xml` (`id`, `version`, `name`, `description`) and
  `res/scripts/client/gui/mods/mod_*.pyc`.
- **Reference source:** `IzeBerg/wot-src`, branch `EU`, a bot-decompiled copy of the client
  whose latest commit is `v.2.4.0.1 #952`, the installed build exactly. Everything below comes
  from it. Nothing in it is copied into this repository; it is read as documentation.

| Needed | Where the client has it |
|---|---|
| Owned vehicles | `IItemsCache.items.getVehicles(REQ_CRITERIA.INVENTORY)`. `intCD` is the API's `tank_id` |
| Vehicle XP | `Vehicle.xp`, and `isElite` |
| Mark count and percentage | the vehicle dossier: `getRecordValue(TOTAL, 'marksOnGun')`, `'damageRating'` / 100 (the % shown in the garage), `'movingAvgDamage'` (the average the mark is built from) |
| Loadout | `Vehicle.optDevices`, `.shells`, `.consumables`, `.battleBoosters` (directives), each `.installed`, and `setupLayouts` for the switchable setups |
| Crew | `Vehicle.crew`, as (slot, `Tankman`) pairs; `Tankman.role`, `.skills`, `.bonusSkills` |
| Resources | `items.stats`: `credits`, `gold`, `crystal` (bonds), `freeXP` |
| Premium | `items.stats.isPremium`, `activePremiumType`, `activePremiumExpiryTime`, computed by the client itself |
| WoT Plus | `IWotPlusController.hasSubscription()` |
| When to write | `IItemsCache.onSyncCompleted`, which fires when account data has loaded or changed |

Decisions:

- **Toolchain: Python 2.7.18**, from winget (`Python.Python.2`). It compiles the mod
  (`py_compile`, with the same magic number as the client's 2.7), packs it (`zipfile`,
  `ZIP_STORED`), and runs the mod's unit tests. Python 2 is end-of-life, but so is the client's
  interpreter, and this is the only way to produce bytecode the client accepts. **The owner
  decides this before step C1.**
- **The mod is one file, `mod/mod_wotctx.py`, split in two.** A thin adapter does the client
  imports lazily and reads the objects. A collector turns them into plain dicts, and that part
  is tested without the game, against fakes. Mod id `ondrejkouril.wotctx`; the package is
  `ondrejkouril.wotctx_<version>.wotmod`.
- **It writes `%LOCALAPPDATA%\wotctx\mod\garage.json`**, beside the cache and not in the game
  folder, which Aslain's installer manages. The write is atomic (temporary file, then rename),
  and the JSON carries `schema`, `captured_at`, `game_version`, `mod_version` and the account id.
- **It writes only in the lobby**: on `onSyncCompleted`, when the player is a `PlayerAccount`
  (in battle it is an `Avatar`), at most once every 10 seconds. It has no network code, no
  input handling and no UI. Every exception is caught and logged as one line in `python.log`,
  so a patch that breaks it cannot break the client.
- **`wotctx` reads it as source `mod:garage`**: stored like any other snapshot (the raw file
  gzipped), then parsed. A `sync` picks it up, and it is not a request, so it has no TTL. A
  dump older than the latest sync of `wg:account/info` is reported as stale data, not used as
  current.
- **Game updates:** the mod lives in `mods\<game version>\`, and each update makes a new,
  empty folder. `wotctx mod install` copies the package into the current one, found in the
  game's `version.xml`. `doctor`'s `mod-data` check warns when the installed game version has
  no copy of the mod, or when the last dump came from an older version.

### Step C1 — toolchain and a mod that proves it loads

Install Python 2.7.18. `mod/mod_wotctx.py` at first writes only a heartbeat: the dump's header
fields, the account id, and resources. `make mod` compiles and packs
`dist/ondrejkouril.wotctx_<version>.wotmod`. `wotctx mod install [--game-dir DIR]` copies it into
`mods\<version>\`, and the config gains `game_dir` (default `C:\Games\World_of_Tanks_EU`).

*Acceptance:* the package lists as all "Stored", with `meta.xml` and one `.pyc`. The collector's
tests pass under Python 2.7. **Owner:** start the game and wait in the garage. `garage.json`
then exists, with credits, gold, bonds and free XP matching the client, and `python.log` holds
the mod's one "loaded" line and no traceback.

### Step C2 — the full garage dump

Per owned vehicle: `tank_id`, XP, elite, marks, mark percentage, moving-average damage, the
installed equipment, shells (with counts), consumables, directives, setups, and the crew with
skills. Also premium and WoT Plus. Field modifications are left out of the first version: their
client API is the least stable, and no current question needs them.

*Acceptance:* collector tests cover each field against fakes. **Owner:** after a garage load,
four spot checks against the client, the standing rule. The Šelma's XP. The Object 140's mark
percentage. The TVP T 50/51's equipment and shells. Premium and WoT Plus both "yes".

**Result (2026-09-25): C1 and C2 done, checked against the client by the owner.**

- **Every figure matched the client.** Resources (1,884,086 credits, 6,347 gold, 12,169 bonds,
  253,317 free XP), premium until 16 Oct, WoT Plus, the Šelma's 90,237 XP, the Object 140's one
  mark at 75.15 %, and the TVP T 50/51's three pieces of equipment, three shell types (28/20/0),
  three consumables and no directive.
- **The client is Czech**, so each item's `user_name` is Czech. `wotctx` keys on the technical
  `name` (`ussr:R97_Object_140`), which does not change with language.
- **The first build in the client (0.2.0) never wrote a dump.** The lobby's first event arrives
  before the account has synced. A ten-second throttle spent its one turn on that event, then
  dropped the sync event that followed. This happened on all eleven returns to the garage in
  `game.log`, silently. 0.2.1 schedules one write three seconds after each burst of events, and
  a write that finds the data not ready costs nothing. It logs each write and each distinct
  reason for skipping, and a test replays the sequence. The log was `game.log`, not
  `python.log`: `python.log` had stopped at 1 September.
- **The dump explains the API's 16 "undescribed" garage ids.** The client lists 60 vehicles:
  eight tier I Steel Hunter vehicles (technical names ending `_SH`) and five event rentals
  (flagged `rented`) besides the tanks. Thirteen tanks, the TVP among them, had no crew. The
  owner put one into the TVP during the check, and the mod rewrote the file within seconds.
- **Setups are not dumped** (only what is fitted now), and neither are field modifications.
  Both are left for later.

### Step C3 — `wotctx` reads it

Migration 6: `mod_vehicles` (xp, elite, marks, rating, moving average) and `mod_crew`.
Loadouts use the existing `loadouts` table, and the mark figures also go into
`moe_progression`. `sync` stores `mod:garage` when the file is newer than the last stored copy.
`doctor`'s `mod-data` check turns from `unknown` into real results: the dump's age, its game
version against the installed one, and whether the mod is installed.

*Acceptance:* golden tests from a scrubbed `garage.json` fixture. A dump from before the
current game version is reported as stale, not served. Deleting the file degrades to exactly
today's behaviour.

**Result (2026-09-25): done.**

- **Parsing.** `internal/mod` parses the dump. A field the mod could not read stays nil, not
  zero, and an unknown schema is refused.
- **Storage.** Migration 6 adds `mod_account`, `mod_vehicles` and `mod_crew`, and gives
  `loadouts` the item's client name, a shell's kind and its count.
- **Sync.** The `mod:garage` source stores a dump only when its capture time is newer than
  the last one kept, with the snapshot's `requested_at` set to that capture time. It refuses
  another account's dump, and turns the mod's own read errors into a caveat. With no file,
  a sync is exactly what it was. `--only mod` makes no request, and `--only wg` does not read
  the file.
- **`doctor`.** The data-age check ignores the dump, which ages whenever the game is closed
  and which no sync can refresh. `mod-data` judges it instead, and warns in each of these
  cases:
  - battles were played after it (compared with Wargaming's last battle time);
  - it predates the installed game version;
  - the mod is missing from the current `mods/<version>/` folder;
  - a newer dump is not synced yet;
  - the mod recorded read errors.
- **Tests.** The fixture is a real dump cut to five vehicles. It carries the figures the owner
  checked, and the tests check them at every layer.
- **Checked live:** `sync` stored the real dump of 60 vehicles, and `mod-data` reads `ok`.

### Step C4 — answers from it

`query tank` gains `vehicle_xp`, `marks` and `loadout`, `query resources` takes premium and WoT
Plus from the mod, and `query garage` uses the mod's true vehicle set. Each field carries the
dump's age in `meta.sources`. `candidates` uses the real XP on the `via` tank instead of the
overlay's `xp_banked`. `meta moe` shows the player's mark percentage beside the thresholds. The
brief's Known gaps shrink to what is still missing. The skill, the framework and
`wot_guide` stop saying "not in any API" for fields the mod supplies, and instead name the
dump's age. The overlay's `xp_banked` and `premium` become fallbacks, used only without a
fresh dump, and `overlay validate` says so.

*Acceptance:* goldens updated and reviewed figure by figure; with no dump, every answer is
unchanged from today.

**Result (2026-09-25): done.**

- **Without a dump, nothing changed.** Every query golden is unchanged. The brief's golden
  changed in one reviewed line: the gap now says the client mod supplies those figures when
  installed, not that it is planned.
- **Tests with a dump.** Eight query tests on a seeded dump, and one for the brief. They cover
  premium from the client, where a Premium Account that expired after the dump reads as
  inactive, and rentals left out. They also cover the `client` block on `query tank`, goal XP
  from the game with its own caveat, research known for every candidate, the "played since"
  caveat, and marks in `meta moe`.
- **Mod 0.3.0 adds the research list.** A vehicle's compact descriptor has type 1 in its low
  four bits, so the client's unlock set filters down to vehicles. Migration 7 stores the list,
  and records whether a dump had one at all. "Researched, not bought" now means researched,
  not owned and not premium, at tier VIII and up. That was changed after the first real list
  (2026-09-26). The client lists every vehicle ever researched, 892 in all, and the first
  rule ("never played") left 88, 70 of them below tier VIII: starters and tanks researched past.
  It also hid the Object 277, a tier X researched but sold, which can be bought back with no
  XP.
- **The final rule** lists tier VIII+ vehicles that are researched, not owned and not premium,
  but leaves out a sold tank whose next step was also researched: that one was passed through
  on the way up. A sold tank where the line stopped stays, with `played_before`. On the live
  account that leaves 13: nine never owned, plus the Object 277 and three other sold tanks. A
  caveat counts the other 244 (200 below tier VIII, 44 passed through). One of the 13, the AMX
  30 1er prototype, is there only because the API's tech tree has no links for the AMX 30 line.
- **Rentals.** The dump showed that the API counts two event rentals (the Leox and the TS-54)
  as garage vehicles. With a dump they are left out of the garage, with a caveat naming them.
- **The overlay's premium, `xp_banked` and `researched_not_bought` are now a backup.**
  `overlay validate` says so when a dump supplies them. Preferences, goals and notes are still
  read from the overlay: no data can supply what the player intends.
- **Text the model reads.** The skill, framework (data sources only; no policy changed),
  metrics, queries cookbook and MCP instructions say where each figure comes from now. The
  skill gained a section on the dump.
- **Checked live** on the real dump (mod 0.2.1). Premium comes from the client, expiring
  16 Oct. The Tesák needs 152,323 XP (242,560 minus the Šelma's 90,237), not the overlay's
  242,560. The garage is 42 tanks with the two rentals left out. The TVP's `client` block
  shows its equipment, 48.12 % marks and crew.

### Step C5 — evaluation and docs

Run the ten questions again, both as the plugin (run 7) and over MCP without the skill (run 8).
Questions 3, 4 and 6 get new criteria: they must use the real figures and give the dump's age.
Rewrite spec §3.6 as built, and add the mod to the README (install, and what to do after a game
update).

*Acceptance:* both runs at least 9 of 10, and 3, 4 and 6 pass on real data.

**Result (2026-09-25): done.**

- **Run 7 (plugin): 10/10.** The model found the real figures unprompted: the Šelma's XP, the
  TVP's fit, and the STB-1's first mark at 1.5 %. That last one corrected my own criterion,
  which had missed it.
- **Run 8 (MCP without the skill): 8/10.** Both failures were judgement, not data:
  - `elite: false` read as "modules are locked";
  - a mark target past the 10 % line recommended as "borderline".
- **Fixes, in what every session reads:** what `elite` means (`metrics.md` and the `wot_tank`
  description), and the mark rule stated against `moving_avg_damage`, first marks included,
  with no tank past the line recommended. Run 8b re-asked both questions three times, and run
  8c, all ten again, scored 10/10.
- **Docs.** Spec §3.6 is rewritten as built, and the storage and CLI sections are updated. The
  README has a section on the client mod: building it, installing it, what to do after a game
  update, and rebuilding the Desktop bundle after a migration.

**Still with the owner:**
- Close the game, run `make mod-install` for mod 0.3.0, which adds the research list, and open
  the game once.
- Reinstall `dist/wotctx-0.4.0.mcpb` in Desktop: the cache is now schema 7.

Risks particular to this phase:

| Risk | Mitigation |
|---|---|
| A game update renames what the mod reads | Every read is wrapped: a missing attribute becomes a missing field and one `python.log` line, never a client error. `doctor` shows the dump's game version |
| Aslain's installer or a game update removes the mod | `doctor` warns when the current `mods\<version>\` has no copy; `wotctx mod install` restores it |
| The mod is mistaken for a cheat | Lobby only, read-only, no network, no input: the category Wargaming permits (spec §3.6). The source is in this repository for anyone to read |
| The decompiled source differs from the running client | It matches the installed build exactly, and C1 and C2 end in checks against the client before `wotctx` relies on anything |

## Phase 5 — Tank Advisor

The contract is `docs/spec-desktop.md`. This phase turns the personal tool into one any player
installs from a single download. The phase has three parts: the generic core and the
customisable advice (D2–D4), which need no GUI and are useful to the owner at once; then the app
(D5–D8); then shipping it (D9–D11). Each step leaves the owner's current installation working.

The phase is done when spec-desktop §16 holds in full. In short: a player with no developer
tools gets a grounded answer within ten minutes of downloading, the advice follows their own
settings, and everything stays current without them.

Decisions D1–D9 were settled on 2026-09-26 (spec-desktop §14). Wargaming's terms were read
the same day (spec-desktop §12). They shaped the name, the notices, Log out, the handling of
personal data, and where the application id lives.

### Step D0 — the owner's accounts (human)

These are not code, and signing approval takes time to come back, so they start first:

1. A **dedicated Wargaming account** for the project, and on it a **standalone** ("Mobile")
   application. Its registration names the repository and a contact address (spec-desktop
   §12.4). Its id goes into a GitHub repository secret, never into git.
2. **SignPath Foundation**: apply for free open-source signing.
3. An **Ed25519 key pair** for signing releases (spec-desktop §9). The private key goes into a repository secret
   and an offline backup. The public key is committed.

Wargaming and tomato.gg are **not** contacted in advance. The project answers them if they
raise something first (spec-desktop §12.4).

*Acceptance:* 1 and 3 are done, and 2 is sent. SignPath's answer is recorded here when it
arrives.

**Progress:** 3, the release key: the public half is committed as `cmd/tankadvisor/release.pub`
(2026-09-27). The private half's secret and backup are the owner's to confirm. 2, SignPath: it asks for proof of users other than the author, so the first releases
go out unsigned and the project applies again later (see D11).

### Step D1 — measure before building

The spec marks four things as "to measure". Each is checked on this machine and recorded here:

- where Game Center records the game's install path (registry, or a file under
  `%PROGRAMDATA%\Wargaming.net\GameCenter\`), and whether a Steam install is laid out the same
  way (from its documentation, if no Steam install is at hand);
- whether opening a `.mcpb` from another process brings up Claude Desktop's install dialog,
  and which Claude plans can use local extensions;
- that Wails builds a window and an NSIS installer here with Go 1.27, WebView2 present, and a
  tray icon (Wails v2 has none of its own, so this also settles the tray library);
- what `account/info` returns with and without `fields`, to fix the field list of D2.

*Acceptance:* each answer recorded here with how it was found. An answer that contradicts the
spec is corrected in the spec before D2.

**Result (2026-09-26): measured; three on-screen checks are left for the owner.**

- **Finding the game.** Game Center records every install in
  `%ProgramData%\Wargaming.net\GameCenter\preferences.xml`, under
  `games_manager/games/game/working_dir`, and its choice under `selectedGames/WOT`. There is no
  per-user registry record: `HKCU\Software\Wargaming.net` holds only the error monitor. A
  recursive scan of `HKLM` was too slow and was stopped, but it is not needed. The file lists
  **two** installs here: `World_of_Tanks_EU` and `World_of_Tanks_CT`, the Common Test. Each
  game folder's `game_info.xml` names what it is: `<id>WOT.EU.PRODUCTION</id>` against
  `WOT.CT.PRODUCTION`, with the realm in `content_localization realm="eu"`. So the app picks
  `WOT.<REALM>.PRODUCTION`, skips test clients, and can preselect the server in the wizard.
  The running game is `<game>\win64\WorldOfTanks.exe`, so its folder is the parent of `win64`.
- **The mods folder is named in `paths.xml`**
  (`<Path mask="*.wotmod" …>./mods/2.4.0.1</Path>`). Deriving it from `version.xml` is right for
  this client but wrong for the Common Test, whose `version.xml` reads `v.2.4.1.0 Common Test #954`
  and whose folder is `mods\2.4.1.0 Common Test`. `internal/game` should read `paths.xml`, and
  fall back to `version.xml`.
- **Steam** was not measurable, since nothing is installed through Steam here. Community posts
  put it under `steamapps\common\World of Tanks\`, possibly with a realm subfolder. Detection
  looks there and one level down for a folder with `game_info.xml`, and a Steam player confirms
  it in D10.
- **Claude Desktop does not claim `.mcpb` files.** There is no file association.
  `FileExts\.mcpb` holds only an *Open with* list naming `claude.exe`, which is how the owner
  installed the bundle. So opening the file through the shell would show Windows' "How do you
  want to open this?" prompt. The app runs `%LOCALAPPDATA%\AnthropicClaude\claude.exe <bundle>`
  instead (a Squirrel stub whose path survives Claude updates). The documented route, dragging
  the file onto *Settings → Extensions*, is the fallback: the app opens Explorer at the file and
  says what to do. Claude records installs in `%APPDATA%\Claude\extensions-installations.json`:
  the id (`local.mcpb.ondrejkouril.wotctx`), version, hash, and `signatureInfo.status`
  (`unsigned` today). The app can read it to show whether, and which version, is installed. The
  format is undocumented, so an unreadable file reads as "unknown".
- **Plans.** Anthropic's docs list desktop extensions as working in "the Claude desktop app,
  signed in to claude.ai", with no plan limit. Team and Enterprise organisations can turn them
  off or allow-list them. The wizard says so when *Extensions* is missing.
- **Wails.** v2 (stable, 2.16.0) has no tray icon. **v3 is beta (`v3.0.0-beta.26`)** and has a
  tray, autostart, notifications, single instance, and **its own updater**: GitHub Releases,
  SHA-256, Ed25519 signatures, swap and restart. A probe with a window and a tray menu built
  here with Go 1.27 and `CGO_ENABLED=0`, at 12.4 MB. `wails3 generate build-assets` produced an
  NSIS script with a per-user mode (`WAILS_INSTALL_SCOPE=user` → `$LOCALAPPDATA\Programs\…`).
  Portable NSIS 3.12, with no system install, built a **6.7 MB installer whose manifest asks for
  `asInvoker`**, so there is no UAC prompt. The script also bundles the WebView2 bootstrapper
  for machines without it. Decision: **Wails v3, pinned to one beta and updated deliberately.**
  The tray settles it, and the updater replaces the hand-written one in spec-desktop §9.
- **`account/info`.** The stored response is 24.6 KB. `wotctx` reads `account_id`, `nickname`,
  `last_battle_time`, `global_rating`, the private resources, `garage`, `boosters`,
  `personal_missions`, and `statistics.random` and `.all`. It parses `clan_id`, `created_at` and
  `logout_at` but stores none of them. Everything else is unread: the per-tank frag counts (7.3
  KB); the clan, company, team, historical and stronghold statistics; and `ban_time`, `ban_info`,
  `restrictions` and `is_bound_to_phone`. `grouped_contacts` is not returned, since it is never
  requested. The `fields` list for D2 is the read set above. Whether `fields` combines with
  `extra` as expected is checked live in D2.

**Still with the owner** (they put windows on screen, so after playing):
1. Run the probe, `scratchpad\wailsprobe\probe.exe`. A window appears; closing it hides it; the
   tray icon's menu has *Open* and *Quit*.
2. Run `scratchpad\wailsprobe\bin\probe-amd64-installer.exe`. It installs with no UAC prompt to
   `%LOCALAPPDATA%\Programs\`, then uninstalls from *Settings → Apps*.
3. Run `%LOCALAPPDATA%\AnthropicClaude\claude.exe "<path to dist\wotctx-0.4.1.mcpb>"`. Claude
   Desktop shows its extension install dialog. Cancel it.

### Step D2 — a generic core

No GUI yet. Everything here is in `wotctx`:

- `config.Default()` loses the owner's account and game folder. `doctor` reports "not set up"
  with the fix, and the owner's `config.yaml` gains the account lines it relied on.
- A built-in application id is injected with `-ldflags` (empty in a local build). The keychain
  id overrides it, and redaction covers both.
- `account/info` requests named `fields` only.
- `wotctx auth wg --logout` calls `auth/logout` and deletes the token.
- `wotctx data delete` removes the database, the dump and the keychain entries, after asking.
- Raw bodies older than 90 days are pruned at sync.
- Question-time fetches send a `User-Agent` naming the project and its repository URL.
- The two switches of spec-desktop §12.4: `sync.retention` (unset keeps all history) and
  `meta.moe_fetch` (default `true`; when `false`, `meta moe` gives the dump's marks and says
  the thresholds are unavailable).
- A test fails if the owner's nickname or account id appears outside `testdata/` and `docs/`.

*Acceptance:* `make test` passes, with goldens unchanged apart from reviewed differences. The
owner's `sync`, `doctor` and a plugin question work as before. A config with no account gives
"not set up", not the owner's data.

**Result (2026-09-26): done; no golden changed.**

- **No account by default.** `config.Default()` has only the realm. `doctor` fails `config`
  with "no account set up yet" and gives the command to fix it, and `sync` refuses with the same
  message. `wotctx auth wg --realm eu|com|asia` records the account on the first login through
  `config.Set`, which keeps comments and key order and never writes a file that would not load.
  A login as a different account than the one recorded is refused, and the token is not stored:
  one installation holds one account's history. The owner's `config.yaml` gained the account
  lines, and the installed build still reads it.
- **Finding the game.** `game_dir` is optional. `internal/game` reads Game Center's
  `preferences.xml`, Steam's library list and the default folders, and keeps only a
  `WOT.<REALM>.PRODUCTION` client. `ModsDir` reads `paths.xml`. `mod install` creates a missing
  mods folder rather than asking for a game start. On this machine, `doctor` found the EU client
  and the installed mod with no `game_dir` set.
- **The application id** comes from the keychain, then from a build-time
  `-X …/internal/wg.builtinApplicationID` (`make … WG_APP_ID=…`), and is redacted either way.
- **`--logout`** calls `auth/logout` and removes the token, and it removes the token even when
  Wargaming cannot be reached. **`wotctx data delete`** removes the cache, the dump, the token
  and the recorded account, after typed confirmation or with `--yes`. It keeps the overlay and
  the player's own application id, and deletes the database first, so a file held open by
  Claude Desktop stops it before anything else is gone.
- **Retention.** Migration 8 adds `snapshots.raw_pruned`. A NULL body already meant "failed",
  so "usable" is now *body present or pruned*. Each sync prunes bodies older than 90 days,
  never the newest per source. Snapshots with pruned bodies are left out of re-parsing.
  `sync.retention` (off by default; at least `7d`; durations accept days) deletes whole
  snapshots and their rows by cascade. Recent-form baselines come from the parsed `tank_stats`
  rows, so pruning bodies cannot remove one.
- **`meta.moe_fetch: false`** skips the fetch. `meta moe` then still gives the dump's marks and
  the combined-damage range, with a caveat naming the switch.
- **User-Agent** on every request: `Mozilla/5.0 (compatible; wotctx; +<repository URL>)`. It
  keeps the crawler shape, because tomato.gg challenges a bare agent. It was checked live, and
  the page answered.
- **`fields`, checked live** on a copy of the owner's data (the real cache is untouched, below).
  `account/info` fell from 24.6 KB to 11.8 KB. It carries exactly the requested keys, and the
  extra blocks come back when named in `fields`. Credits, gold, bonds and free XP match the
  previous snapshot.
- **A test** fails if the owner's nickname or account id appears in any non-test file under
  `internal/`, `cmd/`, `skills/`, `hooks/`, `commands/`, `packaging/`, `.claude-plugin/` or
  `mod/`. The skill's description and a comment in `brief.go` were the only two.

**Held back on purpose:** the owner's installed `wotctx` and Desktop bundle are not rebuilt.
The new binary migrates the cache to schema 8, and the installed bundle (schema 7) would then
refuse it. D4's launcher bundle ends that problem, so both are replaced together there.

### Step D3 — advice in three layers

- **Overlay:** `profile` and `advice` (spec-desktop §8.3), with validation and tests. The owner's
  overlay gets settings that reproduce today's `framework.md` §1 and §9.
- **Framework:** split into core rules, defaults, and the generic §1 and §4.9 (spec-desktop
  §8.1, §8.2). The SPG "never" follows `avoid_classes`.
- **Renderer:** the *Your advice* section, in plain words, with the precedence stated, and
  conflicting rules flagged.
- **Surfaces:** `wot_guide` returns core, then *Your advice*, then the defaults. `wotctx guide`
  mirrors it byte for byte. `SKILL.md` becomes generic and runs `wotctx guide` first. The
  claude.ai export carries the rendered guide.

*Acceptance:* goldens of the rendered guide for the owner's settings, for defaults only, and for
a conflicting rule. **Run 9** (plugin) and **run 10** (MCP, no skill) on the owner's account
both score 10/10. Three settings pairs (`format: plain`, `meta: high`, a player rule) each
change the answer as the setting says, and still pass the core rules.

**Result (2026-09-26): done.** core.md / Your advice / framework.md as defaults; `internal/advice` renders and flags conflicts; `wotctx guide` and `wot_guide` = procedure + guide (pinned by a test); export carries `references/guide.md`; `WOTCTX_CONFIG_DIR`/`WOTCTX_DATA_DIR` for isolated evals. Runs 9b/10 10/10, pairs pass (docs/evals.md). Work moved to a worktree (`../tank-advisor-phase5`) so the live plugin, which loads skills from the main checkout, stays on `main` until the switch-over.

### Step D4 — the launcher bundle

`cmd/wotctx-launcher`: find `wotctx.exe` (from `HKCU\Software\Tank Advisor`, then the default
path), run it with `mcp` and inherited stdio, and pass its exit code on. With no `wotctx`, serve
only `wot_data_status`, saying to reinstall. `make bundle` packs the launcher instead of the
binary. The existing test that the manifest's tools match the server's stays.

*Acceptance:* **Owner:** install the new bundle in Desktop and ask a question. Then replace
`wotctx.exe` with a newer build, restart Desktop, and the new build answers with no bundle
reinstall. With `wotctx.exe` renamed away, Desktop shows the "reinstall" status, not a failure.

**Result (2026-09-26): built, owner check pending.** `cmd/wotctx-launcher` (registry, default folder, PATH; job object ends wotctx with it; a one-tool "app missing" server). Tests: 12 tools through the launcher, and wotctx.exe deletable once the launcher is killed. `make bundle` gives `dist/wotctx-0.5.0.mcpb` (3.3 MB, validated). `packaging/mcpb` embeds the manifest for the app.

### Step D5 — the app: status window and tray

`cmd/tankadvisor` with Wails. The status window of spec-desktop §5.2 is `doctor` with buttons:
login (log in, renew, log out), data (sync now, delete my data), mod, Claude, updates, and
About with every notice of §12.2. There is a tray icon and an autostart the player can switch
off. The styling is the app's own, with nothing borrowed from Wargaming.

*Acceptance:* **Owner:** on this machine, every row shows the real state, and every button does
what it says (log out, then log in again). About carries every §12.2 item. `wotctx` gains no
dependency (`go list -deps ./cmd/wotctx` is unchanged).

**Result (2026-09-27): built; the owner's check is open.**

- **Two layers.** `internal/app` is the window without a window. `Status` turns doctor's checks
  into five rows (Wargaming login, Data, Client mod, Claude, Tank Advisor), each with a
  plain-words hint and the buttons that fix it. `Do` runs an action. Every action is a
  `wotctx` command run in process through `cli.Run`, so nothing is re-implemented.
  `internal/cli/app.go` exports only what the rows need: the checks, the stored login without
  its token, and the game folder. One action runs at a time, and a second click is answered
  "Busy: …" at once. The browser login can be cancelled from the window.
- **`cmd/tankadvisor`** is Wails v3 beta.26. The window hides on close. The tray icon has *Open*,
  *Sync now* and *Quit*. There is a single instance: a second start opens the first one's
  window. *Start with Windows* uses Wails' autostart, registered with `--hidden`, so logging on
  puts the app in the tray only. The page is plain HTML, CSS and a JavaScript module that calls
  the Go service by name through `/wails/runtime.js`, with no npm and no generated bindings. Its
  look is its own: system font, neutral greys, a blue accent, light and dark themes, and an icon
  of three rising bars. WebView2's cache goes to `%LOCALAPPDATA%\Tank Advisor\WebView2`, not to
  Wails' default of `%APPDATA%\TankAdvisor.exe`.
- **What the rows read.** The mod package and the Desktop bundle are found beside the
  executable and picked by version, not by file time. The Claude row reads Claude Desktop's
  `extensions-installations.json`, where our entry is the one whose manifest is named `wotctx`,
  and Claude Code's `installed_plugins.json`. An unreadable file reads as "unknown". *Check for
  updates* asks GitHub's latest-release API and links the release page. The repository is
  private today, so the answer is "No release is published yet." Wails' updater replaces this
  in D9. The Data row gained the **longest gap between syncs** (`store.LongestGap`, over usable
  `account/info` snapshots).
- **About** carries the §12.2 notices: the developer's copyright, "© Wargaming.net. All rights
  reserved", that the data comes from Wargaming.net, "not affiliated with or endorsed by
  Wargaming", and the realm's official game site. **Wargaming Support** is a separate,
  outlined button. `eu.`, `na.` and `asia.wargaming.net/support/` all lead to
  `wargaming.net/support/`, so that is the link. *Log out* is in the login row.
- **Checked here** with the game running, so the window stayed hidden. A build with Wails'
  `mcp` tag ran with `--hidden`, and its page was read through that tag's `js_eval`. All five
  rows rendered from the owner's real data, with the right buttons. Clicking *Check for
  updates* went through `Do` and updated the row. The About text was complete, and the app quit
  cleanly through its own *Quit*. Reading the real rows led to two fixes. The Data summary now
  reads "Last synced 12 minutes ago" rather than doctor's list of sources. A dump that only
  needs a sync now gets *Sync now* instead of "start the game".
- **Dependencies.** `go list -deps ./cmd/wotctx` is unchanged. A test fails if `wotctx`, the
  launcher or `internal/app` links Wails. Adding Wails raised `golang.org/x/text`, which
  `wotctx` already used, from 0.30.0 to 0.39.0. `make test` passes, with no golden changed.
- **`make app`** builds `dist/app/TankAdvisor.exe` with the GUI subsystem, CGO off and the same
  `LDFLAGS` as `wotctx`, so a release carries the built-in application id. It copies the newest
  mod package and bundle from `dist/` beside the app, or the ones named by `MOD_PKG` and
  `BUNDLE`.

**An incident, and why the owner's check waits for the switch-over.** The app opens the cache in
process, and this branch's store migrates it to schema 8. A status test on 2026-09-27 ran
against the owner's real cache and migrated it. The installed `wotctx` (b9a8b9d, schema 7) then
refused the cache, so the plugin and the 0.4.0 Desktop bundle were down from about 11:39 to
11:51. Migration 8 only adds `snapshots.raw_pruned`, and no row had been pruned. So, after a
backup, the column was dropped and `user_version` set back to 7. The installed `doctor` passes
again. Until the switch-over, branch builds run against a copy (`WOTCTX_CONFIG_DIR`,
`WOTCTX_DATA_DIR`).

**Found by the owner's check: renewing and logging out never reached Wargaming.** *Renew*
failed with `auth/prolongate: ACCESS_TOKEN_NOT_SPECIFIED (code 402)`. Probed live with a fake
token: `auth/prolongate` and `auth/logout` ignore an `access_token` sent in a GET query, and only
read it from a POST body. Sent in a body, the same fake token got `INVALID_ACCESS_TOKEN`
(prolongate) and `ok` (logout). `account/list` answers either way, which is why syncing always
worked. The client had sent everything by GET since phase 1. So `--prolong`, the automatic
renewal near expiry and *Renew* have never worked, and `--logout` only ever removed the local
copy while printing "Wargaming did not confirm the logout". `wg.Client.Post` now sends both in a
form body, which also keeps the token out of URLs. The tests' fake server now answers like
Wargaming: GET gets 402. Both tests fail on the old code. `main` has the same bug until this
branch merges.

**Owner, after playing.** This is also D4's check and the switch-over:
1. In the worktree, run `make install` (the new `wotctx`, schema 8, onto `PATH`), then
   `make bundle`, then
   `make app MOD_PKG=../tank-advisor/dist/ondrejkouril.wotctx_0.3.0.wotmod`.
2. Start `dist\app\TankAdvisor.exe`. The Claude row should say the extension is 0.4.0 and the
   app carries 0.5.0. Click *Update extension*, then *Install* in Claude Desktop. That is D4's
   bundle, and D4's own checks follow from there.
3. Go through every row:
   - *Sync now*.
   - *Log out*, then *Log in*. The browser opens Wargaming's page.
   - *Renew*.
   - *Open mods folder*.
   - *Export for claude.ai*. Explorer opens the folder.
   - *Check for updates*.
   - Switch *Start with Windows* on, then sign out and back in. The app is in the tray, with no
     window. Then switch it off.
   - Read About and click its three links.
4. Close the window, and the app stays in the tray. Start it again, and the same window comes
   back. *Quit* from the tray.

### Step D6 — the wizard, the game and the mod

The eight wizard steps of spec-desktop §5.1, with consent recorded in `config.yaml`. Game
detection is as D1 found it. The mod ships inside the app. It is installed at step 5, and again
by itself whenever `version.xml` changes, with elevation only for an unwritable game folder.

*Acceptance:* **Owner**, on a second Windows user account on this machine: the wizard runs from
nothing to a first sync and a mod dump, with no terminal. A simulated update (an edited
`version.xml` in a copy of the game folder) gets the mod into the new `mods\<version>\` unasked.

**Result (2026-09-27): built; the owner's check is open.**

- **The wizard** is `internal/app`'s `Wizard` state plus one page per step in
  `frontend/wizard.js`. The state says which steps are done: a resumed setup opens at the first
  step not done, and every step can be reached again from the window's *Setup* button. The app
  opens on the wizard until `setup.completed` is recorded, and again when the notice's version
  rises. Starting with Windows (`--hidden`) still shows the window while setup is unfinished.
  The config gained the app's own records: `consent` (notice version and date),
  `setup.completed`, and `mod.managed` with `mod.placed`. `wotctx` reads none of them. Each
  step's rule is in spec-desktop §5.1.
- **Game detection** gained the running client (spec §6.1, item 3). It reads the process list
  with the limited query right and takes the parent of `win64\`. Checked live, with the game
  running: it found `C:\Games\World_of_Tanks_EU\win64\WorldOfTanks.exe`, and the same folder
  that Game Center lists is counted once. The app resolves the game folder by one rule
  (`game_dir`, then the best found) over its own detection.
- **The mod, managed.** *Install* records `mod.managed` and each folder in `mod.placed`. A game
  folder the player cannot write to is copied by the app itself, run again with `runas` through
  `ShellExecuteEx` as `TankAdvisor.exe --install-mod <pkg> --game-dir <dir>`, and the app waits
  for its exit code. That run returns before the single-instance check. The upkeep runs at
  start and every 3 minutes. Once the mod is managed, it reinstalls into the current mods
  folder when that folder lacks the bundled package, after a game update or an app update that
  carries a newer mod. It never raises a UAC prompt by itself: it waits, and the status row
  says why. The plan's simulated update is a unit test (`TestAGameUpdateGetsTheModWithoutAsking`):
  an edited `version.xml`, and the mod lands in `mods\2.5.0.0\` with both folders recorded.
- **Two bugs found while building it:**
  - A token left in the keychain with no account recorded made `auth wg` answer "already
    logged in" without recording the account, so the login step could never finish. Now,
    without a recorded account, the login always runs. A test covers it and fails on the old
    rule. `loginManually` also read `os.Stdin` directly, bypassing `Env.Stdin`, and now uses it.
  - `sync` finishes "OK" even when every Wargaming source failed, because a failed source is a
    caveat. The app's *Sync now* and the wizard's last step now look for the account's own data,
    and say "no account data came back" with Wargaming's reason. The CLI's behaviour is
    unchanged.
- **Checked end to end without a window.** The owner was in game, and their app held the
  single-instance lock, so a throwaway harness served the real page and the real
  `internal/app` service over HTTP to headless Edge. That is the same engine as WebView2. The
  harness ran on an isolated install (temporary folders, an in-memory keychain, a fake game
  folder), and a script clicked through all eight steps. There were no page errors. The mod went
  into the fake folder and was recorded as managed. The page noticed the game's first dump by
  itself. The questionnaire wrote only the two answers changed, into a new overlay that
  validates. With a fake application id, the sync step said no account data came back and
  stayed undone. The run found five things to fix, all fixed: the game folder was resolved two
  ways; empty notes were drawn as "null"; `version` came after the answers in a new overlay;
  the sync counted as done; and a stale message stayed after the dump arrived. The harness is
  not in the repository.
- `make lint` and `make test` pass. `go list -deps ./cmd/wotctx` gains no Wails package, as the
  guard test checks.

**Owner, after playing.** The acceptance wants a second Windows user account on this machine,
so that the wizard starts from nothing:
1. Run `make app MOD_PKG=../tank-advisor/dist/ondrejkouril.wotctx_0.3.0.wotmod` in
   the worktree, and copy `dist\app\` to a folder the other account can read.
2. Signed in as the other account, start `TankAdvisor.exe` there. The wizard opens. Go through
   all eight steps with no terminal, logging in with the account to be advised. At step 5, start
   the game in that account and wait in the garage until the page says the file arrived.
3. On your own account, check the update rule on a copy: copy the game folder's `version.xml`,
   `paths.xml` and `game_info.xml` into a new folder, choose it in *Setup → Game*, install the
   mod, then change the version in the copied `version.xml`. Within 3 minutes the mod is in
   the new `mods\<version>\`. Afterwards, choose the real folder again in *Setup → Game*.

### Step D7 — Goals and Advice pages

The Goals page (spec-desktop §5.3), and the Advice page (§8.4) with its live *What Claude
reads* preview. Writes go through `yaml.v3` nodes, so comments and key order survive. A file
that fails validation is never overwritten, and an outside edit is reloaded.

*Acceptance:* a round-trip test on the owner's overlay leaves every comment in place. **Owner:**
change a weight, the format and one rule, see the preview change, and see Claude follow them in
a new conversation. A rule that conflicts with a core rule is flagged, and Claude still keeps
the core rule.

**Result (2026-09-27): built; the owner's check is open.**

- **Writing the file without rewriting it.** Re-encoding the owner's overlay through `yaml.v3`
  kept the comments' words, but dropped the blank lines between sections, collapsed the
  aligned comment columns and moved comment continuation lines. That was measured on the file
  itself. So `yamledit.Apply` splices text instead:
  - `yaml.v3` locates each key;
  - a changed scalar or inline list is rewritten on its line, with the comment at the same
    column;
  - a mapping is edited key by key;
  - a list is rebuilt from its old items' text, so unchanged items keep their comments, a
    changed item is edited in place, and reordering moves each item with its comments;
  - a new key goes after its mapping's last key.

  `config.Set` and the wizard use it too. Values are compared structurally, so quoting and
  style do not count as changes, and a date written unquoted matches the same date passed
  back as a string. An integer and a string never match: an unquoted `123` is a tank_id, and
  `"123"` is a name. Thirteen tests on a copy of the owner's overlay pin the exact bytes
  after each kind of edit.
- **The pages.** `internal/app` holds `AdviceSettings` and `GoalsPage`: exactly what the file
  sets, zero where it sets nothing. `PreviewAdvice` renders *What Claude reads* from the very
  bytes a save would write, with `advice.Conflicts` for the rule warnings and any validation
  errors. Saves are Do actions, refused when the file's hash differs from the one the page
  loaded. A page with nothing unsaved polls the hash and reloads by itself; one with unsaved
  changes asks. The Goals page's tank picker resolves the typed text the way the overlay
  does, folding accents, so "tesak" finds the Vz. 71 Tesák. A name several vehicles share is
  offered by tank_id, and an ambiguous or unknown name blocks the save. *Researched from*
  lists the tech-tree predecessors with their XP cost, fills in `xp_required`, and matches
  the file's "Selma" to the tree's "Šelma" by tank id. The backup fields show only without a
  mod dump. Class order is a rank per class rather than drag-to-rank, because ties are
  allowed (spec §8.4, updated).
- **The acceptance test** `TestTheOwnersOverlaySurvivesARoundTrip` loads both pages on the
  owner's overlay and saves them unchanged. The file comes back byte for byte. A change to a
  weight, the format and a new rule changes exactly those lines, plus `updated_at`, whose
  comment stays in its column.
- **Checked in headless Edge**, with the harness of D6 on an isolated install holding a copy
  of the owner's overlay:
  - the preview followed each change;
  - a "guess the numbers" rule was flagged;
  - Save wrote exactly the three changes, with every comment in place;
  - an outside edit reloaded the page by itself;
  - on the Goals page, the existing goal showed Šelma, the picker found the Tesák, choosing
    Blesk filled 250,000 XP, and the new goal was appended below the commented one.

  The runs found two bugs, both fixed: a stray "null" from an empty banner slot, and the
  *Researched from* match by text, which missed "Selma" against "Šelma". There were no page
  errors.

**A gap from the switch-over, for the owner to decide.** The advice settings D3 wrote for the
owner live in this branch's `wot-overlay.yaml`. That covers the weights, the answer style and
the five rules. But `overlay_path` points to the main checkout's copy, which has none of them.
Since the installed `wotctx` became this branch's (2026-09-27), Claude has been using the
defaults: `explain_basics` is `auto` rather than `never`, and none of the owner's rules apply.
There are two ways to close it: copy the `profile` and `advice` blocks into the main checkout's
overlay, or point `overlay_path` at this worktree's file until the merge.

**Owner, after playing:**
1. Close the gap above first, then start the rebuilt `dist\app\TankAdvisor.exe`.
2. On *Advice*, make three changes, watching *What Claude reads* change with each:
   - set *Strength in the meta* to *Low*;
   - set the layout to *Plain paragraphs*;
   - add a rule, for example "Keep the verdict to one sentence".

   Then add a rule that asks for a guess ("guess the win rate when there is no data"). It shows
   a warning. Save, and look at `wot-overlay.yaml`: only those lines, and `updated_at`, have
   changed. Delete the guessing rule and save again.
3. Start a new conversation in Claude Desktop and ask "what should I research next?". The
   answer is in paragraphs, keeps meta low and follows the new rule.
4. On *Goals*, check the Tesák goal shows *Researched from Šelma*. Change nothing and save:
   "Nothing changed".

### Step D8 — background duties

These are the duties of spec-desktop §5.6. Sync runs when `WorldOfTanks.exe` exits, after the
dump settles, and daily otherwise. The token is renewed ahead of expiry, and a lapse brings a
tray notification with *Log in again*. The mod is looked after on game updates. All of these
can be paused.

*Acceptance:* **Owner:** play a session, close the game, and a sync follows within a minute,
with the fresh dump. `query sessions` shows the session as one interval. An idle day costs one
sync and no CPU in between.

**Result (2026-09-27): built; the owner's check is open.**

- **`internal/app/duties.go`** decides what each duty does, on hooks for the clock, sleeping,
  notices and the sync, so the tests run on a fake clock:
  - `AfterGame` waits until the mod's dump has gone unchanged for 5 s (at most 2 minutes, and
    not at all without a dump), then syncs;
  - `Periodic` renews the login when fewer than 3 days remain; a lapsed login gives a notice
    with *Log in again*, at most once a day; a logout is not a lapse;
  - `Periodic` also syncs when the account's newest usable `account/info` is a day old, and
    checks for updates once a day, announcing a newer release once;
  - `Mod` is D6's upkeep.

  A duty's sync waits while a click in the window holds the app busy. Each duty can be paused
  on its own (`duties.paused`; *Background* row in the status window, with the last thing each
  duty did).
- **`cmd/tankadvisor`** calls them:
  - `watchGame` looks for a running client every 30 s through the process list, then blocks on
    its handle (`game.WaitForExit`, `WaitForSingleObject` in 5 s slices) and calls `AfterGame`
    when it exits. A game already running when the app starts is waited on the same way;
  - `Periodic` runs a minute after start, then hourly;
  - `Mod` runs every 3 minutes.

  Notices are Windows toast notifications through Wails' notifications service, which adds
  `go-toast` to the app only. `wotctx`'s package list is unchanged, and the build still needs
  no cgo. A notice's button runs its action, and clicking the notice opens the window. The
  service registers an AppUserModelID under `HKCU\Software\Classes\AppUserModelId\`, which
  the uninstaller must remove (spec §4, updated).
- **Tests:**
  - the sync waits for the dump to settle, and does not wait with no dump;
  - the daily sync is due only after a day;
  - paused duties do nothing, and the row says so;
  - a lapsed login is noticed once a day, and a logout never;
  - the mod comes back after a simulated update.

  `WaitForExit` was tested on a real two-second process. In headless Edge, *Pause syncing*
  recorded `duties.paused: [sync]`, and the row turned to "Paused: syncing" with the gap
  warning. Renewing near expiry is not unit-tested, because it calls Wargaming; it is the same
  *Renew* action the owner checked in D5.
- `make lint` and `make test` pass.

**Owner, after playing.** The app must be the rebuilt `dist\app\TankAdvisor.exe`, left running:
1. Play a session and close the game. Within about a minute the *Background* row says "last
   … after the game closed: Synced", and *Data* says "Last synced just now".
2. In Claude, ask about today's session. It is one interval, using the fresh dump.
3. On a day without play, leave the app running. Task Manager shows it idle, and the next
   morning *Data* shows one sync from the night.

### Step D9 — installer, release pipeline, self-update

- **NSIS installer:** per user, `PATH`, Start menu, uninstaller (spec-desktop §4), and the
  notices on its licence page.
- **GitHub Actions on Windows:** builds `wotctx`, the launcher and the bundle, then the app
  with them embedded, then the installer. It uses the application id from the secret, signs
  through SignPath once approved, and publishes with `SHA256SUMS` and the Ed25519 signature.
  The `.wotmod` is the maintainer's build, attached as an input.
- **Self-update** (spec-desktop §9): Wails' updater against GitHub Releases, with the public
  key compiled in. The embedded payloads are written out at start, with the rename trick for a
  running `wotctx.exe`.

*Acceptance:* a test release (a pre-release on GitHub) installs on the second Windows account.
Then a newer pre-release updates it while Claude Desktop is running, and after a Desktop
restart the new `wotctx` answers. A release with a bad or missing signature is refused. The
uninstaller leaves no mod, autostart or `PATH` entry, and keeps the database unless asked.

**Result (2026-09-27): built and tested locally; releasing waits on D0.**

- **The installer copies one file.** `packaging/nsis/tankadvisor.nsi` installs per user
  (`asInvoker`, checked in the built installer's manifest). It shows the notices and the MIT
  licence (`notices.txt`), installs WebView2 when Windows lacks it, and creates the Start menu
  shortcut and its *Apps* entry. The rest is `TankAdvisor.exe`'s own work, in Go, and tested:
  - `--install-payloads` writes out the payloads, adds the folder to the user `PATH` (with
    `WM_SETTINGCHANGE`) and records `HKCU\Software\Tank Advisor\InstallDir` for the launcher;
  - `--uninstall [--delete-data]` takes the mod out of every folder in `mod.placed`, and
    removes the `PATH` entry, the autostart value `tank-advisor`, the notification
    registration (the AppUserModelId and its CLSID), `HKCU\Software\Tank Advisor` and the
    browser cache. It deletes the data only on a yes, which is not the default;
  - `--quit` closes a running copy before its file is replaced.

  `make installer` built `TankAdvisor-v0.6.0-dev.1-setup.exe` (16.5 MB, with a 41 MB app
  carrying its payloads). It was not run here: installing touches the owner's `PATH`,
  registry and Start menu, which is what the acceptance's second Windows account is for.
- **Payloads.** A release build embeds `wotctx.exe`, the `.mcpb` and the `.wotmod`
  (`cmd/tankadvisor/payload/`, filled by `make installer`, kept out of git). At each start the
  app writes out any that differ. A newer mod or bundle replaces the older file. A running
  executable is renamed to `.old.exe`, which a later start removes; the test does this to a
  really running copy. The separate launcher executable was dropped from the payloads,
  because it lives inside the `.mcpb`.
- **Self-update** (`cmd/tankadvisor/updates.go`) uses Wails' updater, headless
  (`WindowNone`), with the public key from `cmd/tankadvisor/release.pub`, committed by the owner
  on 2026-09-27. A build without a key, or without a release version, updates nothing. The provider is our own, because Wails' GitHub
  provider never supplies a signature, and treats even the checksum as optional. Ours
  requires `TankAdvisor.exe`, `SHA256SUMS` and `TankAdvisor.exe.sig`. The status window's
  *Update* button downloads, verifies and restarts. Tests against Wails' real updater and a
  fake GitHub:
  - a properly signed release is verified and staged;
  - one signed with another key fails at "ed25519 signature";
  - one without a signature is never offered;
  - a pre-release is offered only to a pre-release build.
- **Signing.** `internal/release` and `cmd/releasetool` handle `keygen`, `sign`, `verify` and
  `sums`: Ed25519 over the SHA-256 digest, the scheme the updater checks.
- **The release workflow** (`.github/workflows/release.yml`) is run by hand with the tag of a
  draft release that has the `.wotmod` attached; GitHub starts no workflow for a draft's own
  events, which the first release found out. It checks the tag, the committed public key and
  both secrets. It runs the tests, builds the installer with the secret application id,
  writes `SHA256SUMS`, signs the app, verifies that signature against the committed key, and
  uploads the files to the draft. Authenticode signing through SignPath has its place marked
  in the workflow, for when SignPath answers. The workflow has not run: the repository has no
  secrets yet.
- `make lint` and `make test` pass.

**Not done, and why:**
- **D0 is the gate.** The release key's public half is committed (2026-09-27); its private
  half still has to go in as the `RELEASE_SIGNING_KEY` secret, with an offline backup. The
  dedicated Wargaming account's application id (`WG_APPLICATION_ID`) and the SignPath
  application are the owner's too.
- **The repository is private.** Players' updaters, and anyone downloading, cannot reach its
  releases until it is public. Whether and when to make it public is the owner's call.
- *Install updates automatically* (spec §9) is not built.

**Owner, once D0 is done:**
1. Create a draft pre-release `v0.6.0-rc.1`, attach `dist\ondrejkouril.wotctx_0.3.0.wotmod`, and
   save it. The workflow fills the draft. Publish it as a pre-release.
2. On the second Windows account, install `TankAdvisor-setup.exe`, with no UAC prompt, and set
   it up.
3. Publish `v0.6.0-rc.2` the same way. With Claude Desktop open, the app offers the update;
   click *Update*. After a Claude Desktop restart, `wotctx version` in a new terminal, and the
   answers, come from rc.2.
4. Uninstall from *Settings → Apps*, answering No to deleting the data. Afterwards the mod is
   gone from the game's `mods` folder, the `Run` entry and the `PATH` entry are gone, and
   `%LOCALAPPDATA%\wotctx\wotctx.db` is still there.

### Step D10 — a player who is not the owner

These are the evaluation and acceptance of spec-desktop §8.6 and §16, on someone else's
machine:

- a friend with World of Tanks and Claude Desktop installs from the pre-release, timed, with no
  help, and says where they hesitated;
- the ten questions on their account with default settings (**run 11**), with that player
  checking the figures against their own client;
- one non-EU realm synced end to end.

*Acceptance:* under ten minutes with no terminal, run 11 at least 9/10 with every failure
understood and fixed, and the non-EU sync clean.

### Step D11 — docs and the first release

- **Spec:** `docs/spec.md` changes as spec-desktop §13 lists, and spec-desktop is rewritten as
  built.
- **README:** a player's page first (download, what it does, privacy, the notices), and the
  developer setup after it.
- **Evals:** `docs/evals.md` records runs 9–11.
- **README privacy section:** the readings of spec-desktop §12.1 and §12.3, and how Wargaming,
  tomato.gg or anyone else reaches the project.
- **Publish 1.0.**
- **After release:** submit the mod to the Mod Hub.

*Acceptance:* the release is public. Spec, README and plan match what shipped.

**Result (2026-09-27): v1.0.0 is released.**

The project moved to a public repository first, `ondrejkouril/tank-advisor`. It starts from the
finished tree with no history and no personal data, and the Go module path, updater and links
were renamed with it. The full history is in the private, archived `tank-advisor-old`.

- **Built by the workflow**, with the owner's decision to release as 1.0.0 rather than as a
  pre-release first. The first attempt found that GitHub starts no workflow for a draft
  release's own events. The workflow now runs by hand with the draft's tag
  (`gh workflow run release -f tag=v1.2.3`). The run passed in 4m22s: tests, the installer
  built with the secret application id, sums, signature, upload.
- **Checked before publishing**, on the downloaded assets:
  - `SHA256SUMS` matches both executables;
  - `TankAdvisor.exe.sig` verifies against the committed `release.pub`;
  - the installer's manifest is `asInvoker`, and its version is v1.0.0;
  - the app carries its version and payloads.
- **Published** 2026-09-27 as the latest release, with the tag `v1.0.0` at `65474c2`, the
  commit it was built from. Assets: `TankAdvisor-setup.exe`, `TankAdvisor.exe`,
  `TankAdvisor.exe.sig`, `SHA256SUMS` and the mod 0.3.0. The notes carry the SmartScreen
  step.
- **Still open, now against a public release:**
  - D9's and D10's checks: the installer has not yet been run on any machine; a first
    install, an update to a later release, and an uninstall remain to be seen;
  - run 11, on another player's account;
  - the Mod Hub submission;
  - signing, once SignPath accepts the project.

**Earlier, the docs:**

- **`docs/spec.md`** has every change spec-desktop §13 lists:
  - §1: no fixed account;
  - §3.1: `fields`, `auth/logout`, and the POST-only auth calls found in D5;
  - §3.6: the app keeps the mod installed;
  - §4: migration 8's pruning, and a new table of every `config.yaml` key, the app's included;
  - §5: `profile` and `advice`;
  - §7: `guide`, `data delete`, and the new `auth wg` flags;
  - §8: the three-layer guide;
  - §10: the launcher;
  - §11: the build-time application id, the privacy guarantee as spec-desktop §12.3 states
    it, GitHub for updates, Wails in `cmd/tankadvisor` only, and the release workflow.
- **`docs/spec-desktop.md`** says "as built": each section was brought up to date by the step
  that built it, and the status line now says so. §4 and decision D5 record SignPath's answer
  (below).
- **README** now opens with a player's page:
  - what Tank Advisor does, and how to get it, with the SmartScreen step;
  - privacy, with the project's two readings of Wargaming's terms (spec-desktop §12.3) and how
    the application id is handled (§12.1);
  - how Wargaming, tomato.gg or anyone else reaches the project;
  - the notices of §12.2.

  The developer part follows, rewritten for phase 5: the pieces, building from source,
  `wotctx` directly, the source layout, and how to release. It names no account.
- **`docs/evals.md`** has runs 9 and 10 from D3. Run 11 is D10's, on another player's account.

**SignPath (D0, 2026-09-27):** it asks for proof that the application is used by people other
than its author, which a project with no release cannot give. So the first releases are
unsigned, as spec-desktop §4 foresaw. The README tells players about SmartScreen's *More info
→ Run anyway*, and each release's notes should say the same. The project applies again once it
has players, and the workflow marks where the signing step goes. Updates are protected either
way, by the release key's Ed25519 signature.

**Still to do in D11:** publish 1.0, after D10 and the rest of D0, and once the repository is
public; then submit the mod to the Mod Hub.

**Personal information removed (2026-09-27), at the owner's request, before publishing:**
- The owner's nickname and account id are gone from the tree: the docs were reworded, and
  the recorded fixtures, goldens and tests now use `example_player` and `512345678`. The guard
  test now covers everything, tests, fixtures and docs included, and holds only the SHA-256
  of the two identifiers, so it can look for them without naming them.
- `wot-overlay.yaml` is no longer tracked, and `/wot-overlay.yaml` is ignored. The overlay
  belongs in the config directory, where the app keeps it. `internal/yamledit/testdata/`
  keeps a copy as a formatting fixture: it holds preferences and goals, but nothing that names
  the account.
- **Merging into `main` deletes the main checkout's `wot-overlay.yaml`**, the file the owner's
  `overlay_path` points to. Before merging, copy this worktree's `wot-overlay.yaml`, which has
  the advice settings, to `%APPDATA%\wotctx\wot-overlay.yaml`, and remove `overlay_path` from
  `config.yaml`.
- **Git history still holds both identifiers and the overlay**, in earlier commits on every
  branch and on `origin`. Making the repository public publishes the history too, unless it is
  rewritten or the repository is published fresh.

Risks particular to this phase:

| Risk | Mitigation |
|---|---|
| Wargaming disagrees with a reading in spec-desktop §12.3 | Not asked in advance; the project answers if Wargaming raises it. The switches built in D2 (retention, `fields`, the id override) make the likely changes settings, and self-update ships them to every player |
| tomato.gg objects to the MoE fetches | `meta.moe_fetch: false` ships as the default in an update. Marks still come from the dump, without thresholds |
| Wargaming blocks the shared application id | A dedicated project account; the error is shown verbatim; a new id ships as an update; the keychain override keeps working |
| SmartScreen scares players off while signing is pending | Apply in D0. Until approved, pre-releases go only to testers, with the *Run anyway* steps written down |
| The generic framework loses what made the owner's advice good | Run 9 and run 10 must score 10/10 with the owner's settings before the app is built on it |
| Player rules undermine grounding | Core rules are fixed and restated after the player's rules. Conflicts are flagged in the app, and a settings pair in D3 tests it |
| A game update breaks the mod for every player at once | Unchanged from phase 4: a failed field is left out, not a crash. The app shows the errors and fetches a fixed mod through self-update |
| Claude Desktop changes how extensions install | The launcher keeps the bundle stable. The `claude_desktop_config.json` fallback is written down in the spec |

## Step 8 — overlay

YAML schema, loader, and `wotctx overlay validate` per spec §5.

*Acceptance:* the seed file validates; an unknown tank name fails with a nearest-match
suggestion; `researched_not_bought` entries already in the garage are flagged stale;
`overlay validate` exits non-zero on a malformed file.

**Result (2026-09-18): done.** The seed is `wot-overlay.yaml` at the repo root, found through
`overlay_path`. Against the live data it validates with two warnings, both stale entries: the
**WZ-113G FT** (`researched_not_bought`) and the **Executor** (the `xp_goals` target) are both in
the garage. They are left in the seed until confirmed in the client, not deleted on the API's
word. The seed's `Obj 277` does not match any name exactly (the API says `Object 277` /
`Obj. 277`), which led to punctuation-insensitive matching as a fallback — and the live list
showed why it can only be a fallback: `T29`/`T-29` and `T34`/`T-34` are distinct tanks, and two
tier VII vehicles are both named exactly `IS-2`. Ambiguity is therefore an error rather than a
silent pick, which also fixed `VehicleByName`, which used to return the highest tier.

## Step 9 — query layer

All `query` subcommands, the provenance envelope, confidence flags, WG-delta and Tomato-window
reported side by side, and `candidates` = researched-not-bought ∪ next research steps, each
with XP and credit cost plus affordability, filtered by `preferences.avoid_classes`.

*Acceptance:* golden-JSON tests from fixtures for every subcommand; every output carries
non-empty `meta.sources` with ages; `candidates` contains no SPGs given the seed overlay;
`query tank` accepts both a name and an id.

**Result (2026-09-18): done.** Eleven golden files under `testdata/golden/query/`, built from a
seeded account with snapshots at 40 days, 10 days and 1 hour, so lifetime, a 30-day window with
a baseline and a 60-day window without one each take their own path; every golden figure was
checked by hand before being pinned. Against the live account, `candidates` offers 25 vehicles
with one SPG excluded, and nothing already owned.

Decisions and findings along the way:

- **Random-only assist, labelled** (the user's call): from the totals, with a caveat on every
  result that the client shows an all-battles figure. `query tank` carries the all-battles
  assist and blocked damage beside it for comparison.
- **"Not the window you asked for" is said out loud.** With snapshots only as frequent as syncs,
  a 30-day window's baseline can be 40 days old. The span was already reported; it is now also
  a caveat, because a skimming reader sees "last 30 days" and not the span.
- **`path_xp` for a goal behind an unowned vehicle.** The Tesák costs 242,560 XP from the
  Šelma, but 428,050 from what is owned; quoting only the first would understate the grind by
  a whole tier.
- **Compact JSON by default**, `--pretty` for people: `garage` fell from 23 KB to 13 KB.
- **Reserves are counted, not listed.** The API names them only by numeric id, so listing thirty
  of them would be noise; active ones are listed because they change earnings now.
- **Name matching folds diacritics** (`Tesak` → `Vz. 71 Tesák`), adding no collisions on the live
  list.
- `sessions` has no battle log to work from, so its unit is the interval between two syncs, and
  each interval states its span.

## Step 10 — brief

Deterministic section budget summing to ≤10,000 characters, with per-section truncation notes.

*Acceptance:* a golden test asserts length ≤10,000 and the presence of "Data age" and
"Known gaps"; another asserts no secret material appears; content covers resources, garage by
tier and class, strongest and weakest tanks lifetime vs 30/60 d, overlay goals, timestamps,
and gaps.

**Result (2026-09-18): done.** `testdata/golden/brief.md` is pinned from the shared seed account
(`internal/testseed`, now used by both the query and brief tests), reviewed line by line;
`TestBriefCoversTheSpec` checks each required item by content, and the truncation test holds
the cap at 2,000, 3,000 and 5,000 characters with every heading surviving. The live brief is
4,558 characters. Rendering it exposed an error of mine: the overlay's `updated_at` had been
set to times later that day, so the brief claimed an overlay newer than itself. It is
corrected, and validation now warns on a future stamp. `wotctx mcp` is registered as pending
(phase 2), so every command in spec §7 exists.

## Step 11 — skill and install target

Write `SKILL.md` and the four references; paste the existing WoT analysis framework into
`references/framework.md`. `make install-skill` copies the directory to
`~/.claude/skills/wot-advisor/`.

*Acceptance:* `SKILL.md` is under 500 lines; in a fresh session in an unrelated directory,
"what tank should I get now?" auto-invokes the skill, runs `doctor` → `sync` → `brief` →
`query candidates`, and answers in the five-part format citing data age and at least one
sample-size caveat.

**Result (2026-09-18): built and installed; acceptance run pending.** There was no existing
framework to paste, so `references/framework.md` was drafted as a proposal and validated with
the player one question at a time (its §9 records the eleven decisions). Validation changed the
tool as well as the prose: the overlay gained `class_rank`, `free_xp_max_tier`,
`improvement_focus` and `credit_buffer`, which `candidates` and the brief now apply, and
`query performance` gained `--min-tier`. Two readings of the player's light-tank play were
corrected along the way — first "weakest class" (an all-tier artefact), then "level with
mediums" (true of WN8, not of win rate) — which is why the framework insists on tier bands and
two metrics. `SKILL.md` is 120 lines. Meta sources were checked with WebFetch itself: patch
notes and news are readable; tomato.gg's MoE table only partly; tanks.gg and the shop not at
all. `make install` was added so `wotctx` is on the PATH; both targets were run, and `wotctx
doctor` works from an unrelated directory.

## Step 12 — evaluation

`docs/evals.md` holds the questions below with their pass criteria. Run them manually and
record the results.

| Question | Pass criteria |
|---|---|
| What tank should I get now? | Uses garage, resources and overlay; no SPGs; ≥2 named candidates with cost and affordability; states data age |
| Is my AMX 13 90 underperforming vs my mediums? | Lifetime vs recent; uses the class rollup; states battle-count confidence on both sides |
| Should I spend free XP on my current research goal? | Reads real free XP; uses overlay `xp_goals` and `xp_banked`; explicitly says vehicle XP is not API-exposed; with no goal set, says so rather than inventing one (the original Executor goal was completed on 2026-09-18) |
| What's fitted on my TVP T 50/51? | Uses `loadouts` if present; otherwise says the mod data is missing, and why |
| How did my last week go? | Uses `query sessions`, not lifetime numbers |
| Which Tier X should I push for marks? | Combines `moe_progression` with recent form; fetches MoE requirements at question time and quotes the page timestamp |
| Is my light-tank play improving since 2.4? | Uses windowed data; refuses a trend claim if no snapshot predates the update, and says so |
| How fresh is my data? | Relays `doctor` accurately, including any known gap |

*Acceptance:* at least 6 of 8 pass without hand-holding; every failure has a filed follow-up.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| WG API lags new content; Tier XI may be absent | Step 5 probe; `research_paths:` overlay fallback with `source='overlay'` on the edges, so provenance stays honest |
| tomato.gg's MoE page is internal page data, not an API, and may change | `meta moe` fails loudly (no rows or no timestamp) rather than returning a partial table; nothing is stored, so a fix is a parser change |
| An upstream file freezes while still answering 200 (it happened: XVM's pre-split WN8 file stopped at 2024-09-12) | `doctor` warns when the expected-values version is over 60 days old |
| Client-mod data absent (phase 4 not built, or broken by a game update) | Optional by design; surfaces as a Known gaps line, never an error |
| WG token expires (~2 weeks) | Auto-prolong under 3 days remaining; `doctor` warns; `sync` fails loudly with the exact command to run |
| Secret leakage into git, logs or fixtures | Keychain-only storage; one redaction helper on all logging; fixture scrubber plus a test that greps `testdata/` for `tmgg_`, token and `application_id` patterns; `.gitignore` for the DB and `.env` |
| WG private data reaching a third party | The only non-Wargaming clients (XVM, tomato.gg) take no account input at all — an architectural guarantee, not a convention |
| Context bloat defeating the whole point | 10 k hard cap on the brief; `query` is JSON-only and narrow; the skill is instructed never to paste raw JSON into answers |
| Cumulative-only WG data inviting false trend claims | Deltas require a snapshot old enough; otherwise omitted with a caveat. Eval question 7 tests exactly this |
| Small samples driving confident advice | Confidence flag on every row; the skill must state it; no verdict rests on a single metric |
| `modernc.org/sqlite` is a large dependency | Accepted — it is the price of cgo-free cross-compilation. It stays behind `internal/store`, so it is replaceable |
| Claude auto-syncing burns the rate limit | 6 h staleness threshold before auto-sync; per-source TTLs; a full sync is about 20 Wargaming requests plus one XVM file. **Not** ETag/304: Wargaming ignores `If-None-Match` (measured), so TTLs are the only real defence |
