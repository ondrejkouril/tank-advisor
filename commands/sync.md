---
description: Sync the World of Tanks account data now and say what changed
argument-hint: "[--only wg|wn8] [--full]"
allowed-tools: Bash(wotctx sync*)
---

`wotctx sync $ARGUMENTS` just ran. Its output:

!`wotctx sync $ARGUMENTS`

Report it in at most four lines: which sources were fetched, which were still fresh, and every
`caveat:` line verbatim. If it failed naming `wotctx auth wg`, tell the player to run that
command in a terminal (it opens a browser) and stop. If another sync was already running, say
so; its data will be in the cache when it finishes. Run nothing else, and give no advice.
