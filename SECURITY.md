# Security

Tank Advisor logs in to Wargaming for its players, keeps their account data on their
computers, and updates itself. A weakness in any of that should reach the maintainer before
it reaches anyone else.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through GitHub:
**[Report a vulnerability](https://github.com/ondrejkouril/tank-advisor/security/advisories/new)**
(the *Security* tab → *Report a vulnerability*).

Say what is affected, how to reproduce it, and the version (*Tank Advisor → About*, or
`wotctx version`). You will get an answer within a week, and credit in the release notes of
the fix, unless you prefer not to be named.

## What is in scope

- The Tank Advisor app and its installer, `wotctx`, the Claude Desktop extension's launcher,
  and the client mod.
- Self-update: every update must carry a valid Ed25519 signature from the release key
  (`cmd/tankadvisor/release.pub`); a way around that check is the most serious kind of report.
- Anything that sends account data anywhere other than Wargaming, or a Wargaming login token
  anywhere at all.

## Supported versions

Only the latest release. Tank Advisor updates itself, so a fix reaches players through an
update.
