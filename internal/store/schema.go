package store

// migrations are applied in order; each is recorded in PRAGMA user_version, so
// applying them twice is a no-op. Never edit a migration that has shipped - add
// another one.
var migrations = []struct {
	version int
	stmts   string
}{
	{version: 1, stmts: schemaV1},
	{version: 2, stmts: schemaV2},
	{version: 3, stmts: schemaV3},
	{version: 4, stmts: schemaV4},
	{version: 5, stmts: schemaV5},
	{version: 6, stmts: schemaV6},
	{version: 7, stmts: schemaV7},
	{version: 8, stmts: schemaV8},
}

// schemaV1 is docs/spec.md section 4 in full. Later plan steps populate tables
// they do not yet write to, but the schema lands in one piece so that the
// provenance story - every parsed row traceable to the snapshot it came from -
// is enforced by foreign keys from the start.
const schemaV1 = `
-- Provenance. Every HTTP response body is stored gzipped before parsing, so a
-- parser fix or an upstream schema change is a re-parse rather than a re-fetch.
-- That matters when the budget is 60 requests a minute.
CREATE TABLE snapshots(
  id            INTEGER PRIMARY KEY,
  source        TEXT    NOT NULL,          -- 'wg' | 'tomato'
  endpoint      TEXT    NOT NULL,          -- 'account/info', 'recents', ...
  requested_at  TEXT    NOT NULL,          -- RFC3339 UTC
  http_status   INTEGER NOT NULL,
  wg_error      TEXT    NOT NULL DEFAULT '',
  etag          TEXT    NOT NULL DEFAULT '',
  not_modified  INTEGER NOT NULL DEFAULT 0,
  raw_gz        BLOB
);
CREATE INDEX snapshots_by_source ON snapshots(source, endpoint, requested_at DESC);

CREATE TABLE http_cache(
  url_key       TEXT PRIMARY KEY,
  etag          TEXT NOT NULL DEFAULT '',
  last_modified TEXT NOT NULL DEFAULT '',
  fetched_at    TEXT NOT NULL
);

CREATE TABLE sync_runs(
  id          INTEGER PRIMARY KEY,
  started_at  TEXT NOT NULL,
  finished_at TEXT,
  ok          INTEGER,
  notes       TEXT NOT NULL DEFAULT ''
);

-- Reference data from encyclopedia/vehicles.
CREATE TABLE vehicles(
  tank_id      INTEGER PRIMARY KEY,
  name         TEXT    NOT NULL,
  short_name   TEXT    NOT NULL DEFAULT '',
  tier         INTEGER NOT NULL,
  type         TEXT    NOT NULL,           -- heavyTank | mediumTank | lightTank | AT-SPG | SPG
  nation       TEXT    NOT NULL,
  tag          TEXT    NOT NULL DEFAULT '',
  is_premium   INTEGER NOT NULL DEFAULT 0,
  is_gift      INTEGER NOT NULL DEFAULT 0,
  is_wheeled   INTEGER NOT NULL DEFAULT 0,
  price_credit INTEGER NOT NULL DEFAULT 0,
  price_gold   INTEGER NOT NULL DEFAULT 0,
  synced_at    TEXT    NOT NULL
);
CREATE INDEX vehicles_by_tier_type ON vehicles(tier, type);
CREATE INDEX vehicles_by_name ON vehicles(name);

-- Tech-tree edges. source records where an edge came from: the API's next_tanks
-- or prices_xp maps, or the hand-maintained overlay when the API omits a tier.
-- Keeping that distinction is what lets an answer say how it knows.
CREATE TABLE vehicle_edges(
  from_tank_id INTEGER NOT NULL,
  to_tank_id   INTEGER NOT NULL,
  xp_cost      INTEGER NOT NULL DEFAULT 0,
  source       TEXT    NOT NULL,           -- 'next_tanks' | 'prices_xp' | 'overlay'
  PRIMARY KEY(from_tank_id, to_tank_id, source)
);
CREATE INDEX vehicle_edges_to ON vehicle_edges(to_tank_id);

-- Account state, one row per sync, so resource history is a free by-product.
CREATE TABLE account_snapshots(
  snapshot_id        INTEGER PRIMARY KEY REFERENCES snapshots(id) ON DELETE CASCADE,
  observed_at        TEXT    NOT NULL,
  credits            INTEGER,
  gold               INTEGER,
  bonds              INTEGER,
  free_xp            INTEGER,
  is_premium         INTEGER,
  premium_expires_at TEXT,
  global_rating      INTEGER,
  last_battle_time   TEXT,
  battle_life_time   INTEGER
);

CREATE TABLE garage(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  tank_id     INTEGER NOT NULL,
  PRIMARY KEY(snapshot_id, tank_id)
);

CREATE TABLE boosters(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  kind        TEXT    NOT NULL,
  count       INTEGER NOT NULL DEFAULT 0,
  state       TEXT    NOT NULL DEFAULT '', -- ACTIVE | INACTIVE | USED
  expires_at  TEXT,
  PRIMARY KEY(snapshot_id, kind, state)
);

-- Per-tank cumulative stats, retained per snapshot so that recent form can be
-- derived as a delta between two snapshots. The Wargaming API offers no
-- windowed view, so this table is the only WG-side source of "lately".
CREATE TABLE tank_stats(
  snapshot_id                INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  tank_id                    INTEGER NOT NULL,
  mode                       TEXT    NOT NULL,   -- 'all' | 'random' | 'epic' | ...
  battles                    INTEGER NOT NULL DEFAULT 0,
  wins                       INTEGER NOT NULL DEFAULT 0,
  losses                     INTEGER NOT NULL DEFAULT 0,
  draws                      INTEGER NOT NULL DEFAULT 0,
  survived                   INTEGER NOT NULL DEFAULT 0,
  damage_dealt               INTEGER NOT NULL DEFAULT 0,
  damage_received            INTEGER NOT NULL DEFAULT 0,
  frags                      INTEGER NOT NULL DEFAULT 0,
  spotted                    INTEGER NOT NULL DEFAULT 0,
  xp                         INTEGER NOT NULL DEFAULT 0,
  battle_avg_xp              INTEGER NOT NULL DEFAULT 0,
  hits_percents              INTEGER NOT NULL DEFAULT 0,
  avg_damage_assisted        REAL    NOT NULL DEFAULT 0,
  avg_damage_assisted_radio  REAL    NOT NULL DEFAULT 0,
  avg_damage_assisted_track  REAL    NOT NULL DEFAULT 0,
  avg_damage_assisted_stun   REAL    NOT NULL DEFAULT 0,
  avg_damage_blocked         REAL    NOT NULL DEFAULT 0,
  tanking_factor             REAL    NOT NULL DEFAULT 0,
  mark_of_mastery            INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(snapshot_id, tank_id, mode)
);
CREATE INDEX tank_stats_by_tank ON tank_stats(tank_id, mode);

-- Tomato.gg. It keeps its own history, so it is the source of truth for recent
-- form; these tables cache the windows it computes.
CREATE TABLE tomato_recents(
  snapshot_id    INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  window_days    INTEGER NOT NULL DEFAULT 0,
  window_battles INTEGER NOT NULL DEFAULT 0,
  battles        INTEGER NOT NULL DEFAULT 0,
  wr             REAL,
  dpg            REAL,
  wn8            REAL,
  wnx            REAL,
  kpg            REAL,
  survival       REAL,
  assist         REAL,
  spots          REAL,
  PRIMARY KEY(snapshot_id, window_days, window_battles)
);

CREATE TABLE tomato_tank_recents(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  tank_id     INTEGER NOT NULL,
  window_days INTEGER NOT NULL,
  battles     INTEGER NOT NULL DEFAULT 0,
  wr          REAL,
  dpg         REAL,
  wn8         REAL,
  PRIMARY KEY(snapshot_id, tank_id, window_days)
);
CREATE INDEX tomato_tank_recents_by_tank ON tomato_tank_recents(tank_id, window_days);

CREATE TABLE tomato_sessions(
  snapshot_id  INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  bucket       TEXT    NOT NULL,           -- 'day' | 'week' | 'month'
  period_start TEXT    NOT NULL,
  battles      INTEGER NOT NULL DEFAULT 0,
  wr           REAL,
  dpg          REAL,
  wn8          REAL,
  PRIMARY KEY(snapshot_id, bucket, period_start)
);

-- Marks of Excellence are absent from the Wargaming API entirely, so this is
-- the only machine-readable record of mark progress.
CREATE TABLE moe_progression(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  tank_id     INTEGER NOT NULL,
  observed_at TEXT    NOT NULL,
  marks       INTEGER NOT NULL DEFAULT 0,
  percent     REAL,
  PRIMARY KEY(snapshot_id, tank_id, observed_at)
);

-- Loadouts depend on the Tomato.gg client mod having uploaded them. Their
-- absence is a known gap reported to the user, never an error.
CREATE TABLE loadouts(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  tank_id     INTEGER NOT NULL,
  slot_kind   TEXT    NOT NULL,            -- equipment | consumable | shell | field_mod | directive | crew
  slot_index  INTEGER NOT NULL,
  item_name   TEXT    NOT NULL DEFAULT '',
  item_id     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(snapshot_id, tank_id, slot_kind, slot_index)
);
CREATE INDEX loadouts_by_tank ON loadouts(tank_id);
`

// schemaV2 adds achievements.
//
// The raw achievement keys are opaque - "medalKay", "warrior", "markOfMastery"
// - so the encyclopedia definitions are stored alongside them. Without the
// definitions a question like "which medals am I close to" cannot be answered,
// only the fact that a medal exists.
const schemaV2 = `
-- Achievement definitions from encyclopedia/achievements. Slow-changing
-- reference data, like vehicles.
CREATE TABLE achievement_defs(
  code        TEXT PRIMARY KEY,
  name        TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  condition   TEXT NOT NULL DEFAULT '',
  hero_info   TEXT NOT NULL DEFAULT '',
  section     TEXT NOT NULL DEFAULT '',
  section_order INTEGER NOT NULL DEFAULT 0,
  "order"     INTEGER NOT NULL DEFAULT 0,
  image       TEXT NOT NULL DEFAULT '',
  synced_at   TEXT NOT NULL
);
CREATE INDEX achievement_defs_by_section ON achievement_defs(section, section_order);

-- Account-wide achievement counts.
--
-- kind separates the three blocks the API returns under different names:
-- 'achievement' is a medal or badge held, 'max_series' is a best streak such as
-- consecutive victories, and 'frags' counts kill-based awards. They share a
-- table because they share a shape and are always read together.
CREATE TABLE account_achievements(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  kind        TEXT    NOT NULL,
  code        TEXT    NOT NULL,
  count       INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(snapshot_id, kind, code)
);

-- Per-tank achievement counts, same kinds.
CREATE TABLE tank_achievements(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  tank_id     INTEGER NOT NULL,
  kind        TEXT    NOT NULL,
  code        TEXT    NOT NULL,
  count       INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(snapshot_id, tank_id, kind, code)
);
CREATE INDEX tank_achievements_by_tank ON tank_achievements(tank_id, code);
`
