package store

// schemaV7 stores the researched vehicles from the client mod's dump (mod
// 0.3.0), which retire the overlay's hand-kept researched_not_bought list.
//
// has_unlocked tells a dump that listed nothing from one that had no list at
// all: a dump from mod 0.2.x says nothing about research, and must not read as
// "nothing researched".
const schemaV7 = `
CREATE TABLE mod_unlocked(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  tank_id     INTEGER NOT NULL,
  PRIMARY KEY(snapshot_id, tank_id)
);

ALTER TABLE mod_account ADD COLUMN has_unlocked INTEGER NOT NULL DEFAULT 0;
`
