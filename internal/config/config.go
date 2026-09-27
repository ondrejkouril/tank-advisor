// Package config resolves wotctx's filesystem paths and loads its settings.
//
// A missing config file is not an error: the defaults hold no account, and
// `wotctx auth wg` records the one that logs in (docs/spec-desktop.md section
// 8.1), so a fresh install reports "not set up" until then. A malformed or
// invalid file is an error, because silently falling back to defaults would
// hide a typo behind plausible-looking output.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the contents of config.yaml.
type Config struct {
	Account Account `yaml:"account"`

	// OverlayPath locates the overlay, the player's own settings file, for a
	// player who keeps it somewhere else, under version control of their own
	// say. Empty means <ConfigDir>/wot-overlay.yaml, where the app keeps it.
	OverlayPath string `yaml:"overlay_path"`

	// GameDir is the World of Tanks client folder, the one holding
	// version.xml and mods/. `wotctx mod install` copies the client mod into
	// it, and doctor reads the client's version from it. Empty means the
	// folder Game Center records for the account's realm (internal/game).
	GameDir string `yaml:"game_dir"`

	Sync       Sync       `yaml:"sync"`
	Meta       Meta       `yaml:"meta"`
	Confidence Confidence `yaml:"confidence"`

	// The Tank Advisor app's own records (docs/spec-desktop.md section 5).
	// wotctx reads none of them.
	Consent Consent `yaml:"consent"`
	Setup   Setup   `yaml:"setup"`
	Mod     Mod     `yaml:"mod"`
	Duties  Duties  `yaml:"duties"`
}

// Duties are the app's background duties (docs/spec-desktop.md section 5.6).
type Duties struct {
	// Paused lists the duties the player has switched off: sync, login, mod,
	// updates.
	Paused []string `yaml:"paused"`
}

// DutyNames are the background duties, in the order the app lists them.
var DutyNames = []string{"sync", "login", "mod", "updates"}

// IsPaused reports whether the player switched a duty off.
func (d Duties) IsPaused(name string) bool {
	for _, p := range d.Paused {
		if p == name {
			return true
		}
	}
	return false
}

// Consent records the player's agreement to the notice at the first step of
// the app's setup (docs/spec-desktop.md sections 5.1 and 12.3).
type Consent struct {
	// Notice is the version of the notice agreed to. A newer notice asks
	// again.
	Notice int `yaml:"notice"`
	// Agreed is the date, YYYY-MM-DD.
	Agreed string `yaml:"agreed"`
}

// Setup records the app's setup wizard.
type Setup struct {
	// Completed is the date the wizard was finished, YYYY-MM-DD, or "".
	Completed string `yaml:"completed"`
}

// Mod records how the app looks after the client mod (docs/spec-desktop.md
// section 6.2).
type Mod struct {
	// Managed is true once the player has had the app install the mod: from
	// then on the app installs it again after each game update.
	Managed bool `yaml:"managed"`
	// Placed lists every mods folder the app put the mod in, so the
	// uninstaller can take it out of each (section 4).
	Placed []string `yaml:"placed"`
}

// Account identifies whose data this install manages. AccountID 0 means no
// account is set up yet.
type Account struct {
	Realm     string `yaml:"realm"` // eu, com or asia
	AccountID int    `yaml:"account_id"`
	Nickname  string `yaml:"nickname"`
}

// Meta controls the question-time fetches of server-wide pages.
type Meta struct {
	// MoEFetch turns tomato.gg's Mark of Excellence page on or off. Nil means
	// on. It exists so the fetch can be switched off at once if tomato.gg
	// asks (docs/spec-desktop.md section 12.4).
	MoEFetch *bool `yaml:"moe_fetch"`
}

// MoEFetchEnabled reports whether `meta moe` may fetch tomato.gg's table.
func (c Config) MoEFetchEnabled() bool {
	return c.Meta.MoEFetch == nil || *c.Meta.MoEFetch
}

// Configured reports whether an account has been set up.
func (c Config) Configured() bool {
	return c.Account.AccountID > 0
}

// Sync controls how eagerly data is refetched.
type Sync struct {
	// AutoSyncAfter is the staleness threshold the skill uses to decide whether
	// to sync before answering. See docs/spec.md section 8.
	AutoSyncAfter Duration `yaml:"auto_sync_after"`

	// TTL is the minimum age, per source, before sync refetches it. Keys match
	// the `source:endpoint` names that appear in the query provenance envelope.
	TTL map[string]Duration `yaml:"ttl"`

	// Retention, when set, deletes snapshots older than this at each sync,
	// together with everything parsed from them. Zero keeps all history,
	// which recent form is built from (docs/spec-desktop.md section 12.4).
	Retention Duration `yaml:"retention"`
}

// RawBodyRetention is how long raw response bodies are kept after parsing.
// They exist only for re-parsing after a parser change (docs/spec-desktop.md
// section 12.3); the parsed rows stay.
const RawBodyRetention = 90 * 24 * time.Hour

// Confidence holds the battle-count thresholds behind the per-tank confidence
// flag (docs/spec.md section 6.3).
type Confidence struct {
	VeryLowBelow  int `yaml:"very_low_below"`
	LowBelow      int `yaml:"low_below"`
	ModerateBelow int `yaml:"moderate_below"`
}

// Confidence levels, ordered weakest to strongest.
const (
	ConfidenceVeryLow  = "very_low"
	ConfidenceLow      = "low"
	ConfidenceModerate = "moderate"
	ConfidenceOK       = "ok"
)

// Classify maps a battle count to a confidence level. Every per-tank and
// rollup row carries the result, so that no answer can quietly rest on a
// handful of battles.
func (c Confidence) Classify(battles int) string {
	switch {
	case battles < c.VeryLowBelow:
		return ConfidenceVeryLow
	case battles < c.LowBelow:
		return ConfidenceLow
	case battles < c.ModerateBelow:
		return ConfidenceModerate
	default:
		return ConfidenceOK
	}
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		// No account until one logs in; the realm is only the login's default.
		Account: Account{Realm: "eu"},
		Sync: Sync{
			AutoSyncAfter: Duration{6 * time.Hour},
			TTL: map[string]Duration{
				// Reference data changes only on patch day. Wargaming ignores
				// If-None-Match (measured; see docs/spec.md section 3.1), so
				// these TTLs are the only thing standing between an eager sync
				// and the request budget.
				"wg:encyclopedia/vehicles":         {7 * 24 * time.Hour},
				"wg:encyclopedia/achievements":     {7 * 24 * time.Hour},
				"wg:encyclopedia/personalmissions": {7 * 24 * time.Hour},
				"wg:account/info":                  {time.Hour},
				"wg:tanks/stats":                   {time.Hour},
				"wg:account/tanks":                 {time.Hour},
				// Achievements move slowly and are never the subject of a
				// "how am I doing lately" question.
				"wg:account/achievements": {12 * time.Hour},
				"wg:tanks/achievements":   {12 * time.Hour},
				// XVM regenerates nightly, but a rating is only comparable
				// within one vintage of expected values, so they are treated
				// as patch-day reference data rather than chased daily.
				"xvm:wn8exp": {7 * 24 * time.Hour},
			},
		},
		Confidence: Confidence{
			VeryLowBelow:  30,
			LowBelow:      100,
			ModerateBelow: 300,
		},
	}
}

// Load reads path on top of the defaults, so a config file may set only the
// values it wants to change. A missing file yields the defaults and no error.
func Load(path string) (Config, error) {
	cfg := Default()

	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return cfg, nil
	case err != nil:
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true) // a typo'd key is an error, not a silent default
	if err := dec.Decode(&cfg); err != nil {
		return Default(), fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Default(), fmt.Errorf("invalid %s: %w", path, err)
	}
	return cfg, nil
}

// Validate rejects a configuration that would produce misleading output.
func (c Config) Validate() error {
	switch c.Account.Realm {
	case "eu", "com", "asia":
	default:
		return fmt.Errorf("account.realm is %q, want eu, com or asia", c.Account.Realm)
	}
	if c.Account.AccountID < 0 {
		return fmt.Errorf("account.account_id must be positive, got %d", c.Account.AccountID)
	}
	if c.Sync.Retention.Duration < 0 {
		return fmt.Errorf("sync.retention must not be negative, got %s", c.Sync.Retention)
	}
	// Shorter than a week would leave no baseline for any window at all.
	if r := c.Sync.Retention.Duration; r > 0 && r < 7*24*time.Hour {
		return fmt.Errorf("sync.retention must be at least 7d, got %s", c.Sync.Retention)
	}
	if c.Sync.AutoSyncAfter.Duration <= 0 {
		return fmt.Errorf("sync.auto_sync_after must be positive, got %s", c.Sync.AutoSyncAfter)
	}
	for name, ttl := range c.Sync.TTL {
		if ttl.Duration <= 0 {
			return fmt.Errorf("sync.ttl[%s] must be positive, got %s", name, ttl)
		}
	}
	for key, date := range map[string]string{"consent.agreed": c.Consent.Agreed, "setup.completed": c.Setup.Completed} {
		if date == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return fmt.Errorf("%s is %q, want a date such as 2026-09-27", key, date)
		}
	}
	for _, p := range c.Duties.Paused {
		known := false
		for _, n := range DutyNames {
			known = known || p == n
		}
		if !known {
			return fmt.Errorf("duties.paused has %q, want some of %s", p, strings.Join(DutyNames, ", "))
		}
	}
	if c.Consent.Notice < 0 {
		return fmt.Errorf("consent.notice must not be negative, got %d", c.Consent.Notice)
	}
	// Strictly increasing thresholds; otherwise Classify would skip a level and
	// a caveat would silently never fire.
	if !(0 < c.Confidence.VeryLowBelow &&
		c.Confidence.VeryLowBelow < c.Confidence.LowBelow &&
		c.Confidence.LowBelow < c.Confidence.ModerateBelow) {
		return fmt.Errorf("confidence thresholds must be 0 < very_low_below (%d) < low_below (%d) < moderate_below (%d)",
			c.Confidence.VeryLowBelow, c.Confidence.LowBelow, c.Confidence.ModerateBelow)
	}
	return nil
}

// OverlayFile resolves the overlay location against the config directory.
func (c Config) OverlayFile(p Paths) string {
	if c.OverlayPath != "" {
		return c.OverlayPath
	}
	return filepath.Join(p.ConfigDir, "wot-overlay.yaml")
}

// TTLFor returns the refetch interval for a source, falling back to one hour
// for a source the config does not mention.
func (c Config) TTLFor(source string) time.Duration {
	if ttl, ok := c.Sync.TTL[source]; ok {
		return ttl.Duration
	}
	return time.Hour
}

// Duration is a time.Duration that reads from YAML as a string such as "6h"
// or "90d", which keeps config files legible.
type Duration struct {
	time.Duration
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string such as \"6h\": %w", err)
	}
	parsed, err := ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

// ParseDuration is time.ParseDuration plus whole days ("90d"), which a
// retention window is naturally stated in.
func ParseDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return parsed, nil
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) {
	return d.Duration.String(), nil
}
