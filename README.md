# Tank Advisor

**A companion app for World of Tanks that lets Claude answer questions about your own account
with real figures instead of guesses.** Ask *"what tank should I get now?"*, *"which tier X
should I push for marks?"* or *"is my light-tank play improving?"*, and Claude answers from your
garage, your statistics and what the game client shows. It says how old the data is, flags
small samples, and never makes up a number.

Tank Advisor is not affiliated with or endorsed by Wargaming.

```
> What tank should I get now?

Short answer: Your next tank is the LPT-67 Šelma, the next step on your Blesk → Šelma →
Tesák goal. You need about 19,195 more XP on the Blesk — roughly 22 battles, or 3 one-hour
sessions. The XP will come quickly; the real problem is credits. You're about 2.71M short,
so play credit-earners alongside the Blesk.

Why: … The Blesk is your best tank by WN8 (2,507 WN8, 51.9 % win rate, 54 battles) —
that's only 54 battles, so it's an early sign, not proof …

Data: account and stats 12 min old, XVM WN8 values from 2026-09-12, overlay 2026-09-18.
```
*(Excerpted from evaluation run 2 in [`docs/evals.md`](docs/evals.md).)*

## Get it

You need Windows 10 or 11, World of Tanks on PC (EU, North America or Asia), a Wargaming
account, and [Claude Desktop](https://claude.ai/download) or Claude Code.

1. Download `TankAdvisor-setup.exe` from the
   [latest release](https://github.com/ondrejkouril/tank-advisor/releases/latest).
2. Run it. It installs for you alone and needs no administrator rights.
   **Windows may warn that it "protected your PC"**, because the first releases are not yet
   code-signed (the free signing service for open-source projects asks for a user base first).
   Click **More info**, then **Run anyway**. Every *update* is still checked against the
   project's own signature before it installs.
3. Tank Advisor starts and walks you through setup, with no terminal:
   - agree to what it stores;
   - pick your server;
   - log in on Wargaming's own page (Tank Advisor never sees your password);
   - confirm the game folder;
   - install the client mod;
   - connect Claude;
   - answer a few questions about how you like advice;
   - sync.
4. Open Claude and ask about your account.

## What it does

- **Keeps your data current by itself.** It syncs when you close the game (so each play
  session shows up whole), and once a day otherwise. It renews your Wargaming login before it
  runs out, and tells you if it lapses.
- **Reads what no Wargaming service gives out.** A small client mod reads each tank's XP, your
  progress towards the next mark, loadouts and crew while you sit in the garage, and writes
  them to a file on your computer. It only reads, only in the garage, shows nothing in battle
  and never touches the network. Tank Advisor puts it back after every game update.
- **Lets you shape the advice.** On the **Advice** page you can set:
  - how long a session is, and your class order;
  - what matters when choosing a tank, and how answers look;
  - rules of your own, in your own words.

  A preview shows what Claude will read. On the **Goals** page you set the tanks you're
  grinding towards. Your settings never switch off the core rules: Claude still quotes the
  data's age, sample sizes and sources.
- **Works with Claude Desktop, Claude Code and claude.ai.** Desktop gets an extension, Code a
  plugin, and claude.ai (and the phone) an export you upload.
- **Updates itself**, after checking each update's signature. The status window shows what is
  working and what needs a click. Every background job can be paused.

## Privacy

- **Everything stays on your computer**, under your Windows account: account statistics,
  garage, resources, and the mod's file. There is no Tank Advisor server; nothing is sent to
  the project.
- **Tank Advisor talks to Wargaming** through its public API and its own login page. It asks
  Wargaming only for the fields it uses. It reads the public pages of XVM (WN8 reference
  values) and tomato.gg (Marks of Excellence thresholds); those requests carry no account data.
  It asks GitHub whether an update exists.
- **When you ask Claude about your account, Claude receives the figures it needs to answer**,
  under your own agreement with Anthropic.
- **Delete my data** in the status window removes the synced history, the mod's file and your
  login. The uninstaller asks whether to do the same. History cannot be fetched again: recent
  form is built from snapshots taken over time.

**Two readings of Wargaming's terms, stated openly.** The
[API terms](https://developers.wargaming.net/documentation/rules/agreement/) were written for
web services, and two clauses sit awkwardly with a local tool:
1. They license API data on condition that no "permanent copies" are made. Recent form needs
   statistics kept over time. The project's reading is that a copy you hold, can delete in one
   click, and have removed on uninstall is not a permanent copy in that sense. Raw responses
   are pruned after 90 days, and a retention limit is one setting away (`sync.retention`).
2. They bar an application from transferring players' personal data to third parties.
   Tank Advisor transfers nothing. Your own assistant reads your own statistics at your
   request, as you could read them aloud. The project's reading is that this is your use of
   your own data, not a transfer by the application.

**The application id.** A standalone application necessarily carries its Wargaming application
id. It is kept out of the source and injected when a release is built. It is registered on a
Wargaming account kept for the project, so the project does not depend on anyone's playing
account. If Wargaming ever blocks it, syncing stops, the app shows Wargaming's message, and an
update brings a new one.

**Reaching the project.** Wargaming, tomato.gg, XVM or anyone else can raise a concern through
[the issue tracker](https://github.com/ondrejkouril/tank-advisor/issues). Every request
the tool makes names the project and links this repository. Whatever they ask for (a shorter
history, fewer fields, no tomato.gg fetches) is a setting that an update can change for every
player.

## Notices

- Tank Advisor © 2026 ondrejkouril, under the [MIT licence](LICENSE).
- Account data comes from Wargaming.net through its public API. © Wargaming.net. All rights
  reserved. Official World of Tanks website: [EU](https://worldoftanks.eu/),
  [North America](https://worldoftanks.com/), [Asia](https://worldoftanks.asia/).
  **[Wargaming Support](https://wargaming.net/support/)**.
- Tank Advisor is not affiliated with or endorsed by Wargaming, XVM or tomato.gg.
- WN8 expected values come from XVM. Marks of Excellence thresholds come from tomato.gg.

---

# For developers

The app is built around **`wotctx`**, a Go CLI and MCP server. It syncs the account from the
Wargaming API into a local SQLite cache, and answers narrow questions as small JSON, each
wrapped in a provenance envelope: which sources, how old, what is known to be missing.

## How it works

| Layer | What | Changes |
|---|---|---|
| **Data** | `wotctx` → SQLite cache → narrow JSON queries | every sync |
| **Overlay** | `wot-overlay.yaml`: goals, preferences and advice settings, edited by hand or in the app | when the player says |
| **Guidance** | [`skills/wot-advisor/`](skills/wot-advisor/): core rules, the player's advice rendered from the overlay, the framework as defaults | rarely, deliberately |

The CLI holds numbers and never gives a verdict. The guidance holds judgement and never
hard-codes a number. The overlay holds what only the player knows.

**Sources.**
- The Wargaming Public API: account, garage, resources, per-tank statistics, achievements,
  personal missions, and the vehicle encyclopedia and tech tree.
- XVM's WN8 expected values; WN8 is computed locally from them.
- At question time only: tomato.gg's Mark of Excellence table, and Wargaming's news pages.

Every raw response is stored gzipped before parsing, so a parser fix is a re-parse, never a
re-fetch. **Recent form comes only from local snapshots:** the API is cumulative and cannot be
backfilled, so "last 30 days" needs a snapshot from 30 days ago. Until one exists, the answer is
*"not yet: N days of history"*.

**The pieces.**
- **Tank Advisor** (`cmd/tankadvisor`, Wails v3) is the installer, the tray app and its window.
- **`wotctx`** (`cmd/wotctx`) is the CLI and MCP server. The app installs it and puts it on the
  `PATH`.
- **The Claude Desktop extension** (`packaging/mcpb`) carries only `wotctx-launcher`, which runs
  the installed `wotctx`. An app update therefore reaches Desktop with no reinstall.
- **The Claude Code plugin** (`.claude-plugin/`) and **the claude.ai export**.
- **The client mod** (`mod/`, Python 2.7).

## Build and run from source

Requirements: Go 1.25+ (built with 1.27), GNU make and a POSIX shell (Git Bash on Windows).
Node is needed to pack the Desktop bundle, NSIS to build the installer, and Python 2.7 to build
the mod. `wotctx` also cross-compiles for macOS and Linux (`make release`); the app is
Windows-only.

```sh
make install          # wotctx into $(go env GOPATH)/bin; put that on your PATH
make bundle           # dist/wotctx-<version>.mcpb, the Desktop extension with its launcher
make app              # dist/app/TankAdvisor.exe, with the newest bundle and mod beside it
make mod              # dist/ondrejkouril.wotctx_<version>.wotmod (needs Python 2.7)
make installer VERSION=v1.2.3 MOD_PKG=dist/ondrejkouril.wotctx_<version>.wotmod
```

A development build carries no Wargaming application id. Register your own at
<https://developers.wargaming.net> (My Applications → **Mobile**, the form's name for a
standalone app), then log in; the first login records the account:

```sh
wotctx auth wg --realm eu --application-id <your application_id>   # eu, com or asia
wotctx sync
wotctx doctor
```

On Windows a running `wotctx.exe` cannot be overwritten; `make install` needs it renamed first
(`mv ~/go/bin/wotctx.exe ~/go/bin/wotctx.old.exe`). Running copies carry on until they restart.
**A newer build may migrate the cache**, and an older `wotctx` then refuses it. Point
experiments at a copy with `WOTCTX_CONFIG_DIR` and `WOTCTX_DATA_DIR`.

## Using `wotctx` directly

**Claude Code:** this repository is a plugin marketplace with one plugin:

```sh
claude plugin marketplace add ondrejkouril/tank-advisor   # or the path of a clone
claude plugin install wot@tank-advisor
```

The plugin brings:
- the `wot-advisor` skill, which runs `wotctx guide` before its first judgement, so it follows
  the player's advice settings;
- the wotctx MCP server and `/wot:sync`;
- a sync in the background when a session starts.

**Claude Desktop:** install the `.mcpb` (Settings → Extensions); the app does it from its
Claude row. **claude.ai:** `wotctx export claude-ai` writes `wot-brief.md` and
`wot-advisor.zip`; upload the zip as a skill, and the brief to a Project.

| Command | What it gives |
|---|---|
| `wotctx doctor [--json]` | Auth, data age per source, overlay and WN8 status; each problem with its fix |
| `wotctx sync [--only wg\|wn8\|mod] [--full] [--dry-run] [--quiet]` | Fetch whatever is past its TTL |
| `wotctx guide [--topic X]` | The guide: core rules, the player's advice, the framework's defaults |
| `wotctx brief` | The whole account on one Markdown page |
| `wotctx query garage \| tank <name> \| performance \| resources \| sessions \| candidates \| missions` | Narrow JSON answers, each with its provenance |
| `wotctx meta moe <tank>` | Mark of Excellence thresholds, fetched now |
| `wotctx auth wg [--realm R] [--relogin] [--prolong] [--logout]` | Log in, renew, log out |
| `wotctx data delete [--yes]` | Delete the cache, the dump, the login and the recorded account |
| `wotctx mod install <package>` | Copy the client mod into the game's current mods folder |
| `wotctx overlay validate \| path` | Check or locate the overlay |
| `wotctx export claude-ai [--out DIR]` | The brief and the skill zip, for claude.ai |

The full contract is [`docs/spec.md`](docs/spec.md) §7.

## Development

```sh
make lint                   # go vet + gofmt check
make test                   # all tests; no network
go test ./internal/query ./internal/brief -update   # regenerate golden output, then review the diff
```

Tests run against recorded, scrubbed API responses (`testdata/fixtures/`) and a small seeded
account (`internal/testseed/`); query and brief output is pinned in `testdata/golden/`.
`internal/app`, the window's logic, has no Wails dependency, so it is tested like the rest; a
test keeps Wails out of `wotctx`, the launcher and `internal/app`. The guidance is evaluated end
to end in [`docs/evals.md`](docs/evals.md).

```
cmd/wotctx/           the CLI and MCP server      cmd/wotctx-launcher/  the Desktop extension's launcher
cmd/tankadvisor/      the app: Wails, tray, frontend/, updates, install modes
cmd/releasetool/      release key, signatures, SHA256SUMS
internal/app/         the app's logic: status, wizard, pages, duties, install
internal/cli/         command tree and output     internal/wg/     Wargaming client
internal/store/       SQLite, migrations, snapshots, deltas
internal/syncer/      what to fetch, TTLs, re-parse
internal/overlay/     overlay schema, validation, name resolution
internal/advice/      the player's advice, rendered for Claude, and rule conflicts
internal/yamledit/    edits YAML people write by hand, one line at a time
internal/query/       the query layer             internal/brief/  the Markdown brief
internal/game/        finding the game            internal/mod/    the client mod's dump
internal/release/     Ed25519 release signatures
mod/                  the client mod (Python 2.7) and its build
skills/wot-advisor/   the skill: SKILL.md and references/ (embedded for wot_guide)
packaging/mcpb/       the Desktop extension       packaging/nsis/  the installer
.github/workflows/    the release build
```

**Releasing.** Create a draft release with its tag (`v1.2.3`, or `v1.2.3-rc.1` for a
pre-release), attach the mod built with `make mod`, and save it. The release workflow checks:
- the committed public key, `cmd/tankadvisor/release.pub`;
- the `WG_APPLICATION_ID` and `RELEASE_SIGNING_KEY` secrets.

It then builds and signs everything, uploads it to the draft, and leaves it for you to publish.

## Documentation

- [`docs/spec.md`](docs/spec.md): the `wotctx` contract, with data sources as measured, storage,
  overlay schema, derived metrics and the CLI.
- [`docs/spec-desktop.md`](docs/spec-desktop.md): the Tank Advisor app and installer, advice each
  player shapes, and Wargaming's terms as read.
- [`docs/plan.md`](docs/plan.md): the build schedule, with how each step was checked.
- [`docs/evals.md`](docs/evals.md): the evaluation questions and results.
- [`skills/wot-advisor/references/`](skills/wot-advisor/references/): the core rules and the
  framework.

## Status

Phases 1–4 (the CLI, MCP, packaging and the client mod) are complete and evaluated. Phase 5,
the Tank Advisor app and installer, is built. Its first public release waits on a check by a
player other than the author, and on the release setup. See [`docs/plan.md`](docs/plan.md).

## License

[MIT](LICENSE)
