# `wot-advisor` evaluation

Plan step 12. Each question is asked in a **fresh, headless Claude Code session in an unrelated
directory**, with no hint that a skill exists:

```
cd <empty temp dir>
claude -p "<question>" --output-format stream-json --verbose \
  --allowedTools "Bash(wotctx *)" WebFetch Read Skill
```

The transcript shows which commands ran; the final message is the answer. A question passes
only if **every** criterion holds. Criteria are written against the account as it stood on
2026-09-18 and the validated framework (`skills/wot-advisor/references/framework.md`).

*Acceptance (plan):* at least 6 of the original 8 pass without hand-holding, and every failure
has a filed follow-up. Questions 9 and 10 were added after the plan; they are reported but do
not change the threshold.

## Questions and pass criteria

Common to all: the skill is invoked; `wotctx doctor --json` runs first; data age is stated;
no raw JSON is pasted; no SPG is recommended; nothing the data cannot know is stated as fact.

| # | Question | Pass criteria |
|---|---|---|
| 1 | What tank should I get now? | Runs `brief` and `query candidates`. Five-part format (Short answer / Why / Best setup / Watch-outs / Verdict). Names ≥ 2 candidates with XP and credit cost and affordability **including the 500k buffer**. Mentions the Šelma → Tesák goal. Does **not** suggest free XP for a tier IX+ tank. States ≥ 1 sample-size caveat |
| 2 | Is my AMX 13 90 underperforming vs my mediums? | Uses `query tank` and `query performance --by class` **with `--min-tier 8`** (or otherwise compares tier VIII+ mediums, not all-tier). Uses ≥ 2 metrics. States battle counts / confidence for both sides. Makes no trend claim (no 30-day history yet) |
| 3 | Should I spend free XP to finish the Šelma? | Reads free XP (41,294) and the 19,195 XP still needed. Says **no**, because the player's policy spends free XP on research only up to tier VIII. Says vehicle XP is hand-maintained in the overlay (dated), not from any API |
| 4 | What's fitted on my TVP T 50/51? | Says loadouts are not in any data source (client mod planned). Does not present a loadout as the player's. Asks the player, or offers general advice labelled as opinion |
| 5 | How did my last week go? | Uses `query sessions`, not lifetime figures. States the span actually covered (history began 2026-09-17) and the battle count with its confidence. Draws no conclusion from a very-low sample |
| 6 | Which Tier X should I push for marks? | Uses `wotctx meta moe` for the tanks it discusses and quotes the page's timestamp. Reports the player's combined damage as a **range**, never as mark progress. Asks for current mark percentages. Recommends at most one target, respecting the `moderate`+ confidence rule |
| 7 | Is my light-tank play improving since 2.4? | Refuses a trend claim: no snapshot predates update 2.4 (history since 2026-09-17), and says so. May describe current tier VIII+ light-tank play, labelled as lifetime |
| 8 | How fresh is my data? | Relays `doctor` accurately: per-source ages, the XVM expected-values version, token expiry (2026-10-01), overlay status, and that client-mod data does not exist |
| 9 | Which personal missions do I have left? | Uses `query missions`. Reports T 55A (6 open: LT-15 and five heavy) and Object 260 not started. Says the Dravec / Fossa / Black Rock chains are not in the API. Does not call the first open mission "the next" one |
| 10 | How am I doing in Onslaught? | Says the API has no Onslaught statistics and does not present random-battle figures as Onslaught performance |

## Results

### Run 1 — 2026-09-18, skill as committed in `7abfec5`

Harness grants exactly `Bash(wotctx *)`, `WebFetch`, `Read`, `Skill`. Every session invoked the
skill unprompted. 10 sessions, 8–42 s each, $0.13–0.55 each.

| # | Result | Notes |
|---|---|---|
| 1 | **pass** | `brief` + `candidates`; five parts; Šelma 4.09M incl. buffer, 2.71M short; tier X options with shortfalls; kept free XP for modules; 54-battle caveat. 19,195 ÷ 887 XP ≈ 22 battles, correct |
| 2 | **pass** | `query tank` + `performance --min-tier 8`; counts both sides; also compared against tier VIII+ light tanks, the fairer baseline; no trend claim |
| 3 | **fail** | Advice correct (no free XP: tier IX, the player's policy; 41,294 and 19,195 right) but never said banked XP is hand-entered in the overlay rather than from any API — the query's caveat said so and was not relayed |
| 4 | **fail** | Content right (loadouts in no data source; asked for them), but its `wotctx doctor` went to **PowerShell**, was denied, and it gave up without retrying in Bash: no data checked, no data age |
| 5 | **pass** | `sessions`; real span ~14 h since 2026-09-17 21:22; 2 battles, very low; "can't tell yet" |
| 6 | **pass** | `meta moe` on six tier X; ranges, "not your mark progress"; asked for mark %; one target (Object 140, top of range within 10 % of the 85 % threshold, "leaning"); flagged the T110E5 as a two-metric fit problem. Minor: called the page's own stamp "fetched" |
| 7 | **pass** | Refused the trend (no snapshot before 2.4); lifetime tier VIII+ only, labelled |
| 8 | **pass** | Ages, XVM version, token expiry, overlay, missing mod data — all accurate |
| 9 | **fail** | Listed "HT-10: A Weighty Argument" as open: HT-10 is done and the name does not exist — the conditions belong to HT-12. It had grepped a pretty-printed listing and misaligned fields (its python fallback was denied). Also stated without a source that the SPG branch is not needed |
| 10 | **fail** | Same as 4: PowerShell denied, gave up; the substance (no Onslaught stats in the API) was right |

**Original 8: 6 pass — meets the plan's acceptance (≥ 6).** Added questions: 0 of 2.

Causes and follow-ups:

1. **Permissions** (4, 10; PowerShell tried first in 7 of 10 runs). The skill allowed only
   `Bash(wotctx *)`; on Windows the model reaches for PowerShell, and when denied sometimes
   stops. Compound commands (`wotctx …; cat …`, loops, pipes into python) are denied too, as
   they do not match the prefix. **Fix:** allow `PowerShell(wotctx *)`, `Read` and `Grep`; tell
   the skill to run one `wotctx` command per call, never to post-process output with other
   programs, and to retry in the other shell once before giving up.
2. **Banked-XP provenance not relayed** (3). **Fix:** the framework requires saying, every time
   banked XP is quoted, that it is hand-entered in the overlay on its date.
3. **Lossy post-processing** (9). The model had no narrow way to ask for open missions only.
   **Fix:** `query missions --operation X --open`, and a rule against reconstructing records
   with text tools; game-rule claims (like which branches an operation needs) need a source.

### Run 2 — 2026-09-18, skill as committed in `ec16b10`

All ten questions re-run in fresh sessions after the fixes, the six run-1 passes included, to
rule out regressions. The harness now grants exactly the skill's `allowed-tools` (`Bash(wotctx *)`,
`PowerShell(wotctx *)`, `Read`, `Grep`, `WebFetch`) plus `Skill`. No tool call was denied in
any session. 8–33 s and $0.07–0.34 each.

| # | Result | Notes |
|---|---|---|
| 1 | **pass** | Šelma first; 2,710,969 short incl. buffer; tier XI mediums (325,000 XP, 7.4M) and researched tier X listed as unaffordable; banked XP "typed into your overlay by hand … no API has vehicle XP"; offered to check the bond shop |
| 2 | **pass** | Tier VIII+ against both LT and MT baselines; used `meta moe` unprompted: lifetime range 1,865–1,918 vs 1,859 for the first mark, "doesn't show your actual mark progress" |
| 3 | **pass** | Fixed: "entered by hand in the overlay on 2026-09-18; no API reports vehicle XP"; `free_xp_allowed: false` cited; all figures correct |
| 4 | **pass** | Fixed: `doctor` ran; TVP stats (46 battles, low); loadouts in no data; linked the ask to the player's spotting-heavy assist (613 of 735) |
| 5 | **pass** | Real span ~14 h, 2 battles, very low, "can't tell yet"; ask again around 25 Sep |
| 6 | **pass** | Object 140, "leaning"; ranges not progress; one-target rule applied explicitly; worked out the fallback if it already has two marks. Minor, as in run 1: "fetched" for the page's own stamp |
| 7 | **pass** | Fetched the patch-notes page: 2.4 predates the 4 Sep micropatch, hence the first snapshot; refused the trend; first real 30-day read ≈ 17 Oct |
| 8 | **pass** | Accurate. One irrelevant line about a claude.ai connector needing authorisation — from the machine's session environment, not the skill |
| 9 | **pass** | Fixed: used `--operation "T 55A" --open`; the six open missions exactly match `wotctx` (HT-12 "Sturdy Armor" correctly named); no unsourced game-rule claims |
| 10 | **pass** | Fixed: `doctor` ran; no Onslaught stats in the API; refused to use `all − random` until verified, and proposed the check |

**Original 8: 8 pass. All 10 pass.** Plan step 12 acceptance met; the three run-1 causes are
fixed, not merely filed.

Observations, not failures:

- Answers are consistent across runs where the data did not change (#1, #2, #6 reached the same
  recommendations twice), which is the point of pinning judgement in the framework.
- Every session found the skill from the question alone, in a directory with nothing in it —
  which is also the plan step 11 acceptance run.
- Cost per question fell from up to $0.55 in run 1 to at most $0.34 in run 2, mostly because
  denied calls and their retries disappeared.

### Runs 3 and 4 — 2026-09-18, over MCP (plan step M4)

The same ten questions, asked through `wotctx mcp` instead of the CLI, with **no shell at all**.
Each session ran headless in an empty directory with only the wotctx server loaded
(`--strict-mcp-config`), so the model's only way to reach the data was the eleven tools. That
is Claude Desktop's situation, driven from the command line so it can be repeated and graded.

- **Run 3, no skill** (Desktop as installed): `--tools WebFetch --disable-slash-commands`. The
  server's `instructions` are all the guidance there is.
- **Run 4, with the skill** (Desktop with the skill uploaded): `--tools Skill Read Grep
  WebFetch`. The skill is unchanged: its text names CLI commands, and the model mapped each
  one to the matching tool without being told to.

```
# mcp.json: {"mcpServers":{"wotctx":{"command":"C:/Users/<you>/go/bin/wotctx.exe","args":["mcp"]}}}
claude -p "<question>" --output-format stream-json --verbose --strict-mcp-config --mcp-config mcp.json \
  --disable-slash-commands --tools WebFetch --allowedTools mcp__wotctx WebFetch          # run 3
claude -p "<question>" --output-format stream-json --verbose --strict-mcp-config --mcp-config mcp.json \
  --tools Skill Read Grep WebFetch --allowedTools mcp__wotctx Skill Read Grep WebFetch   # run 4
```

(On Windows, `--mcp-config` needs a Windows path, and the JSON is safest with forward slashes.)

No tool call was denied or failed in any of the 20 sessions. Run 3 took 6–22 s and
$0.02–0.11 per question; run 4 took 9–32 s and $0.11–0.35.

| # | Run 3 (no skill) | Run 4 (skill) |
|---|---|---|
| 1 | **fail**: right substance (Šelma, 2.71M short incl. buffer, goal line, free-XP policy, overlay-dated XP, 54-battle caveat), but not the five-part format, which only the skill defines | **pass**: five parts, ~22 Blesk battles, banked XP "hand-entered in the overlay on 2026-09-18", 2,710,969 short |
| 2 | **fail**: never used `min_tier 8`; compared against all-tier mediums using a win rate it worked out itself (~54.7 %) | **pass**: `performance --min-tier 8` for both classes; same verdict as runs 1–2 (a light-tank pattern, not this tank) |
| 3 | **pass**: no, by the tier VIII policy; 41,294 and 19,195; XP "from your own notes, updated today" | **pass** |
| 4 | **pass**: loadouts in no API; asked the player | **pass** |
| 5 | **pass**: real span 17 h, 2 battles, no conclusion. Called the Leox French (it is Italian): model knowledge stated as fact | **pass**. Ran `wot_sync` although nothing was stale; harmless, since sources inside their TTL are skipped |
| 6 | **fail**: two targets ("Object 140, with the Patton a close second"); the Patton has 63 battles (`low`) | **pass**: Object 140, "leaning", T110E5 only as a fallback; ranges, asked for mark %. Same minor issue as before: "fetched" for the page's stamp |
| 7 | **pass**: refused the trend. Offered to look the account up on tomato.gg, which the tool never does | **pass**: fetched the updates page, refused the trend, gave the thresholds for a future claim |
| 8 | **pass** | **pass** |
| 9 | **pass**: `open_only` used; the six T 55A missions exact | **pass** |
| 10 | **pass**: no Onslaught stats in the API | **pass**: also proposed the `all` vs `random` check |

**Run 3: 7 of 10. Run 4: 10 of 10.** Plan step M4 acceptance (≥ 8 with the skill) is met.

The three run-3 failures come from rules that live only in the skill's framework: the answer
format, tier bands for class comparisons, and one mark target at `moderate` confidence or
better. The server's instructions carry the procedure but not the judgement. That is the input
to phase 3: a Desktop user without the skill gets correct data and weaker advice, so the
`.mcpb` bundle should either ship the framework through MCP (as a prompt or resource) or tell
the user to upload the skill.

### Run 5 — 2026-09-25, over MCP with `wot_guide`, no skill (plan step P2)

Run 3's setup exactly (MCP only, no shell, no skill, `--disable-slash-commands --tools
WebFetch`), against a build with `wot_guide`: the skill's procedure and framework, embedded in
the binary, and a line in the server's instructions to read it before the first judgement.
The sessions ran in parallel, each with its own `wotctx mcp`, which also exercised the new
sync lock.

**Harness pitfall, cost one run:** `claude -p` reads stdin and appends it to the prompt. In a
`while read q; do claude -p "$q" … & done < questions.txt` loop, the backgrounded sessions
swallowed the remaining questions, so one session answered all ten. Every session now gets
`< /dev/null`. The spoiled run was discarded, not graded.

**The account has moved on since the criteria were written** (2026-09-18): the Šelma is
bought, the Blesk is sold, the Kranvagn is owned, and free XP is 251,507. The overlay was not
updated, so it still lists the Šelma goal and the Kranvagn as researched. Answers were graded
against the data as it stood, and on whether they noticed.

`wot_guide` was called unprompted in 7 of 10 sessions: every recommendation question, and none
of the three factual ones (4, 8, 9), which the instructions do not require it for. No tool call
was denied. 7–43 s and $0.03–0.22 per question.

| # | Result | Notes |
|---|---|---|
| 1 | **pass** | Five parts. Tesák 4,709,135 short incl. the buffer; Object 277 / T110E4 and Centurion 7/1 with XP, credits and shortfall; free XP for Šelma modules only, not tier X research; 12-battle caveat; spotted the stale overlay and offered to fix it |
| 2 | **pass** | `performance --min-tier 8`; WN8 level with mediums, win rate 5.8 points lower, same as the LT baseline (and noted the 13 90 is 530 of its 862 battles); `meta moe` for context; no trend claim |
| 3 | **pass** | The premise is out of date and it said so: the Šelma is owned. Free XP 251,507; modules yes, Tesák research no (policy); the overlay's 19,195 "hand-entered … no API has vehicle XP" |
| 4 | **fail** | No loadout data, asked, 46-battle caveat — but called the TVP's Ace Tanker badge (`mastery` 4) "1st Class": the query gives the number only, and the scale is in `references/metrics.md`, which a factual answer never reads. Found while grading run 6, which had it right |
| 5 | **pass** | `sessions`: 35 battles (low), real span 18–25 Sep, interval by interval; "leaning", not a verdict |
| 6 | **pass** | Object 140, "leaning", T110E5 only as a backup with two metrics against it; ranges, asked for mark %. Minor, as in every run: "fetched" for the page's stamp |
| 7 | **pass** | Refused the trend (history begins after 2.4); 19 battles "too few"; noticed 12 of them are one new tank |
| 8 | **fail** | Ages, XVM version, token expiry and overlay warnings right, but said the overlay holds "credits, gold, free XP" and told the player to "update your resource numbers" in it — wrong, and harmful if followed |
| 9 | **pass** | T 55A's six open missions exact; Object 260 not started; the chains not in the API; no "next" |
| 10 | **pass** | No Onslaught stats; refused `all − random` (Object 140: 270 vs 262) as an Onslaught figure; proposed the check |

**Run 5: 8 of 10** (first graded 9: the Q4 misreading was found later, while grading run 6),
against run 3's 7. The three run-3 failures (format, tier bands, one mark target) all pass: the
judgement now reaches a client that has no skill. Both failures share one cause, below.

Both failures are factual questions answered without the guide, filling a gap with
invention. **Fixes, where every session sees them:** the server's instructions now say what the
overlay holds and that resources never come from it; `wot_garage` and `wot_tank` describe the
mastery scale. Asked twice more after the fix: both described the overlay
correctly. One (8b) passes in full. The other (8a) omitted the gaps no source covers
(vehicle XP, marks, loadouts), so it misses one criterion by the letter.

### Run 5b — 2026-09-25, the same, after the two fixes

All ten again, same setup as run 5. 8–44 s and $0.07–0.37 per question; no call denied.
`wot_guide` read unprompted in 7 of 10, as before (not for 4, 8, 9).

| # | Result | Notes |
|---|---|---|
| 1 | **pass** | Five parts; Tesák 4,709,135 short incl. buffer; Centurion 7/1 and tier XI with costs; free XP for Šelma modules only; "banked XP … no API has vehicle XP"; flagged the stale overlay |
| 2 | **pass** | Tier VIII+ baselines, split out the other light tanks (332 battles) to avoid comparing the 13 90 with itself; ranges for the mark, not progress |
| 3 | **pass** | Šelma already owned; free XP for modules, not the Tesák (policy, and credits) |
| 4 | **pass** | Fixed: "Ace Tanker" |
| 5 | **pass** | Šelma "1st Class" (`mastery` 3, correct); per-class lines, each "descriptions, not verdicts" |
| 6 | **pass** | Object 140, "leaning"; T110E5 conditional on the player's current marks |
| 7 | **pass** | Could not read the updates page and said so; "since 2.4" cannot be measured |
| 8 | **pass** | Fixed: overlay described correctly; mod data (vehicle XP, marks, crews, loadouts) named as missing |
| 9 | **pass** | |
| 10 | **pass** | Checked `all` against `random` on the tanks played: equal, so no Onslaught signal either way |

**Run 5b: 10 of 10.** Plan step P2 acceptance (≥ 9, the run-3 failures fixed) is met.

### Run 6 — 2026-09-25, the installed plugin (plan step P3)

The `wot` plugin installed at user scope from the local marketplace, loaded in place from this
repository; the hand-installed skill and user-scope MCP registration removed. Each session had
exactly what the plugin gives Claude Code: `--allowedTools "Bash(wotctx *)" "PowerShell(wotctx *)"
Read Grep WebFetch Skill mcp__plugin_wot_wotctx`. A copy of the skill uploaded to claude.ai also
loads (`anthropic-skills:wot-advisor`); no session chose it. 7–62 s and $0.13–0.45 per question.

Nine sessions invoked `wot:wot-advisor` and then ran `wotctx` in a shell (mostly PowerShell)
rather than the plugin's MCP tools; the skill's text names commands. Q8 did not invoke the skill:
it called `wot_data_status` and answered — accurately — so the common "skill is invoked"
criterion, written when the skill was the only route to the data, is noted rather than failed.

| # | Result | Notes |
|---|---|---|
| 1 | **pass** | Five parts; Tesák 4,709,135 short; Object 277 / T110E4 and tier XI mediums with costs; free XP policy; asked for banked Šelma XP |
| 2 | **pass** | `--min-tier 8`; `meta moe` with a range, not progress; the 13 90 is 61 % of the LT line, said so |
| 3 | **pass** | Šelma owned; modules yes, Tesák no; overlay figure "entered by hand on 2026-09-18" |
| 4 | **pass** | No loadout data; Ace Tanker correct |
| 5 | **pass** | Sessions interval by interval; the 6½-day gap without a sync named, and "sync after each session" suggested |
| 6 | **pass** | Object 140, "leaning"; M48A5 left out at 63 battles |
| 7 | **pass** | Updates page returned a bot check, said so; no trend |
| 8 | **pass** | See above: accurate, via the MCP tool without the skill |
| 9 | **pass** | T 55A's six exact; Object 260; chains not in the API |
| 10 | **pass** | No Onslaught stats; proposed the check |

**Run 6: 10 of 10.** Plan step P3 acceptance (≥ 9) is met.

## Criteria for runs 7 and 8 — with the client mod (plan step C5)

With the mod's dump in the cache, three questions have figures the data did not have before, and
their criteria change. The rest stand as written above, graded against the account as it is.
The dump is from 2026-09-25 20:53 UTC (mod 0.2.1, so no research list yet).

| # | Pass criteria with the dump |
|---|---|
| 3 | Uses the Šelma's real XP from the game (90,237), not the overlay's, and says so with the dump's date: 152,323 XP remain to the Tesák. Free XP is for modules, not tier X research (policy) |
| 4 | Lists the TVP T 50/51's actual fit, with the dump's date: three pieces of equipment (improved rotation mechanism class 1, coated optics class 1, improved ventilation class 2), AP-CR ×28 / HEAT ×20 / HE ×0, small repair kit, small first-aid kit, automatic fire extinguisher, no directive, and a crew. Names in the player's language, not raw technical names. Recommends at most two changes, if any |
| 6 | Uses the real marks and percentages (Object 140: 1 mark, 75.15 %; T110E5: 1 mark, 74.27 %), with the dump's date. Measures the gap to the next mark against `moving_avg_damage` (Object 140 2,961 vs 3,391 for 85 %: about 15 %), so that by the 10 % rule no tank qualifies yet, and says how far rather than encouraging. At most one target. Does not ask for mark percentages it already has |

**Correction to criterion 6, found while grading run 7:** it was wrong. It checked only the
second marks of the tanks that already have one, and so concluded that no tank qualifies. The
STB-1 has no mark yet: its moving average of 2,295 is 1.5 % below the 2,330 needed for the
first, the one tier X that meets the 10 % rule. Run 7 found it, and so did every later run.
Question 6 is graded on the corrected reading: the STB-1 as the target, and the others' distance
given without encouragement.

### Run 7 — 2026-09-25, the installed plugin with the client mod dump (plan step C5)

The setup of run 6. The sessions ran `wotctx` from PowerShell through the skill, as in run 6.
$0.15–0.48 and 10–64 s per question; no call was denied.

| # | Result | Notes |
|---|---|---|
| 1 | **pass** | 90,237 XP on the Šelma "from the game client at 20:53 UTC", so 152,323 XP remain; confirmed the Šelma's fit and warned that the Centurion Mk. I has no crew and no ammunition |
| 2 | **pass** | Tier VIII+ baselines; used the real mark (1 mark, 73.84 %, moving average 2,267) beside the lifetime range |
| 3 | **pass** | Real XP with its date; free XP for modules only; and noted, rightly, that "not elite" does not prove modules are locked |
| 4 | **pass** | The exact fit (three pieces of equipment, 28/20/0, three consumables, no directive) and the crew, in English, with the dump's time; "no battles since, so it's current" |
| 5 | **pass** | 44 battles, interval by interval; the Tesák progress from the dump |
| 6 | **pass** | The STB-1 first (1.5 %); the Object 140 at 14.5 %, "outside the rule, a stretch"; asked for nothing it had |
| 7 | **pass** | No trend; 27 light-tank battles, too few |
| 8 | **pass** | Relayed `mod-data` (dump age, mod and game version) with the rest |
| 9 | **pass** | |
| 10 | **pass** | |

**Run 7: 10 of 10.**

### Run 8 — the same over MCP without the skill, and 8c after two fixes

Run 5b's setup, against the dump.

- **Run 8: 8 of 10.**
  - **Q3 failed.** It read `elite: false` on the Šelma as "some modules are still locked", and
    advised on that. Elite means every module *and every follow-on vehicle* is researched, so
    the Tesák alone keeps the Šelma from being elite.
  - **Q6 failed.** It found the STB-1 but made the Object 140, at 14.5 %, the main target, "a
    borderline case". The framework says: past 10 %, say how far, and don't encourage it.
- **The fixes.**
  - `metrics.md` and `wot_tank`'s description now say what `elite` does and does not mean.
  - The framework's mark rule now names `moving_avg_damage` as the recent figure. It counts a
    first mark, and says a tank past the line is never the recommended target.
  - Asked again (run 8b), twice over MCP and once as the plugin: all three Q3 answers read
    `elite` correctly, and all three Q6 answers pick the STB-1 alone.
- **Run 8c, all ten again: 10 of 10.** $0.04–0.31 and 7–40 s per question.
  - Q3 now says "the data can't tell locked modules apart from the Tesák not being
    researched".
  - Q6 recommends the STB-1 alone.
  - Q4 lists the TVP's full fit and crew and "all modules are researched". Correct for a tier X,
    which has no follow-on.

Plan step C5 acceptance (both runs ≥ 9, with 3, 4 and 6 on real data) is met by runs 7 and 8c.

### Runs 9, 9b and 10 — 2026-09-26, the generic framework with the owner's settings (plan step D3)

Isolated from the live setup: a copy of the owner's data (`WOTCTX_DATA_DIR`), the worktree's
plugin via `--plugin-dir` with the installed one and context-mode disabled, and the new binary
first on the PATH. Q3 asks about the Tesák now that the Šelma is owned.

- **Run 9 (plugin): 9 of 10.** Q1 said "the Šelma isn't fully researched yet, so some of its XP
  would go on its own modules": run 8's `elite` misreading. Fix: `query tank` now carries a
  caveat whenever `elite` is false (`NotEliteCaveat`). **Run 9b** re-asked Q1 twice and Q3
  once: all three hedge correctly and pass.
- **Run 10 (MCP, no skill): 10 of 10.**
- **Settings pairs, all pass.** `format: plain` dropped the headings but kept "Verdict:" labels;
  the rendered wording now forbids part labels, and 9b's plain Q6 has none. `meta: high` tried
  to assess the meta and said it could not, rather than use an undated figure. A player rule
  against gold premiums turned "gold or bonds" into "no gold premium; your own rule rules that out".
