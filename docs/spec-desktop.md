# Tank Advisor: a desktop app and installer for any player. Specification

Status: as built, 2026-09-27 (plan steps D1–D9), with the owner's checks and the first release
still to come (plan steps D10 and D11). Written as a draft on 2026-09-26, with the decisions
settled with the owner the same day (§14), after reading Wargaming's developer terms (§12). Each
section now describes what was built; where the build changed the draft, the section says so.
This spec extends `docs/spec.md` (the `wotctx` contract), which was brought in line on
2026-09-27 (§13). `docs/plan.md` records each step and how it was checked.

*Tank Advisor* is a working name. It is deliberately not "WoT Advisor": Wargaming's terms grant
no right to use its trade marks (§12).

## 1. Purpose

Today the advisor works for one account on one machine. Setting it up takes Go, GNU make,
Python 2.7, a registered Wargaming application, a terminal login, a hand-written `config.yaml`,
a mod copied again after every game update, and a Desktop bundle rebuilt after every schema
change. That setup is what keeps anyone else from using it.

The goal is a player with Windows, World of Tanks and Claude Desktop, and no developer tools.
They download one installer and get from the download to a grounded answer in **under ten
minutes without opening a terminal**. After that, nothing needs doing by hand: not after a
game update, not after an app update, and not when the login token expires.

Three problems, all in scope:

1. **Installation and upkeep.** An installer, and a small desktop app that owns the mod, the
   Claude integration, the login, syncing and its own updates.
2. **Advice each player shapes.** The skill and framework were written for, and validated
   with, one player (the owner, EU). Every player sets up their own advice from the app: who
   they are, what matters when choosing a tank, how answers look, and rules of their own (§8).
3. **Wargaming's terms.** A public application has obligations a personal tool could ignore
   (§12).

Non-goals, unchanged from `docs/spec.md` §1 unless stated: no hosting, no server of ours, no
telemetry. Also out of scope: macOS and Linux (the game is Windows-only), Lesta's RU client,
several accounts in one installation (§15), and an app UI in any language other than English.
Claude answers in the player's language anyway.

## 2. What the player gets

| Part | What it is | Who updates it |
|---|---|---|
| **Tank Advisor** app | Tray app with a small window: setup wizard, status, advice settings, goals, updates | Itself (§9) |
| `wotctx.exe` | The existing CLI and MCP server | Updated with the app |
| Client mod | `ondrejkouril.wotctx_<ver>.wotmod`, prebuilt | The app, after each game update (§6) |
| Claude Desktop extension | A `.mcpb` with a launcher (§7.1) | Installed once. It needs reinstalling only when its manifest changes |
| Claude Code plugin | The existing `wot` plugin, optional | Claude Code's marketplace |
| claude.ai export | The existing brief and skill zip, from a button | On demand |

The app never installs Claude itself. It detects Claude Desktop and links to the download page
when Claude Desktop is missing.

## 3. Architecture

```
            ┌────────────────────── %LOCALAPPDATA%\Programs\Tank Advisor\ ────────────────────┐
            │  TankAdvisor.exe  (GUI + tray; imports internal/*)      wotctx.exe  (CLI + MCP) │
            │  wotctx-launcher.exe  (copied into the .mcpb)           mod\*.wotmod            │
            └──────────────────────────────────────────────────────────────────────────────────┘
                 │ sync, auth, doctor,                │ stdio MCP               │ copy
                 │ overlay edit (in process)          │                         ▼
                 ▼                                    │               <game>\mods\<version>\
   %LOCALAPPDATA%\wotctx\wotctx.db   ◄────────────────┘                         │ writes
   %APPDATA%\wotctx\config.yaml, wot-overlay.yaml                              ▼
   Windows Credential Manager (service "wotctx")          %LOCALAPPDATA%\wotctx\mod\garage.json
```

- **Two executables, one code base.** `TankAdvisor.exe` is a new `cmd/tankadvisor` built with
  Wails v3 (Go + WebView2). It imports the same `internal/` packages as `wotctx` and calls them in
  process: sync, auth, doctor, the overlay, the mod install. `wotctx.exe` stays a console
  program, because the MCP server needs stdio and the plugin and terminal users need a CLI. No
  logic is duplicated.
- **The window's logic has no window.** `internal/app` builds the status rows and runs the
  actions behind the buttons, importing `internal/cli` and nothing of Wails, so it is tested
  like the rest of the core. `cmd/tankadvisor` only draws it: a plain HTML and JavaScript page
  that calls `internal/app` through Wails' runtime, with no Node build step.
- **New dependencies stay in `cmd/tankadvisor`.** `wotctx` keeps the dependency list of
  `docs/spec.md` §11. A test fails if `wotctx`, the launcher or `internal/app` links Wails.
- **Paths do not move.** The database, config, overlay default, mod dump and keychain service
  stay where `docs/spec.md` puts them. An existing installation, the owner's included, is
  picked up as it is (§10).
- **One writer at a time.** The app's background sync and a sync from Claude share the
  existing `sync_runs` lock (`docs/spec.md` §7.4).
- **The UI looks like itself.** Wargaming forbids interface elements that mimic its products
  (§12). So there are no game fonts, icons, tank-garage styling or Wargaming logos.

## 4. Installer

A per-user **NSIS** installer (`packaging/nsis/tankadvisor.nsi`) that needs no administrator
rights. It copies one file, `TankAdvisor.exe`, and runs it with `--install-payloads`, which does
the rest in Go, where it is tested. The installer and uninstaller keep only the questions, the
shortcut and their own registry entry. The app installs to `%LOCALAPPDATA%\Programs\Tank Advisor\`, adds that folder to the **user** `PATH`
(for `wotctx` and the Claude Code plugin), creates a Start menu entry, and registers an
uninstaller under `HKCU`. Its last page starts the app, and the app's wizard does the rest. The
installer asks nothing, apart from its licence page, which carries the notices of §12.

Why the wizard and not the installer: login, game detection, the mod and Claude all need
re-doing later (a token lapses, the game moves, Claude is installed afterwards). If they live in
the app, there is one implementation of each, and it serves both the first run and repair.

**Code signing** goes through the **SignPath Foundation**, which signs open-source projects for
free. SignPath asks for proof that the application has users other than its author, so the first
releases are **unsigned**: the release notes and the README explain Windows SmartScreen's
*More info → Run anyway*. The project applies again once players use it. When SignPath accepts
it, both the installer and the executables are signed, and the release workflow has the step
marked for it. Authenticode is about the first download only. Every update is checked against
the release key's Ed25519 signature either way (§9).

**Uninstall** removes the app, the mod from every `mods\<version>\` folder it placed it in (the
app keeps a list in `config.yaml`), the autostart entry, the `PATH` entry, and the window's
browser cache in `%LOCALAPPDATA%\Tank Advisor\WebView2`, and the notification registration
that Wails writes under `HKCU\Software\Classes\AppUserModelId\`. It asks separately
before deleting the keychain entries and the database, and says plainly that snapshot history
cannot be fetched again (`docs/spec.md` §6.1). It cannot remove the Claude Desktop extension, so
it tells the player where to do that.

## 5. The app

### 5.1 First-run wizard

| Step | What happens | Done when |
|---|---|---|
| 1. Welcome | What the app stores, where the data goes (§12.3), and that it is not affiliated with Wargaming. The player agrees or quits | agreed. The consent is recorded in `config.yaml` as `consent.notice` (the notice's version) and `consent.agreed` (the date). A newer notice opens the wizard again |
| 2. Server | Pick EU, NA or Asia (`account.realm`), preselected from the game found (§6.1). Locked once an account is recorded | chosen |
| 3. Log in | "Log in with Wargaming" opens the browser; the existing local-callback flow (`docs/spec.md` §3.1) returns `account_id`, `nickname` and the token. The app never sees a password (§12). A token left in the keychain without a recorded account does not count: the login runs, because it is what records the account | token in the keychain; the account written to `config.yaml`, never typed |
| 4. Game | Find the game folder (§6.1), show every candidate, and let the player pick one or choose another folder (the game folder or its `win64\`). A test client, or another realm's client, is refused. Only a folder that is not the best one found is written down, as `game_dir`, so that otherwise detection keeps following the game | `version.xml` readable |
| 5. Mod | Install the mod for the current game version; ask the player to start the game and wait in the garage for a few seconds. The page watches for the dump and says when the game is logged in as another account | the first `garage.json` for this account and this game version arrives. The step can be skipped, and then XP, marks, loadouts and crew are unknown, as today |
| 6. Claude | Detect Claude Desktop and Claude Code. Offer the extension, the plugin, or both (§7) | the integrations the player chose are installed |
| 7. Your advice | A short questionnaire: session length, battles per hour, experience, classes never to recommend, answer length and format (§8.3). Every question has a default and a *Skip*; only the answers the player changes are written, so later changes to a default still reach them | the overlay validates |
| 8. First sync | Run `sync`, then show three example questions to ask Claude. A sync that brought no account data back says so, with Wargaming's reason, since `sync` itself counts a failed source as a caveat | the account's own data (`account/info`) is in the cache. Finishing records `setup.completed` |

Every step can be re-run from the status window. The wizard never shows a command line, and an
error says what to click, not what to type.

### 5.2 Status window

One screen, a row per item, each with its state and a button to fix it. It is `wotctx doctor`
with buttons:

- **Wargaming login.** Account, realm, token expiry. *Renew*, *Log in again* and **Log out**.
  Log out calls `auth/logout`, which `internal/wg` already has, and deletes the token. It is
  required by Wargaming's policy (§12). `wotctx auth wg --logout` is added for the CLI.
- **Data.** The last sync, the age of each source, the longest gap in history. *Sync now*, and
  **Delete my data** (§12.3), which first says what goes and what stays.
- **Client mod.** Installed for the current game version or not, the mod version against the
  one the app carries, the last dump's capture time, battles played since the dump, and errors
  from the dump. *Install*, *Update* or *Reinstall*; *Sync now* when the mod has written a dump
  that is not synced yet; *Open mods folder*.
- **Claude.** Extension, plugin and export state. The extension's installed version is compared
  with the bundle the app carries. *Install extension* (or *Update extension*), and *Export for
  claude.ai*, which writes to `Documents\Tank Advisor\claude.ai` and opens that folder. The
  plugin's state is shown; installing it is the wizard's (§5.1, step 6). Without Claude
  Desktop, *Get Claude Desktop* opens its download page.
- **Tank Advisor.** The app version and whether a newer one exists (§9). *Update*. *Start with
  Windows* on or off; started that way, the app opens in the tray without its window.
- **About.** The notices and links of §12.2, including a clearly marked **Wargaming Support**
  button.

### 5.3 Goals

A form over the overlay's `xp_goals` and the backup-only fields (`premium`,
`researched_not_bought`, `xp_banked`). The backup fields show only when there is no mod dump,
which matches the existing rule that the dump wins. Tank fields are pickers backed by the
`vehicles` table and the resolution rules of `docs/spec.md` §5, so an ambiguous name cannot be
saved. A goal's `via` is offered from the tech tree and fills in `xp_required` from it.

### 5.4 Advice

The page of §8.4. It is where each player customises the advice.

### 5.5 Editing the overlay

Both pages edit `wot-overlay.yaml`, which stays hand-editable. The app finds each key through
`yaml.v3`'s nodes, but rewrites only the lines a change touches (`yamledit.Apply`). Re-encoding
the whole file through `yaml.v3` would keep the comments' words, but not the blank lines
between sections, the aligned comment columns, or where a comment's continuation lines sit. A
changed value keeps its comment at the same column. A list is rebuilt from its old items' own
text, so an unchanged item keeps its comments, and a page saved unchanged writes nothing.
Before saving, the app validates the file it would write, including the tank names against the
synced vehicles. It also refuses to save over a file that changed on disk since the page loaded
it. A page with nothing unsaved reloads by itself when the file changes. An overlay that fails
validation is never overwritten; the app shows its errors instead.

### 5.6 Background duties

The app starts at logon (an `HKCU\...\Run` entry, which the player can switch off) and sits in
the tray. It does four things, all through existing code:

| Duty | Trigger | Notes |
|---|---|---|
| **Sync** | when `WorldOfTanks.exe` exits, after the mod's dump has gone unchanged for 5 s (it writes 3 s after the lobby settles); otherwise when the account's data is a day old | Sync boundaries then fall between play sessions, which makes `query sessions` intervals match real sessions. This closes the coverage gap of `docs/spec.md` §7.4 on days Claude is not opened. A click in the window comes first; the duty waits for it |
| **Token upkeep** | hourly check | `auth/prolongate` when fewer than 3 days remain (existing). If the token has lapsed, a Windows notification offers *Log in again*, at most once a day. A logout is not a lapse |
| **Mod upkeep** | `version.xml` changes (polled every 3 minutes, and at start) | §6.2. Only once the app has installed the mod (`mod.managed`). It never asks for permission by itself: a folder that needs it waits for a click, and the status window says so |
| **Update check** | daily, and on demand | §9. A newer release is announced once, as a notification |

Background work uses the Wargaming API and XVM's file only. It never reads Wargaming's
websites, which the policy allows only through the API when no user is involved (§12).
Budget: idle means no network, and next to no CPU: the app looks for a running game every 30 s, runs the
clock-driven duties once an hour, and checks the mod every 3 minutes, each a few file reads. While
the game runs, the app waits on its process handle. The player can pause every duty, each on its
own, from the status window's *Background* row (`duties.paused` in `config.yaml`).

## 6. The client mod, managed

### 6.1 Finding the game

Candidates, in order, measured in plan step D1:

1. the existing `game_dir` in `config.yaml`;
2. **Game Center's list**: `%ProgramData%\Wargaming.net\GameCenter\preferences.xml`, every
   `games_manager/games/game/working_dir`, with `selectedGames/WOT` first. Game Center keeps no
   per-user registry record;
3. the folder of a running `WorldOfTanks.exe`, the parent of its `win64\`, read from the
   process list, which needs no privileges;
4. Steam: `steamapps\common\World of Tanks\` in each Steam library, and one level below it.
   This is unverified until a Steam player confirms it (plan D10);
5. the default `C:\Games\World_of_Tanks_*`.

A candidate counts only if its `game_info.xml` says `WOT.<REALM>.PRODUCTION`. That skips the
Common Test (`WOT.CT.PRODUCTION`), which Game Center lists beside the real client. The realm in
that id preselects the server in the wizard, and `doctor` warns when it differs from
`account.realm`. The player confirms the result.

### 6.2 After a game update

The existing `wotctx mod install` logic runs unattended: when `version.xml` reports a new
version, the app copies the mod into the client's mods folder. That folder is the one
`paths.xml` names (`<Path mask="*.wotmod" …>./mods/2.4.0.1</Path>`), with the version from
`version.xml` as the fallback. The two agree for a production client, but not for the Common
Test, whose folder is `mods\2.4.1.0 Common Test`. Game Center updates
while the game is closed, so the copy normally lands before the first launch on the new
version, and there is no locked file to fight (`docs/spec.md` §3.6: the game only holds a
package it has loaded).

If the game folder is not writable (a `Program Files` install), the app asks for elevation for
that one copy and explains why.

**Compatibility.** A game update can break the mod without stopping it: a failed part is left
out of the dump and named in its `errors` (`docs/spec.md` §3.6). The app shows those errors and
checks for an app update that carries a fixed mod. It does not remove a working-but-degraded
mod, because a degraded dump still beats none.

### 6.3 Shipping the mod

The mod ships prebuilt inside the installer, so players never need Python 2.7. Hosted CI
runners no longer set up Python 2.7, so at first the maintainer builds the mod (`make mod`) and
the release pipeline takes the `.wotmod` as an input (§9.1). Moving the mod build into CI with a
Python 2.7 container comes later.

After the first public release, the mod is also submitted to the **official WoT Mod Hub**
(wgmods.net). Wargaming's fair-play guidance tells players that Mod Hub mods are the safe way
to avoid penalties. A player worried about a ban can then check the mod there. The app still
installs its own bundled copy, so there is one update path.

The mod stays inside Wargaming's rules as `docs/spec.md` §3.6 describes: read-only, lobby
only, no input, no network, nothing shown in battle. It also shows **no advertising or
branding in the client**, which the policy forbids in client modifications.

## 7. Claude integration

### 7.1 Claude Desktop: an extension that never goes stale

Today's bundle carries its own `wotctx.exe` (`docs/spec.md` §10). Every code change then means
rebuilding and reinstalling the bundle, and two binaries of different versions share one
database. For a player who never runs `make`, that is where things break.

Instead, the bundle's `server/` holds **`wotctx-launcher.exe`**, a tiny Go program. It finds
the installed `wotctx.exe` (the installer records the path under `HKCU\Software\Tank Advisor`,
with the default path as the fallback), starts it with `mcp` and inherited stdio, and exits with
its exit code. When the app is missing, the launcher serves a single tool, `wot_data_status`,
whose result says to reinstall the app. It never just fails silently.

What that buys:

- **The extension is installed once.** App updates reach Claude Desktop at its next start,
  with no bundle rebuild and no reinstall. The bundle changes only when its manifest does, that
  is, when a tool is added or renamed. The app compiles in the manifest's tool list, notices the
  change, and offers a reinstall.
- **One binary over the cache.** The version-mismatch guard stays, but it only trips in the
  moment after an update. The app says *restart Claude Desktop* when an update adds a
  database migration.
- The launcher is copied into the bundle at build time.

**Installing it** (measured in plan step D1). Claude Desktop registers no file association for
`.mcpb`, so opening the file through the shell would bring up Windows' "How do you want to
open this?" prompt. The app runs `%LOCALAPPDATA%\AnthropicClaude\claude.exe <bundle>`
directly instead. That path is Claude's update stub, so it survives Claude's own updates. Claude
Desktop then shows its install dialog, and the player confirms. If that fails, or Claude
Desktop is installed some other way, the app falls back to the documented route. It opens
Explorer at the bundle and says to drag it onto *Settings → Extensions*. The last resort is an
`mcpServers` entry in `claude_desktop_config.json`.

**Knowing it is installed.** Claude records each extension in
`%APPDATA%\Claude\extensions-installations.json`: its id (`local.mcpb.<author>.<name>`), its
version and hash, and whether it is signed. The status window reads that file. The format is
undocumented, so the app only reads it, and a file it cannot parse shows as "unknown", not as
an error. The bundle is unsigned today. Signing it with `mcpb sign` is a D4 question.

**Plans.** Desktop extensions work in "the Claude desktop app, signed in to claude.ai" on any
plan. A Team or Enterprise organisation can turn them off or restrict them, and then
*Extensions* is missing from settings. The wizard says that in one line when it cannot install
the extension.

### 7.2 Claude Code: the existing plugin

For players who use Claude Code, the wizard runs `claude plugin marketplace add
ondrejkouril/tank-advisor` and `claude plugin install wot@tank-advisor`.
The plugin already runs `wotctx` from the `PATH`, which the installer now sets. Claude Code's
marketplace keeps the plugin current, so the app only checks that it is installed.

### 7.3 claude.ai and the phone

*Export for claude.ai* runs `wotctx export claude-ai` into a folder it opens, and repeats the
existing two upload instructions. The exported skill zip includes the player's rendered advice
(§8.5), so the phone follows the same settings. The app says to export again after changing
them.

## 8. Advice each player shapes

### 8.1 What is personal today

| Where | Personal content | Becomes |
|---|---|---|
| `config.Default()` | the owner's account id and nickname, EU, `C:\Games\World_of_Tanks_EU` | no account default. The wizard writes the account, and `doctor` reports "not set up" until it has |
| `SKILL.md` description | the owner's server and nickname, and their mission chains | generic: "the player's own account" |
| `framework.md` §1 | who the player is: experience, one-hour reserve sessions, premium, class order, autoloaders, light-tank focus | the **profile**, from the overlay (§8.3) |
| `framework.md` §4.1, §4.2, §5 | the owner's weights, credit buffer, free-XP limit, answer format | **defaults** the player can change (§8.3) |
| `framework.md` §6 "Never recommend SPGs" | the owner's rule | follows `avoid_classes`, no longer a fixed "never" |
| `framework.md` §4.9 | the mission chains the owner is on | generic rules, with chains named only as examples |
| `framework.md` §9 | the validation record | kept, labelled as where the defaults came from |
| mcpb and plugin descriptions | "the player's validated rules" | "your own advice settings on a tested framework" |

**Rule:** after this change, nothing in `skills/` or `internal/` names an account, a
nickname, or a preference the player did not give. A test greps for the owner's nickname and
account id outside `testdata/` and the docs.

### 8.2 Three layers of advice

The framework splits into three layers, in this order of precedence:

1. **Core rules: fixed, not customisable.** These make an answer *grounded*. Numbers come from
   the tools, never from memory. State data age and sample size. No verdict on one metric. Keep
   the player's stats, server-wide data and opinion apart. No trend without a baseline. Take
   premium from the dump or overlay, never the API. Take XP and marks from the dump or say they
   are unknown. Never paste raw JSON. This is today's `framework.md` §2, §3, §6 (less its
   SPG line) and the provenance parts of §5. A player can change what the advice
   *prefers*, never whether it is *true*.
2. **The player's settings.** The profile, decision weights, answer style and the player's own
   rules (§8.3). They override the defaults they touch.
3. **Defaults.** Today's validated framework, used for anything the player has not set.

The guide tells Claude this precedence in plain words. A player rule that conflicts with a core
rule (for example "just guess my XP if you don't have it") is shown in the app as conflicting,
and it reaches Claude with the precedence restated, so the core rule still applies.

### 8.3 What a player can set

Everything lives in the overlay. The existing `preferences` and `constraints` keep their
meaning, and a new `advice` block holds the rest:

```yaml
preferences:                   # existing
  class_rank: {mediumTank: 1, lightTank: 2, AT-SPG: 3, heavyTank: 3}
  avoid_classes: [SPG]
  strengths: [medium tanks, autoloaders]
  improvement_focus: [lightTank]
  free_xp_max_tier: 8
constraints:                   # existing
  playtime: limited
  credit_buffer: 500000

profile:                       # new
  session_minutes: 60          # the unit for time estimates ("about 4 sessions")
  battles_per_hour: 8          # the default estimate; replace with your own
  experience: auto             # auto | new | returning | experienced

advice:                        # new
  weights:                     # how "what next?" options are weighed (framework §4.1)
    fit: highest               # highest | high | medium | low | tiebreak | ignore
    time: high
    credits: medium
    earning: low
    meta: tiebreak
    goals: tiebreak            # "goals come first" stays a default rule, not a weight
  answer:
    length: normal             # short | normal | detailed
    format: sections           # sections (Short answer / Why / Best setup / Watch-outs / Verdict) | plain
    explain_basics: auto       # auto (from experience) | always | never
    also_worth_knowing: true   # the one unasked-for line
  coaching: true               # a coaching angle, and unasked progress notes, for improvement_focus
  rules:                       # the player's own rules, in their own words
    - text: Never suggest spending gold on premium tanks; I only use bonds.
      added: 2026-09-26
```

- `experience: auto` derives from the data (random battles and account WN8), with the
  thresholds in the framework, not the code: the code emits numbers, not verdicts. An
  explicit value is for a returning player whose old statistics mislead.
- `advice.rules` is what the owner's §9 decisions were: the player's own judgement, written
  down. At most **20 rules of at most 300 characters each**, so `wot_guide` stays small.
- Validation follows `docs/spec.md` §5: unknown keys and bad enum values are errors, and a class
  both ranked and avoided is a warning. `version` stays `1`, because every new key is optional.
  An older `wotctx` fails loudly on the unknown keys, which is right.

### 8.4 The Advice page

One page in the app, in four parts:

- **About you.** Session length, battles per hour, experience, and a table of the five classes:
  a preference rank from 1 up (ties allowed, as `class_rank` allows), never recommend, and
  getting better at. Then strengths. The table replaced dragging to rank, because dragging
  cannot show a tie.
- **What matters when choosing.** The six weights as one row each with a segmented control, and
  the free-XP tier limit and credit buffer beside them.
- **How answers look.** Length, format, basics, the extra line, coaching.
- **Your rules.** A list the player can add to, edit, reorder and delete. Each rule shows a
  warning when it conflicts with a core rule or with a setting above (a rule about SPGs while
  SPGs are avoided, say).

Beside all four, **What Claude reads** shows a live preview of the rendered guide section
(§8.5), so the player sees the effect of each change in words. The preview is rendered from the
very file a save would write, so an error that would block the save shows in the preview first.
A field left empty shows its default, marked as such, and is not written. **Reset to defaults** is per
part. Changes take effect at Claude's next `wot_guide` call, with no restart. The page says so,
and suggests starting a new conversation.

### 8.5 How the advice reaches Claude

`wot_guide` (default topic) returns, in order: the core rules; a **Your advice** section
rendered from the overlay, in plain words, not YAML; then the default framework, with each
default the player has overridden replaced by a pointer to their setting. Preferences are
marked as the player's stated preferences. The existing rule in `framework.md` §3.4, that
preferences are respected but not trusted as claims about skill, still applies.

The same renderer serves every surface:

- A new CLI command, `wotctx guide [--topic X]`, mirrors `wot_guide`. The existing test that
  holds CLI and MCP to byte-for-byte equality covers it.
- `SKILL.md` tells Claude Code to run `wotctx guide` before its first judgement, rather than
  reading `references/framework.md` directly, so the plugin follows the player's settings too.
- `wotctx export claude-ai` writes the rendered guide into the skill zip (§7.3).

### 8.6 Evaluation beyond one account

Every evaluation so far ran on the owner's account. Before a public release:

- **The owner's settings reproduce today's advice.** The owner's overlay gets a profile,
  weights, answer style and rules equal to today's `framework.md` §1 and §9. The ten questions
  of `docs/evals.md` must still score **10/10**. This shows the split lost nothing.
- **Settings change answers.** Three extra questions are asked twice with different settings:
  `format: plain`, `meta: high`, and a player rule. Each pair must differ in the way the
  setting says, and each must still pass the core rules.
- **Another real account.** The ten questions are re-run on at least one other real account (a
  friend, or an alt on another realm), with default settings. That player checks the figures
  against their own client: plausible API data has been wrong twice on the owner's account (the
  garage size and premium status), and each time only the client showed it.
- **A second realm.** One non-EU realm is synced end to end. NA and Asia are configured today
  but have never been tested.

## 9. Updates

Everything the player installs is kept current by one mechanism, apart from what Claude keeps
current itself:

| Part | How it updates |
|---|---|
| App, `wotctx.exe`, launcher, bundled mod, skill text (embedded in `wotctx`) | App self-update, below |
| Mod in the game folder | App, after a game update (§6.2) or an app update that carries a newer mod |
| Desktop extension | Not needed. The launcher runs the current `wotctx` (§7.1); reinstall is offered only when the manifest changes |
| Claude Code plugin | Claude Code's marketplace |
| Account data, WN8 table, token | Background duties (§5.6) |

**Self-update uses Wails' own updater** (decided after plan step D1, replacing a hand-written
one). Releases are GitHub Releases on `ondrejkouril/tank-advisor`. Links are
permanent and go straight to GitHub, with no referral pages, as the policy requires (§12). The
updater reads the newest release and shows its notes. It downloads the app's executable and
checks it against a `SHA256SUMS` file in the release. It also checks an **Ed25519 signature**
(`TankAdvisor.exe.sig`, over the file's SHA-256) against the public key compiled into the app
(`cmd/tankadvisor/release.pub`), and it refuses a release it cannot verify. Then it swaps the
executable and restarts. Wails' own GitHub provider takes a checksum if it finds one and never a
signature, so the app has its own provider. It refuses to offer a release that lacks the
executable, the sums or the signature. A build with no public key compiled in, as every
development build is, updates nothing and only links the release page. A pre-release build
follows pre-releases; a release build only releases. Updates are offered, not forced.
*Install updates automatically*, an option off by default, is not built yet.

**One executable is the whole update.** The updater swaps a single file, so `TankAdvisor.exe`
**embeds** `wotctx.exe`, the `.mcpb` (which holds the launcher) and the `.wotmod`. At start, the app compares
each embedded payload's hash with the file on disk and writes any that differ. So an update is
one signed download, and the installer is only needed for the first install.

**Replacing a running `wotctx.exe`.** Claude Desktop may hold it open. Windows allows renaming a
running executable, so the app renames it to `wotctx.old.exe`, writes the new one, and deletes
the old copy on a later start. That is the same trick the README gives for `make install`.
Running MCP servers keep the old code until Claude Desktop restarts.

### 9.1 Release pipeline

GitHub Actions on a Windows runner (`.github/workflows/release.yml`) builds `wotctx` and the
bundle, then the app with them embedded, then the installer (`make installer`). It runs when
the maintainer runs it with the tag of a draft release they made with the mod attached (a
draft's own events start no workflow), and it uploads the installer, the
app, `SHA256SUMS` and the signature to the draft. The maintainer then publishes it. It injects the Wargaming application id from
a repository secret (§12.1). It Authenticode-signs through SignPath, and publishes the release
with `SHA256SUMS` and the Ed25519 signature the updater checks. The `.wotmod` is a
maintainer-built input at first (§6.3). The Ed25519 private key lives only in a repository
secret and in the maintainer's offline backup.

**New network traffic.** The update check is a GET to `api.github.com` and a download from
`github.com`. It carries no account data.

## 10. Existing installations

The owner's machine already has a keychain entry with a personal `application_id`, a
`config.yaml` with `overlay_path` pointing at this repository, a hand-installed plugin, and a
self-built bundle. The app takes all of it as it finds it:

- An `application_id` in the keychain overrides the built-in one (§12.1).
- An existing `config.yaml` is read, and the app writes only keys it owns (`account`,
  `game_dir`, `mod_installs`, `consent`) through the same comment-preserving path as the
  overlay.
- `overlay_path` is honoured, so the owner keeps editing the overlay in git, and the Advice page
  edits that file.
- The wizard detects the self-built bundle and offers to replace it with the launcher bundle.

## 11. Load on others

- **Wargaming.** A standalone application's limit is per IP address, at "in general 10 requests
  per second" (developer guide, *Limitations*). So players do not share a budget, and each still
  self-caps at 5 a second (`docs/spec.md` §3.1). A full sync is about 20 requests.
- **XVM.** Its expected-values file is a static download fetched weekly, which is negligible.
- **tomato.gg.** Its MoE page is fetched only when a player asks about marks, but with many
  players this becomes regular scraping of someone else's site. The project does not ask
  tomato.gg in advance (§12.4). It sends an honest `User-Agent` naming the project and its
  repository URL, so tomato.gg can see who is fetching and reach the project. If tomato.gg
  objects, the fetch is switched off with `meta.moe_fetch: false`, which an update can also
  ship as the default. Without it, `meta moe` still gives the player's marks from the dump, and
  says the thresholds are unavailable.

## 12. Wargaming's terms, checked 2026-09-26

Read: the *Wargaming API Terms of Use* (effective 25 May 2018) and the *Wargaming API Policy*,
both at `developers.wargaming.net/documentation/rules/agreement/`, and the developer guide
*Using API*. The terms also bind developers to Wargaming's EULA and Privacy Policy. The game's
Fair Play policy on mods was checked too.

**The basic fit is good.** The licence is granted "for developing publicly available
Applications" (ToU §8), so distributing the app is the intended use. A free app may take
donations (Policy §4). Login must go through the Wargaming ID only, and the app never asks for
a password (Policy §3.2, ToU §5), which the existing OpenID flow already does.

### 12.1 The application id

ToU §5 says to use "the API key (application_id) that We give You" and to keep it "secure", and
the guide says to make sure it "is not revealed to third parties". But a standalone application
is by definition "client-to-server", with requests "sent from different IP addresses", and
"only application_id is validated". So a distributed standalone app necessarily carries its
id. Registering a *standalone* application is how Wargaming expects this to work.

How the design keeps the id as private as a desktop app can:

- The id is **not in the public repository**. CI injects it at build time from a repository
  secret (`-ldflags`), so the id is not in the source, only in the built binary.
- It is registered on a **dedicated Wargaming account for the project**, not the owner's
  playing account. ToU §10 says developers lose "all Applications" if their Wargaming account
  is deleted, so a ban or deletion of the playing account would otherwise cut off every player.
- Redaction continues to cover it in logs and errors.
- `auth wg --application-id` stays, for anyone who wants their own id.

What remains: the id can be extracted from the binary, and Wargaming "may specifically suspend
or terminate your use … at any time for any reason" (ToU §8). If the id is blocked, every
player stops syncing until an update ships a new one. The app shows Wargaming's error verbatim
and points to the update check.

### 12.2 Notices the app must carry (Policy §1)

In the app's About page, the installer's licence page and the README:

- the developer's copyright notice, and "© Wargaming.net. All rights reserved" (§1.8);
- a link to the official game website for the player's realm, and a statement that the data
  comes from Wargaming.net (§1.9);
- nothing implying affiliation or endorsement. The app states "not affiliated with or endorsed
  by Wargaming" (§1.9, ToU §9);
- a clearly marked button linking to Wargaming's Customer Support Center (§1.10);
- a **Log out** function, since the app uses authorisation (§1.3; §5.2 above).

Also: no Wargaming trade marks without written permission (ToU §12), which is why the app has
its own name (§14, D7) and no Wargaming logos, and no interface mimicking Wargaming's (Policy
§1.6).

### 12.3 Personal data

What the terms say:

- ToU §10 requires notifying users about the data collected and how it is used, collecting only
  what is needed, protecting it, deleting it on Wargaming's request, and deleting data no longer
  used.
- ToU §6 requires end-user consent to access the platform through the API.
- Policy §3.3 prohibits "collection, storage or transfer to third parties of personal data
  (including email address and password) of Wargaming users".
- ToU §9 licenses API data on condition that the licensee does not "create permanent copies".

How the design meets them:

- **Consent and notice.** Wizard step 1 says what is stored (account statistics, the garage,
  resources, the mod's dump, all on this computer), where requests go (Wargaming; XVM's and
  tomato.gg's public pages, which carry no account data; GitHub for updates), and that
  **when the player asks Claude about their account, Claude receives the figures it needs to
  answer, under the player's own agreement with Anthropic**. The consent is recorded, and the
  Wargaming login itself is the consent that ToU §6 asks for.
- **Nothing reaches us.** The project has no server. The app and `wotctx` send account data
  only to Wargaming. The data sits on the player's own computer, under their Windows account.
- **Minimise.** `account/info` is asked for only the fields the tool uses, through the API's
  `fields` parameter. That drops `ban_info`, `restrictions`, `is_bound_to_phone` and whatever
  else goes unread, and `grouped_contacts` (other players' data) is never requested. Tool results
  sent to Claude are audited to carry no account id or other identifier the answer does not
  need.
- **Delete.** *Delete my data* in the status window, and the uninstaller's option, remove the
  database, the dump and the keychain entries. Raw response bodies, which exist only for
  re-parsing after a parser change, are pruned after 90 days. The parsed statistics stay, since
  they are what recent form is built from.

**Two readings the project takes.** The terms were written for web services, and two clauses
sit awkwardly with a local tool:

1. **"Permanent copies" and snapshot history.** Recent form needs statistics kept over time on
   the player's machine (`docs/spec.md` §6.1). The project's reading is that a copy the player
   holds, can delete in one click, and can have removed on uninstall is not a permanent copy in
   the sense of ToU §9.
2. **"Transfer to third parties" and Claude.** Policy §3.3 bars the application from
   transferring personal data. The app transfers nothing. The player's own assistant reads the
   player's own statistics at the player's request, as the player could read them aloud. The
   project's reading is that this is the player's use of their own data, not a transfer by the
   application.

These readings, and the application-id arrangement of §12.1, are the project's own. They are
written down in the README's privacy section so anyone can see them. They are not sent to
Wargaming for confirmation (§12.4).

### 12.4 Wargaming and tomato.gg speak first

The project does not contact Wargaming or tomato.gg in advance: nothing is sent for approval,
and no release waits on an answer. If either raises a concern, the project answers and changes
the design to match. To make that possible:

- **They can find the project.** The README, the About page and the `User-Agent` of every
  question-time fetch name the project and link its repository, where issues are open. The
  Wargaming application's registration carries the same link and a contact address.
- **The likely changes are settings, not rewrites:**

  | If they ask for | Change | Where it lives |
  |---|---|---|
  | A limit on how long history is kept | a retention window on snapshots, beside the 90-day pruning of raw bodies | `sync.retention` in `config.yaml`, with the default changed by an update |
  | Less data from `account/info` | a shorter `fields` list | `internal/wg` |
  | A different application setup, or a new id | a new built-in id, or players' own ids through the override | the release pipeline's secret; `auth wg --application-id` |
  | No fetches of tomato.gg's MoE page | `meta.moe_fetch: false` (§11) | `config.yaml`, with the default changed by an update |

- **The update path carries the change.** Self-update (§9) reaches every player, so a change
  Wargaming or tomato.gg asks for ships like any fix.

## 13. Changes to `docs/spec.md`

These changes were made to `docs/spec.md` on 2026-09-27:

- **§1.** "A personal, single-account tool" becomes "a single-account tool: one Wargaming account
  per Windows user, set up by the app", and the fixed account line moves to the owner's
  `config.yaml`.
- **§3.1.** `account/info` requests named `fields` only (§12.3).
- **§3.6.** "Run `wotctx mod install` again after every game update" becomes "the app does this
  (spec-desktop §6.2); the command remains".
- **§4.** Raw bodies older than 90 days are pruned (§12.3). `config.yaml` gains
  `sync.retention` (unset: history is kept) and `meta.moe_fetch` (default `true`), the two
  switches of §12.4. The app keeps its own records there too: `consent`, `setup.completed`, and
  `mod.managed` with `mod.placed`, the folders the uninstaller cleans (§4, §5.1). `wotctx`
  reads none of them.
- **§5.** The overlay gains `profile` and `advice` (§8.3).
- **§7.** New commands: `wotctx guide`, `wotctx auth wg --logout`, and `wotctx data delete` for
  the app's *Delete my data*.
- **§8.** The skill is generic, and the player's advice is rendered from the overlay (§8.5).
- **§10.** The Desktop bundle carries a launcher, not a binary (§7.1).
- **§11.** Secrets: the built-in application id is injected at build time and stays out of git
  (§12.1). Privacy: the guarantee becomes "account data leaves this machine only to Wargaming,
  and to Claude when the player asks it", stated as in §12.3. Network: GitHub for updates (§9).
  Dependencies: Wails, in `cmd/tankadvisor` only. Build: the release pipeline (§9.1).

## 14. Decisions (settled 2026-09-26)

| # | Decision | Settled as |
|---|---|---|
| D1 | Wargaming application id | One project-owned standalone id, registered on a dedicated account, injected at build time. The override stays (§12.1) |
| D2 | Claude Desktop hookup | `.mcpb` with the launcher (§7.1); editing `claude_desktop_config.json` is the fallback |
| D3 | GUI toolkit | Wails **v3** (Go + WebView2), pinned to one beta, after plan step D1: v2 has no tray, and v3 brings tray, autostart, notifications, single instance and the updater (§9) |
| D4 | Installer | NSIS, per user, from Wails' template (`WAILS_INSTALL_SCOPE=user`; D1 built one that runs `asInvoker`, with no UAC prompt) |
| D5 | Code signing | SignPath Foundation; unsigned with documented steps if refused. SignPath asks for proof of other users (2026-09-27), so the first releases are unsigned, and the project applies again once it has players |
| D6 | Release pipeline | GitHub Actions on Windows; the mod built by the maintainer until CI has Python 2.7 |
| D7 | Name | **Tank Advisor** (working name). Changed from "WoT Advisor" after reading the terms: no Wargaming trade marks. "For World of Tanks" appears only as a description, beside the non-affiliation notice |
| D8 | Where it is listed | GitHub Releases for the app. The mod also on the official Mod Hub after the first release, because Wargaming directs players there (§6.3) |
| D9 | Customisable advice | Every player sets their advice from the app. Core grounding rules stay fixed (§8) |

## 15. Deferred, on purpose

- **Several accounts or realms per Windows user.** The database has no account key. Adding
  one is a migration touching every table, and nothing about installation needs it.
- **A chat inside the app.** Claude Desktop is the chat, and the app is plumbing and settings.
- **Sharing advice settings between players** (import and export of the `advice` block). It is
  easy later, and nobody has asked for it yet.
- **Localising the app.**
- **The per-battle log** (`docs/spec.md` §3.6, "Not yet"). It is independent of distribution.

## 16. Done means

1. On a Windows user account that has never seen this project, with World of Tanks and Claude
   Desktop installed and no Go, make, Python or Node: download the installer, run it, finish
   the wizard, ask Claude *"what tank should I get now?"*, and get a grounded answer quoting
   data age. All of it **in under ten minutes, with no terminal**.
2. A change on the Advice page (a weight, the answer format, a new rule) shows in the preview at
   once and in Claude's next answer in a new conversation, and it cannot switch off a core rule.
3. A simulated game update (a new `version.xml` and an empty `mods\<new>\`) gets the mod
   installed without the player doing anything, and the next dump arrives.
4. An app update, released while Claude Desktop is running, installs silently. After a Claude
   Desktop restart, Claude uses the new `wotctx` with no extension reinstall.
5. A lapsed token produces a tray notification, and one click restores syncing. Log out
   revokes the token.
6. The evaluation of §8.6 passes: 10/10 on the owner's account with the owner's settings, the
   settings pairs behave, and another real account passes.
7. The About page, installer and README carry every notice of §12.2. The application id is not
   in git. The README states the readings of §12.1 and §12.3 and how to reach the project.
8. The uninstaller leaves no mod in the game folder, no autostart and no `PATH` entry. It keeps
   the database unless asked.
