> **Historical.** This is the brief the project was planned from (2026-09-17), kept as a
> record. It is superseded by `docs/spec.md` (the contract) and `docs/plan.md` (the schedule):
> in particular, Tomato.gg was dropped when its API turned out to need a paid key (spec §3.2),
> and WN8, recent form and the client mod replaced what it was meant to supply.

# Planning brief: `wotctx` — World of Tanks account context for Claude

Use in a fresh Claude Code session inside an empty repo, in plan mode.
Do not write code or files until I approve the plan.

## 1. Goal

Build a personal tool that gives Claude grounded, current context about my World of Tanks PC account, so I can ask:

- "What tank should I get now, based on my stats and garage?"
- "Which of my Tier X tanks should I push for marks?"
- "Is my light-tank play improving since update 2.4?"

…and get answers that use my real data, say how fresh it is, and flag small sample sizes.

Account: WoT PC, EU server · the owner's account (nickname and account_id removed before publishing)

## 2. Decisions already made (challenge only with a concrete reason)

1. **Three layers, kept separate**
   - *Reasoning* — a hand-written Claude skill: how to answer garage, purchase, research, grind, MoE and loadout questions. Stable, versioned. I will paste my existing WoT analysis framework into `skills/wot-advisor/references/`.
   - *Data* — fetched and cached by a Go CLI, exposed on demand. Never pasted wholesale into context.
   - *Overlay* — a small hand-edited YAML file for state no API exposes.
2. **CLI-first.** In Claude Code the skill calls `wotctx … --json` via Bash. An MCP server (`wotctx mcp`, stdio) comes later and reuses the same query layer.
3. **Go, single binary**, cross-compiled for Windows and macOS/Linux. SQLite via a pure-Go driver (no cgo). Official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`) when MCP is added.
4. **Personal tool.** No hosting, no multi-user, no web UI.

## 3. Data sources

**A. Wargaming Public API** — https://developers.wargaming.net (EU base `https://api.worldoftanks.eu/wot/`)
- `account/info` — lifetime stats per mode via `extra` (e.g. `statistics.random`, `statistics.epic`); with an access token: credits, gold, bonds, free XP, premium expiry, garage vehicle list (`private.garage`), Personal Reserves.
- `tanks/stats` (`extra=random`), `tanks/achievements`, `account/achievements`.
- `encyclopedia/vehicles` — names, tiers, classes, nations, premium flag; tech-tree edges if exposed.
- Auth: OpenID `auth/login` → access_token (valid ~2 weeks) → `auth/prolongate`. App type: Standalone (~10 req/s per IP).
- Data is cumulative only. ETag conditional requests are supported.

**B. Tomato.gg API** — https://tomato.gg/api (base `https://api.tomato.gg`, header `x-api-key`, 60 req/min per key)
- `recents`, `sessions` (`showTanks=true`), `tank-recents`, `moe-progression`, `map-stats`, `overall`.
- Mod-captured (requires the Tomato.gg mod in my client): `equipment` (equipment, consumables, shells, field mods, directives, crew), `battles` / `combined-battles`.
- Tomato.gg already keeps history, so it is the source for recent form. Local snapshots are for WG private fields (credits, free XP over time) and offline use.

**C. Overlay** — `wot-overlay.yaml`, versioned, hand-edited. Seed:

```yaml
researched_not_bought: [Type 71, WZ-113G FT, Kranvagn, T110E4, Obj 277]  # verify before first use
xp_goals:
  - target: Executor (Tier XI)
    via: Concept No. 5
    xp_required: 325000
    xp_banked: null        # manual; no API exposes vehicle XP
preferences:
  avoid_classes: [SPG]     # Obj 261 and M40/M43 kept only for missions
  strengths: [medium tanks, autoloaders]
constraints:
  playtime: limited        # recommendations must be time-efficient
notes: []
```

**D. Meta data** — server-wide tank performance, economics, MoE/mastery requirements (Tomato.gg site pages, Tanks.gg). Not in the documented Tomato.gg API, so not synced in v1. The skill fetches these at question time and labels their freshness.

## 4. Verify before planning

Check current docs and real responses and cite what you found. Don't assume.

1. WG `encyclopedia/vehicles`: are tech-tree edges exposed (`next_tanks`, `prices_xp` or similar)? Are Tier XI vehicles covered? If not, propose how the overlay covers research paths.
2. WG `tanks/stats` and `account/info`: available `extra` sections, exact private fields, per-tank assist fields.
3. Tomato.gg: which endpoints my key can access, response schemas, private-profile behaviour, terms for automated personal use, and whether the meta pages can be fetched as text at question time.
4. Claude Code: current plugin layout (skills, commands, hooks, `.mcp.json`), SessionStart hook output limits, skill-authoring guidance (trigger descriptions, progressive disclosure).
5. Claude apps: can one plugin with a local MCP server be installed in Claude Desktop, or should the server ship as an `.mcpb` desktop extension?

## 5. v1 requirements

**CLI**
- `wotctx auth` — WG OpenID login; token in OS keychain; auto-prolong. Tomato.gg key also in keychain.
- `wotctx sync` — pull A + B into SQLite; keep raw JSON per snapshot; idempotent; respect rate limits (prefer aggregate endpoints, cache, ETag); `--only wg|tomato`.
- `wotctx brief` — compact Markdown, hard limit 10,000 characters: resources, garage by tier/class, strongest/weakest tanks (lifetime vs last 30/60 days), overlay goals, data timestamps, known gaps.
- `wotctx query …` — JSON output: `garage`, `tank <name|id>`, `performance --by class|tier --window 30d`, `resources`, `candidates` (researched-not-bought + next research steps + affordability).
- `wotctx doctor` — auth status, token expiry, key validity, data age, whether mod data is available.

**Derived metrics**
- Per class and tier: battles, WR, DPG, assist, survival, WN8/WNX where available — lifetime vs recent.
- Confidence flag per tank based on battle count (configurable thresholds).
- No verdict rests on a single metric.

**Skill `wot-advisor`**
- Trigger description covers garage, purchase, research, grind, MoE, equipment and crew questions.
- Procedure: check data age (suggest `wotctx sync` if stale) → read brief → query specifics → build candidates → score fit (my performance by class and playstyle, overlay preferences and goals, credits/XP/bonds, patch and discount timing) → fetch meta at question time → answer.
- Output: Short answer / Why / Best setup / Watch-outs / Verdict. Always state data age and sample-size caveats. Keep my stats, server-wide performance and opinion clearly separated.

**Non-functional**
- Secrets never in the brief, logs, fixtures or git. WG token and private WG data never sent to any third party, including Tomato.gg.
- Tests use recorded API fixtures; no live calls in CI.
- Minimal dependencies; `make` or `just` targets for build, test, release.

## 6. Phasing

1. **MVP** — auth, sync, overlay, brief, query, skill used from a Claude Code project. Done when "What tank should I get now?" gets a grounded, data-cited answer.
2. **MCP** — `wotctx mcp` (stdio) exposing the query layer as tools; tested in Claude Desktop.
3. **Packaging** — plugin (skill + MCP + `/wot:sync` command; optional SessionStart hook that injects the cached brief without running a full sync), `.mcpb` bundle if Desktop needs it, and brief export for manual upload to my claude.ai Project (mobile).

## 7. Evaluation set

Draft 6–8 test questions with pass criteria. Starting points:

- "What tank should I get now?" → uses garage, resources and overlay; no SPGs; states data age.
- "Is my AMX 13 90 underperforming vs my mediums?" → lifetime vs recent, battle-count caveats.
- "Should I spend free XP to finish the Executor?" → checks free XP and the overlay XP bank; says what's unknown.
- "What's fitted on my TVP T 50/51?" → uses loadout data if the mod captured it, otherwise says it's missing.
- "How did my last week go?" → uses sessions, not lifetime numbers.
- "Which Tier X should I push for marks?" → MoE progression plus recent form.

## 8. What I want from you now

1. Your understanding and up to 5 open questions.
2. Findings from section 4, with sources.
3. Architecture: package layout, SQLite schema sketch, CLI surface, skill outline, phase-2 MCP tool list.
4. Phase 1 as small, testable steps with acceptance criteria, including the manual setup I must do (WG app registration, Tomato.gg key, mod install).
5. Risks and mitigations.

After I approve: first commit is `docs/spec.md` and `docs/plan.md`, then implement step by step.
