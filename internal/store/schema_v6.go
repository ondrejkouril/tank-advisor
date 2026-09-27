package store

// schemaV6 stores the client mod's garage dump (docs/spec.md section 3.6).
//
// Each dump is a snapshot like any response (source 'mod', endpoint 'garage',
// requested_at = when the client wrote it), and these tables hold it parsed:
//
//   - mod_account: the dump's header, resources and the client's own premium
//     state, one row per dump.
//   - mod_vehicles: per vehicle what no API has - XP, elite, rented, the
//     moving average behind the marks - plus the client's names.
//   - mod_crew: one row per seat; skills as JSON, since they are only ever
//     read back whole.
//
// The mark count and percentage go into moe_progression and the fitted items
// into loadouts, the two tables schema v1 created for the mod. loadouts gains
// what the dump carries beyond an id: the client's name, a shell's kind and
// count. Crew stays out of loadouts: a seat has skills, not an item.
const schemaV6 = `
CREATE TABLE mod_account(
  snapshot_id  INTEGER PRIMARY KEY REFERENCES snapshots(id) ON DELETE CASCADE,
  captured_at  TEXT    NOT NULL,
  game_version TEXT    NOT NULL DEFAULT '',
  mod_version  TEXT    NOT NULL DEFAULT '',
  credits      INTEGER,
  gold         INTEGER,
  bonds        INTEGER,
  free_xp      INTEGER,
  premium      INTEGER,
  premium_type INTEGER,
  premium_expires_at TEXT,
  wot_plus     INTEGER,
  errors       TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE mod_vehicles(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  tank_id     INTEGER NOT NULL,
  name        TEXT    NOT NULL DEFAULT '',
  user_name   TEXT    NOT NULL DEFAULT '',
  tier        INTEGER NOT NULL DEFAULT 0,
  xp          INTEGER,
  elite       INTEGER,
  rented      INTEGER NOT NULL DEFAULT 0,
  moving_avg_damage INTEGER,
  PRIMARY KEY(snapshot_id, tank_id)
);

CREATE TABLE mod_crew(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  tank_id     INTEGER NOT NULL,
  slot        INTEGER NOT NULL,
  role        TEXT    NOT NULL DEFAULT '',
  skills      TEXT    NOT NULL DEFAULT '[]',
  bonus_skills TEXT   NOT NULL DEFAULT '{}',
  PRIMARY KEY(snapshot_id, tank_id, slot)
);

ALTER TABLE loadouts ADD COLUMN user_name TEXT NOT NULL DEFAULT '';
ALTER TABLE loadouts ADD COLUMN kind      TEXT NOT NULL DEFAULT '';
ALTER TABLE loadouts ADD COLUMN count     INTEGER;
`
