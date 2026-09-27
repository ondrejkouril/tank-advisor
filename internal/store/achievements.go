package store

import (
	"context"
	"fmt"
	"time"
)

// AchievementDef is an achievement's definition.
type AchievementDef struct {
	Code         string
	Name         string
	Description  string
	Condition    string
	HeroInfo     string
	Section      string
	SectionOrder int
	Order        int
	Image        string
	SyncedAt     time.Time
}

// AchievementCount is one achievement and how many times it was earned.
type AchievementCount struct {
	Kind  string
	Code  string
	Count int
	// TankID is 0 for an account-wide count.
	TankID int
}

// UpsertAchievementDefs writes the achievement definitions.
func (db *DB) UpsertAchievementDefs(ctx context.Context, defs []AchievementDef) (int, error) {
	if len(defs) == 0 {
		return 0, nil
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storing achievement definitions: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO achievement_defs(code, name, description, condition, hero_info,
			section, section_order, "order", image, synced_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET
			name = excluded.name, description = excluded.description,
			condition = excluded.condition, hero_info = excluded.hero_info,
			section = excluded.section, section_order = excluded.section_order,
			"order" = excluded."order", image = excluded.image,
			synced_at = excluded.synced_at`)
	if err != nil {
		return 0, fmt.Errorf("storing achievement definitions: %w", err)
	}
	defer stmt.Close()

	for _, d := range defs {
		if d.Code == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, d.Code, d.Name, d.Description, d.Condition,
			d.HeroInfo, d.Section, d.SectionOrder, d.Order, d.Image, formatTime(d.SyncedAt)); err != nil {
			return 0, fmt.Errorf("storing achievement %s: %w", d.Code, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storing achievement definitions: %w", err)
	}
	return len(defs), nil
}

// AchievementDefCount returns how many definitions are known.
func (db *DB) AchievementDefCount(ctx context.Context) (int, error) {
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM achievement_defs`).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting achievement definitions: %w", err)
	}
	return n, nil
}

// PutAccountAchievements records account-wide achievement counts.
func (db *DB) PutAccountAchievements(ctx context.Context, snapshotID int64, counts []AchievementCount) (int, error) {
	if snapshotID == 0 {
		return 0, fmt.Errorf("achievements need a snapshot id")
	}
	if len(counts) == 0 {
		return 0, nil
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storing account achievements: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO account_achievements(snapshot_id, kind, code, count)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(snapshot_id, kind, code) DO UPDATE SET count = excluded.count`)
	if err != nil {
		return 0, fmt.Errorf("storing account achievements: %w", err)
	}
	defer stmt.Close()

	for _, c := range counts {
		if c.Kind == "" || c.Code == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, snapshotID, c.Kind, c.Code, c.Count); err != nil {
			return 0, fmt.Errorf("storing achievement %s: %w", c.Code, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storing account achievements: %w", err)
	}
	return len(counts), nil
}

// PutTankAchievements records per-tank achievement counts.
func (db *DB) PutTankAchievements(ctx context.Context, snapshotID int64, counts []AchievementCount) (int, error) {
	if snapshotID == 0 {
		return 0, fmt.Errorf("achievements need a snapshot id")
	}
	if len(counts) == 0 {
		return 0, nil
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storing tank achievements: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO tank_achievements(snapshot_id, tank_id, kind, code, count)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(snapshot_id, tank_id, kind, code) DO UPDATE SET count = excluded.count`)
	if err != nil {
		return 0, fmt.Errorf("storing tank achievements: %w", err)
	}
	defer stmt.Close()

	stored := 0
	for _, c := range counts {
		if c.TankID == 0 || c.Kind == "" || c.Code == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, snapshotID, c.TankID, c.Kind, c.Code, c.Count); err != nil {
			return 0, fmt.Errorf("storing achievement %s for tank %d: %w", c.Code, c.TankID, err)
		}
		stored++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storing tank achievements: %w", err)
	}
	return stored, nil
}

// AchievementWithDef pairs a count with its definition, which is the only form
// in which a count is meaningful to a reader.
type AchievementWithDef struct {
	AchievementCount
	Def AchievementDef
	// HasDef reports whether a definition was found. A code with no definition
	// is reported as-is rather than dropped, since the count is still real.
	HasDef bool
}

// LatestAccountAchievements returns the most recent account-wide counts joined
// to their definitions.
func (db *DB) LatestAccountAchievements(ctx context.Context) ([]AchievementWithDef, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT a.kind, a.code, a.count,
		       COALESCE(d.name, ''), COALESCE(d.description, ''), COALESCE(d.condition, ''),
		       COALESCE(d.section, ''), d.code IS NOT NULL
		FROM account_achievements a
		LEFT JOIN achievement_defs d ON d.code = a.code
		WHERE a.snapshot_id = (
			SELECT aa.snapshot_id FROM account_achievements aa
			JOIN snapshots s ON s.id = aa.snapshot_id
			ORDER BY s.requested_at DESC, s.id DESC LIMIT 1
		)
		ORDER BY a.kind, a.code`)
	if err != nil {
		return nil, fmt.Errorf("reading account achievements: %w", err)
	}
	defer rows.Close()

	var out []AchievementWithDef
	for rows.Next() {
		var a AchievementWithDef
		if err := rows.Scan(&a.Kind, &a.Code, &a.Count,
			&a.Def.Name, &a.Def.Description, &a.Def.Condition, &a.Def.Section, &a.HasDef); err != nil {
			return nil, fmt.Errorf("reading account achievements: %w", err)
		}
		a.Def.Code = a.Code
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading account achievements: %w", err)
	}
	return out, nil
}

// LatestTankAchievements returns one tank's most recent counts with
// definitions.
func (db *DB) LatestTankAchievements(ctx context.Context, tankID int) ([]AchievementWithDef, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT a.kind, a.code, a.count,
		       COALESCE(d.name, ''), COALESCE(d.description, ''), COALESCE(d.condition, ''),
		       COALESCE(d.section, ''), d.code IS NOT NULL
		FROM tank_achievements a
		LEFT JOIN achievement_defs d ON d.code = a.code
		WHERE a.tank_id = ? AND a.snapshot_id = (
			SELECT ta.snapshot_id FROM tank_achievements ta
			JOIN snapshots s ON s.id = ta.snapshot_id
			ORDER BY s.requested_at DESC, s.id DESC LIMIT 1
		)
		ORDER BY a.kind, a.code`, tankID)
	if err != nil {
		return nil, fmt.Errorf("reading tank achievements: %w", err)
	}
	defer rows.Close()

	var out []AchievementWithDef
	for rows.Next() {
		var a AchievementWithDef
		if err := rows.Scan(&a.Kind, &a.Code, &a.Count,
			&a.Def.Name, &a.Def.Description, &a.Def.Condition, &a.Def.Section, &a.HasDef); err != nil {
			return nil, fmt.Errorf("reading tank achievements: %w", err)
		}
		a.Def.Code = a.Code
		a.TankID = tankID
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading tank achievements: %w", err)
	}
	return out, nil
}
