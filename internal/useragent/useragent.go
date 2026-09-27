// Package useragent names wotctx in every request it makes, with a link back to
// the project, so that Wargaming, XVM or tomato.gg can see who is fetching and
// reach the project if they want to (docs/spec-desktop.md section 12.4).
package useragent

// String is the User-Agent header value. It keeps the "Mozilla/5.0
// (compatible; …)" shape crawlers use: tomato.gg serves a browser-shaped page
// and answers a bare agent with a bot challenge.
const String = "Mozilla/5.0 (compatible; wotctx; +https://github.com/ondrejkouril/tank-advisor)"
