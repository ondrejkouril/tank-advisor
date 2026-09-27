package store

// schemaV3 removes the Tomato.gg tables and adds WN8 expected values.
//
// The Tomato.gg API turned out to require a paid subscription for an API key,
// so it was dropped from the design (docs/spec.md section 3.2). The three
// tables that cached its computed windows are removed rather than left empty:
// a table nothing can ever fill is a standing invitation to write a query that
// silently returns nothing.
//
// Two tables that were introduced for Tomato.gg are kept, because the planned
// client mod (section 3.6) is a genuine source for both and neither name is
// provider-specific:
//
//   - moe_progression: Marks of Excellence, absent from the Wargaming API
//     entirely, so the mod will be the only source.
//   - loadouts: equipment and crew, likewise never exposed by the API.
//
// WN8 replaces what Tomato.gg would have supplied as a number. It is computed
// locally (section 6.2) from cumulative stats plus XVM's freely published
// expected values, which land in wn8_expected.
const schemaV3 = `
DROP TABLE IF EXISTS tomato_recents;
DROP TABLE IF EXISTS tomato_tank_recents;
DROP TABLE IF EXISTS tomato_sessions;

-- Expected values behind WN8, from XVM. Slow-changing reference data like the
-- vehicle encyclopedia. synced_at is kept so a WN8 figure can state which
-- vintage of expected values produced it - the table is updated daily upstream,
-- and a rating is not comparable across vintages.
CREATE TABLE wn8_expected(
  tank_id   INTEGER PRIMARY KEY,
  damage    REAL NOT NULL,
  frags     REAL NOT NULL,
  spot      REAL NOT NULL,
  def       REAL NOT NULL,
  win_rate  REAL NOT NULL,
  synced_at TEXT NOT NULL
);
`
