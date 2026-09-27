package store

// TankStatsParserVersion is the version of the tanks/stats parser whose output
// a tank_stats row holds. Rows written by an older parser lack fields added
// since, and are brought up to date by re-parsing the snapshot they came from -
// the raw body is kept for exactly this reason, so no request is spent.
//
// Version 2 adds the fields WN8 needs.
const TankStatsParserVersion = 2

// schemaV4 adds the per-tank totals WN8 is computed from, and records which
// vintage of expected values is stored.
//
// dropped_capture_points is WN8's "def", and was simply not parsed before. The
// three assist totals were believed absent from the random block; they are
// present as totals, only the avg_* fields are missing (measured 2026-09-18).
// Rows written before this migration default to parser_version 1, which is how
// the re-parse finds them.
const schemaV4 = `
ALTER TABLE tank_stats ADD COLUMN capture_points         INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tank_stats ADD COLUMN dropped_capture_points INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tank_stats ADD COLUMN radio_assisted_damage  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tank_stats ADD COLUMN track_assisted_damage  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tank_stats ADD COLUMN stun_assisted_damage   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tank_stats ADD COLUMN parser_version         INTEGER NOT NULL DEFAULT 1;

-- XVM stamps each file with a version date, which lags the fetch by days. A WN8
-- figure quotes it, since ratings are not comparable across vintages.
ALTER TABLE wn8_expected ADD COLUMN version TEXT NOT NULL DEFAULT '';
`
