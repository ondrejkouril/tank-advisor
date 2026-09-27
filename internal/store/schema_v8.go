package store

// schemaV8 lets a snapshot keep its parsed rows after its raw body is dropped
// (docs/spec-desktop.md section 12.3). A NULL raw_gz already means "the
// request failed", so pruning needs its own marker: a pruned snapshot is
// still usable, it just can no longer be re-parsed.
const schemaV8 = `
ALTER TABLE snapshots ADD COLUMN raw_pruned INTEGER NOT NULL DEFAULT 0;
`
