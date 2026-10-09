package store

// schemaV9 keeps the server's game version as Wargaming's encyclopedia/info
// reports it (docs/plan.md phase 6, step S5). One row per version: when a
// sync first and last saw it. first_seen dates a patch to within a sync, so
// an answer can say a comparison straddles an update.
const schemaV9 = `
CREATE TABLE game_versions(
  version TEXT PRIMARY KEY,
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  tanks_updated_at TEXT
);
`
