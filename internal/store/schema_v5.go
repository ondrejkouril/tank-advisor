package store

// schemaV5 adds personal missions.
//
// pm_missions is reference data from encyclopedia/personalmissions, replaced
// whole on each fetch. pm_status is per snapshot, like the garage: the
// statuses ride on the account/info response, so each account snapshot
// records which missions were complete at that moment. A mission with no row
// has not been completed - the API does not say whether it was started.
const schemaV5 = `
CREATE TABLE pm_missions(
  mission_id  INTEGER PRIMARY KEY,
  campaign_id INTEGER NOT NULL,
  campaign    TEXT    NOT NULL DEFAULT '',
  operation_id INTEGER NOT NULL,
  operation   TEXT    NOT NULL DEFAULT '',
  set_id      INTEGER NOT NULL DEFAULT 0,
  name        TEXT    NOT NULL DEFAULT '',
  class       TEXT    NOT NULL DEFAULT '',
  min_tier    INTEGER NOT NULL DEFAULT 0,
  max_tier    INTEGER NOT NULL DEFAULT 0,
  primary_conditions   TEXT NOT NULL DEFAULT '',
  secondary_conditions TEXT NOT NULL DEFAULT '',
  synced_at   TEXT    NOT NULL
);

CREATE TABLE pm_status(
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  mission_id  INTEGER NOT NULL,
  status      TEXT    NOT NULL,
  PRIMARY KEY(snapshot_id, mission_id)
);
`
