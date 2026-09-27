# WoT analysis framework: the defaults

> **Status: validated 2026-09-18 with the player it was built for; made generic 2026-09-26.**
> Section 9 records where each rule came from. These are the **defaults**: they apply
> wherever **Your advice** (the player's settings) says nothing, and the player's settings win
> wherever they speak. The **core rules** come first and are never overridden. Change a rule
> here deliberately, in git, when practice shows it wrong, never ad hoc in an answer.

This file says how to weigh evidence and decide. It holds **no account numbers and no player
values**. Numbers come from `wotctx` at question time. Every value a rule depends on (weights,
session length, battles per hour, credit buffer, free-XP limit, answer shape) is a setting,
shown in *Your advice* with its default when the player has not set it.

---

## 1. Who the advice is for

Read the player from *Your advice* and the data, not from assumptions:

- **Experience.** With `auto`, decide from the brief: under 1,000 random battles, a new player;
  explain terms the first time they matter. From 5,000 battles, experienced; skip basics unless
  asked. In between, explain a term only when the answer turns on it. Account WN8 is context,
  never a label for the player.
- **Time is costed in the player's sessions.** Every recommendation states its time cost in
  sessions of the length *Your advice* gives, converted at its battles per hour. "≈ 33 battles,
  about 4 sessions", not "≈ 33 battles".
- **Premium status** changes credit and XP earnings; take it from `query resources`, and
  reason with it. With it unknown, say which way the answer would change.
- **Class preference** from *Your advice* decides fit. Fit outranks meta: a strong tank of a
  disliked class ranks below a well-fitting one of a liked class. Say so in one line when the
  meta pulls the other way. Classes the player never wants recommended are never recommended,
  even when they are the cheapest or strongest option.
- **Stated strengths** are preferences to respect, not claims to trust (§3.4).
- **Improvement focus**, with coaching on: questions about those classes get a coaching angle
  (which ratio to work on, core §3.3), tracked over time once windows exist, and progress or
  regression is noted in one line, unasked, when the data allows.

## 2. Evidence

The core rules (§2 there) govern evidence, sample size and recency. Nothing here relaxes them.

## 3. Reading the numbers

The core rules (§3) say which metric answers what, and how to compare like with like.

### 3.4 Preferences versus evidence

When stated strengths disagree with the data, say so plainly and let the player decide, but
only after comparing like with like (core §3.2) and on more than one metric (core §2.4).

The worked case, from the account this was built on, tier VIII+, both classes at `ok`
confidence: light tanks and mediums were level on WN8 (about 1,920 against 1,980), but win rate
was 7 points lower in light tanks and survival 9 points lower. An all-tier reading had first
called light tanks the weakest class; the tier-band reading then called them level. Both were
one-metric readings. The honest summary was that light-tank *output* matched the mediums while
light-tank *results* did not, which points at decisions (when to spot, when to relocate, when
to stay alive), not damage or aim. That is the shape of a good coaching direction.

## 4. Decisions

### 4.1 "What should I buy or research next?"

Build the list from `query candidates`, then weigh each option on the six factors in *Your
advice*, at the weights it gives: fit to the player, time to get it, credit cost and
affordability, earning potential once owned, strength in the current meta, and alignment with
the player's goals. A factor weighted "only a tie-breaker" decides only between options that are
otherwise equal; one "ignored" is not mentioned as a reason.

| Factor | Evidence |
|---|---|
| Fit | class preference (*Your advice*), own tier VIII+ class performance (core §3.2) |
| Time | `path_xp`, `xp_remaining`, free XP; at the player's recent XP per battle |
| Credits | `price_credit`, `credits_short`, `affordable` |
| Earning | tier and premium status; tier X usually loses credits |
| Meta | server-wide stats, fetched and dated |
| Goals | overlay `xp_goals`, `path_xp` |

Rules:

- **Goals are always named.** An option on a goal's path is named even if others score higher;
  the others are offered alongside, not instead.
- **Researched-not-bought beats research** when fit is similar: it costs credits only.
- **Time-to-tank**: XP still needed ÷ the player's recent average XP per battle on the parent
  tank (lifetime if there is no window yet) = battles; ÷ battles per hour = hours; in sessions.
- **Credit buffer**: `query candidates` already includes the buffer in `affordable` and
  `credits_short`, also under `--budget-credits`. Quote those, and never add the buffer again.
  If the player says to ignore it, subtract `credit_buffer` from `credits_short` and say so.

### 4.2 Free XP

Free XP has two good uses: getting past a grind the player does not want (up to the tier limit
in *Your advice*), and **modules**, so no new tank is played stock. `query candidates` applies
the limit: `free_xp_allowed` is false above it, and `free_xp_covers` is then false too, so the
data never suggests what the player's policy forbids. Above the limit, never suggest free XP to
research a tank, not to finish a step, not to close a small gap, not "if you want it faster".
If the player asks directly, give the arithmetic and say it is outside their policy.

Vehicle XP is not in any API. With the client mod's dump, `xp_banked` is the via tank's XP as
the game shows it (`xp_banked_source: client mod`); otherwise it is the overlay's hand-kept
figure, which goes stale the moment the tank is played. **Every time banked XP, or a remainder
or battle estimate derived from it, is quoted, say where it comes from**: "from the game client
on <date>" or "hand-entered on <date>; no API has vehicle XP". When battles were played after
the figure was taken, say it is behind.

### 4.3 "Is tank X underperforming?"

1. Tank line vs the player's class line (same window, both with confidence).
2. If worse beyond the core §2.2 threshold: compare the tank's DPG to its server-wide average.
3. Classify: **tank is weak** (server figures low too) / **fit problem** (tank fine, player
   worse than their own class norm) / **sample too small** / **no real difference**.
4. For a fit problem, look at the class profile (core §3.3): which ratio is off.

### 4.4 Marks of Excellence

MoE progress comes from the client mod's dump: `meta moe` and `query tank` carry the marks, the
progress percentage the garage shows and the moving average it is computed from, with the
dump's capture time. Without a dump, ask for current mark percentages when they matter.

- Thresholds come from `wotctx meta moe <tank>`, with the page's timestamp.
- With the dump, the gap is exact: the next threshold against `moving_avg_damage`. Without it,
  the player's combined damage is a **range** (`low`–`high`): quote the range, never a single
  figure, and never call it mark progress.
- A tank is a good mark target when the next mark's threshold is within **10 %** above the
  player's **recent** figure and confidence is at least `moderate`. The recent figure is
  `moving_avg_damage` when the dump has it; otherwise `recent_30d`, and lifetime only while
  history is too short, saying so. The next mark may be the first.
- Further than 10 %: say how far, and do not encourage it. A tank past the line is never the
  recommended target, not even as "borderline". If none qualifies, say so. Thresholds drift
  (`change_30d`); mention it when a target is moving away.
- Prefer **one** mark target at a time.

### 4.5 Loadouts, equipment, crew

The fitted equipment, shells, consumables, directives and crew skills come from the client mod's
dump (`query tank` → `client`). Without a dump, ask what is fitted before advising a change. An
empty crew is possible: a tank can stand in the garage unmanned.

- Tank-specific advice comes from fetched sources, dated. General principles by class may be
  used, labelled as opinion.
- Recommend at most **two changes** at a time, so the effect is attributable.

### 4.6 "How did my session / week go?"

- Use `query sessions`, never lifetime numbers. Sessions are sync intervals; say what span
  each covers.
- Under 30 battles in the window: report it, draw no conclusion.
- Compare with the player's own lifetime, not the server.

### 4.7 Credits and economy

- Tier X usually costs credits per battle even with premium; tier VIII premiums earn.
- When credits are the bottleneck, suggest the credit-earning tanks the player owns and plays
  well (own data), not generic lists.

### 4.8 Premium tanks, gold and bonds

Premium vehicles bought with gold, from the shop, or from the **bond shop** are ordinary
candidates, weighed by the same §4.1 factors, unless the player's rules say otherwise.

- The data knows the player's gold and bonds (`query resources`) and a vehicle's `price_gold`,
  but **not what is on sale**. Offers, bundles and bond-shop stock are in no API: fetch them at
  question time and quote the page date; never recommend an offer that was not seen live.
- A premium suggestion must not duplicate one in the garage.
- A premium earns credits and trains crew but grinds no line: say which of those the player
  wants it for.

### 4.9 Mission chains

- **Classic personal missions are visible**: `wotctx query missions` reports, per operation,
  missions done, done with honours and open, and each class's open missions with conditions.
  Only Campaign 1 is described by the API; statuses from later campaigns are counted as
  undescribed. Completion is not in mission order, so never call the first open mission "the
  next one": it may have been skipped on purpose.
- **Newer mission chains** (for example those for the Vz. 60S Dravec, the Fossa VM 68 or the
  Black Rock) are not in the API: their conditions come from the web at question time, dated,
  and the player's progress from the player.
- Match conditions to the player's **owned** tanks and own stats: a "deal X damage" condition
  goes to the tank whose recent DPG makes it likeliest, not to the meta pick. Class preferences
  still apply. Cost it in sessions, and say when a condition is mostly luck rather than grind.

### 4.10 Onslaught

- **No Onslaught statistics exist in the API.** Everything the data says is random battles,
  and must not be presented as Onslaught performance.
- Whether Onslaught battles count in the `all` block is **unverified**; do not use
  `all − random` as an Onslaught figure.
- Rules, rewards and season details come from the web at question time, dated. Advice on which
  owned tanks suit it is opinion informed by random-battle stats, and labelled so.

## 5. Answer format

With the **sections** format (*Your advice*), every recommendation-type answer has five parts,
in this order:

1. **Short answer**: one or two sentences. The decision, not the reasoning.
2. **Why**: evidence, with the three kinds (core §2.1) kept apart and sample sizes stated.
3. **Best setup**: how to play or fit it, if relevant; omit when not.
4. **Watch-outs**: caveats, gaps, stale data, what would change the answer.
5. **Verdict**: one line, including confidence: *confident / leaning / can't tell yet*.

With **plain**, the same content without the headings. Either way, factual questions ("how much
free XP do I have?") are answered directly, with data age.

Style: concise; numbers with thousands separators; metric units; no filler openings; unasked
additions only as *Your advice* allows.

## 6. Never

The core rules' "Never" list is binding. In addition, by default:

- Never recommend a class the player never wants recommended.
- Never suggest free XP above the player's tier limit (§4.2).

## 7. Worked example (shape, not current numbers)

> **Q: What should I get next?**
>
> **Short answer:** Keep playing the tier VIII light tank on your goal line: 19,195 XP remain
> for the tier IX, about N battles (M sessions) at your recent XP rate. Keep your free XP for
> the tier IX's modules, so you never play it stock.
>
> **Why:** *My stats:* the step is 90 % done; this is your best light tank (2,507 WN8, 54
> battles: early signs only), and at tier VIII+ your light tanks match your mediums…
> *Server-wide (tomato.gg, updated …):* … *Opinion:* …
>
> **Best setup:** …
>
> **Watch-outs:** banked XP is from the game client at 18:40 and behind if you have played
> since; credits: the tier IX is 3.59M plus your buffer; you have 1.38M, so credits, not XP,
> will hold you up.
>
> **Verdict:** confident on the order; leaning on the line itself.
>
> *Data: wg:account/info 1 h old, XVM expected values 2026-09-12, game client 2 h old.*

## 8. Maintenance

- This file changes rarely and deliberately, in git. Values belong in the player's settings,
  the config or the data, not here.
- When a rule turns out wrong in practice, change it here, not ad hoc in an answer.

## 9. Where the defaults came from

The defaults were validated one by one, on 2026-09-18, with the player the tool was built for,
and became defaults for everyone on 2026-09-26. Their personal parts moved into that player's
settings.

1. Significance thresholds (core §2.2): WN8 ±150, WR ±2 pp, DPG ±10 %.
2. Session length: one hour, converted at ~8 battles per hour (estimate); now the defaults of
   `profile.session_minutes` and `profile.battles_per_hour`.
3. Credit buffer: that player's 500,000 is now their setting; the default is none.
4. Free-XP policy: that player's tier VIII limit is now their setting; the default is no limit.
5. Tier bands (core §3.2): compare classes at tier VIII+, never all tiers.
6. Mark targets (§4.4): within 10 % of the next mark, `moderate`+ confidence, one at a time.
7. Fit over meta (§4.1): now the default weights.
8. Premium tanks as ordinary candidates, bond shop included (§4.8).
9. Mission chains (§4.9) and Onslaught (§4.10) in scope.
10. Four smaller rules: patches reset evidence (core §2.3), at most two loadout changes at a
    time (§4.5), credit-earners from owned tanks (§4.7), answer style (§5).
