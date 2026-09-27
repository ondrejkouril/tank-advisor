# Meta sources: fetched at question time, never synced

Server-wide performance, MoE thresholds, patch changes, shop offers and mission conditions
have **no API endpoint** `wotctx` can sync. Fetch them with WebFetch when a question needs
them, and **quote the page's own date** in the answer. No date, no figure.

Checked 2026-09-18 with WebFetch (what the skill actually uses):

| Need | Source | Works? | How to use it |
|---|---|---|---|
| Patch notes, what changed in an update | <https://worldoftanks.eu/en/news/updates/> | **yes** | Lists updates and micropatches with dates; follow an article link for details. Current major update then: 2.4 "Overdrive" |
| General news, events, missions, Onslaught seasons | <https://worldoftanks.eu/en/news/> | **yes** | Dated articles; search the listing for the event or mission name |
| MoE thresholds (65 / 85 / 95 / 100 %) | `wotctx meta moe <tank>` (reads <https://tomato.gg/moe/eu>) | **yes, via wotctx** | WebFetch sees only the first of 16 pages, so do **not** WebFetch it: `wotctx meta moe` fetches the page at question time, extracts the tank's row from its embedded data (794 tanks, tiers V–XI) and returns it with the page's own timestamp as the source age. It fails loudly if the page format changes |
| Tank characteristics | tanks.gg | **no** | Returns an empty app shell to WebFetch. Do not rely on it |
| Premium shop offers | <https://eu.wargaming.net/shop/wot/> | **no** (app shell) | Offers cannot be read; ask the player what is on offer, or use news articles announcing sales |
| Bond shop stock | in the game client only | **no** | Ask the player |
| WN8 expected values | synced by `wotctx` (`xvm:wn8exp`) | n/a | Never fetch; use `wotctx` |

## Rules

- Quote the source and its date: "tomato.gg MoE table, updated 18 Sep 2026".
- Server-wide figures are **server-wide**: label them apart from the player's stats and from
  opinion (framework §2.1).
- A page that fails, is an app shell, or lacks the row needed is reported as such. Never fill
  the gap from general knowledge presented as data.
- When an update is newer than the player's recent stats, check its notes for changes to the
  tanks in question before comparing old and new performance (framework §2.3).
