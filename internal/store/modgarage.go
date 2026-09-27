package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ModAccount is one client mod dump's header and account-level figures.
// Pointers are nil where the mod could not read a field after a game update;
// that has to stay distinguishable from zero.
type ModAccount struct {
	SnapshotID  int64
	CapturedAt  time.Time
	GameVersion string
	ModVersion  string

	Credits, Gold, Bonds, FreeXP *int

	Premium          *bool
	PremiumType      *int
	PremiumExpiresAt time.Time // zero when unknown
	WotPlus          *bool

	// Errors names what the mod could not read in this dump.
	Errors []string
}

// ModVehicle is one vehicle as the client knows it.
type ModVehicle struct {
	TankID int
	// Name is the client's technical name, stable across languages; UserName
	// is the garage's, in the client's language.
	Name     string
	UserName string
	Tier     int
	XP       *int
	Elite    *bool
	Rented   bool
	// MovingAvgDamage is what the Marks of Excellence are computed from.
	MovingAvgDamage *int
}

// MarkProgress is a vehicle's Marks of Excellence and progress to the next.
type MarkProgress struct {
	TankID  int
	Marks   int
	Percent float64
}

// LoadoutSlot is one fitted item. Kind is equipment, shell, consumable or
// directive; an empty slot has no row.
type LoadoutSlot struct {
	TankID    int
	Kind      string
	Index     int
	ItemID    int
	ItemName  string // technical
	UserName  string // as the client shows it
	ShellKind string // shells only: ARMOR_PIERCING, HOLLOW_CHARGE, ...
	Count     *int   // shells only
}

// CrewSeat is one seat; Role is empty when nobody sits in it.
type CrewSeat struct {
	TankID      int
	Slot        int
	Role        string
	Skills      []CrewSkill
	BonusSkills map[string][]string
}

// CrewSkill is a skill and its training, 0-100.
type CrewSkill struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
}

// ModGarage is everything parsed out of one dump.
type ModGarage struct {
	Account  ModAccount
	Vehicles []ModVehicle
	Marks    []MarkProgress
	Loadout  []LoadoutSlot
	Crew     []CrewSeat
	// Unlocked is every researched vehicle; nil when the dump has no list.
	Unlocked []int
}

// PutModGarage stores a parsed dump against its snapshot, in one transaction.
func (db *DB) PutModGarage(ctx context.Context, g ModGarage) error {
	id := g.Account.SnapshotID
	if id == 0 {
		return errors.New("mod garage needs a snapshot id")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storing mod garage: %w", err)
	}
	defer tx.Rollback()

	a := g.Account
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mod_account(snapshot_id, captured_at, game_version, mod_version, credits, gold, bonds,
			free_xp, premium, premium_type, premium_expires_at, wot_plus, errors)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, formatTime(a.CapturedAt), a.GameVersion, a.ModVersion,
		nullInt(a.Credits), nullInt(a.Gold), nullInt(a.Bonds), nullInt(a.FreeXP),
		nullBool(a.Premium), nullInt(a.PremiumType), nullTime(a.PremiumExpiresAt), nullBool(a.WotPlus),
		strings.Join(a.Errors, "\n")); err != nil {
		return fmt.Errorf("storing mod account: %w", err)
	}

	for _, v := range g.Vehicles {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mod_vehicles(snapshot_id, tank_id, name, user_name, tier, xp, elite, rented, moving_avg_damage)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, v.TankID, v.Name, v.UserName, v.Tier, nullInt(v.XP), nullBool(v.Elite),
			boolToInt(v.Rented), nullInt(v.MovingAvgDamage)); err != nil {
			return fmt.Errorf("storing mod vehicle %d: %w", v.TankID, err)
		}
	}
	for _, m := range g.Marks {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO moe_progression(snapshot_id, tank_id, observed_at, marks, percent)
			VALUES(?, ?, ?, ?, ?)`,
			id, m.TankID, formatTime(a.CapturedAt), m.Marks, m.Percent); err != nil {
			return fmt.Errorf("storing marks for %d: %w", m.TankID, err)
		}
	}
	for _, s := range g.Loadout {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO loadouts(snapshot_id, tank_id, slot_kind, slot_index, item_name, item_id, user_name, kind, count)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, s.TankID, s.Kind, s.Index, s.ItemName, s.ItemID, s.UserName, s.ShellKind, nullInt(s.Count)); err != nil {
			return fmt.Errorf("storing %s %d of %d: %w", s.Kind, s.Index, s.TankID, err)
		}
	}
	for _, c := range g.Crew {
		skills, err := json.Marshal(nonNilSkills(c.Skills))
		if err != nil {
			return err
		}
		bonus, err := json.Marshal(nonNilBonus(c.BonusSkills))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mod_crew(snapshot_id, tank_id, slot, role, skills, bonus_skills)
			VALUES(?, ?, ?, ?, ?, ?)`,
			id, c.TankID, c.Slot, c.Role, string(skills), string(bonus)); err != nil {
			return fmt.Errorf("storing crew seat %d of %d: %w", c.Slot, c.TankID, err)
		}
	}

	if g.Unlocked != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE mod_account SET has_unlocked = 1 WHERE snapshot_id = ?`, id); err != nil {
			return fmt.Errorf("storing unlocked vehicles: %w", err)
		}
		for _, tankID := range g.Unlocked {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO mod_unlocked(snapshot_id, tank_id) VALUES(?, ?)`, id, tankID); err != nil {
				return fmt.Errorf("storing unlocked vehicle %d: %w", tankID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storing mod garage: %w", err)
	}
	return nil
}

// ModUnlocked returns one dump's researched vehicles. ok is false when the
// dump carried no list (mod 0.2.x), which is not the same as none.
func (db *DB) ModUnlocked(ctx context.Context, snapshotID int64) (unlocked map[int]bool, ok bool, err error) {
	var has int
	if err := db.sql.QueryRowContext(ctx, `SELECT has_unlocked FROM mod_account WHERE snapshot_id = ?`, snapshotID).Scan(&has); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrNotFound
		}
		return nil, false, fmt.Errorf("reading unlocked vehicles: %w", err)
	}
	if has == 0 {
		return nil, false, nil
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT tank_id FROM mod_unlocked WHERE snapshot_id = ?`, snapshotID)
	if err != nil {
		return nil, false, fmt.Errorf("reading unlocked vehicles: %w", err)
	}
	defer rows.Close()
	unlocked = map[int]bool{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, false, err
		}
		unlocked[id] = true
	}
	return unlocked, true, rows.Err()
}

// LatestModAccount returns the newest dump's account row, by capture time.
func (db *DB) LatestModAccount(ctx context.Context) (ModAccount, error) {
	row := db.sql.QueryRowContext(ctx, `
		SELECT snapshot_id, captured_at, game_version, mod_version, credits, gold, bonds, free_xp,
		       premium, premium_type, premium_expires_at, wot_plus, errors
		FROM mod_account ORDER BY julianday(captured_at) DESC, snapshot_id DESC LIMIT 1`)
	var (
		a                          ModAccount
		captured                   string
		credits, gold, bonds, fxp  sql.NullInt64
		premium, premType, wotPlus sql.NullInt64
		expires                    sql.NullString
		errs                       string
	)
	err := row.Scan(&a.SnapshotID, &captured, &a.GameVersion, &a.ModVersion, &credits, &gold, &bonds, &fxp,
		&premium, &premType, &expires, &wotPlus, &errs)
	if errors.Is(err, sql.ErrNoRows) {
		return ModAccount{}, ErrNotFound
	}
	if err != nil {
		return ModAccount{}, fmt.Errorf("reading the latest mod dump: %w", err)
	}
	if a.CapturedAt, err = parseTime(captured); err != nil {
		return ModAccount{}, err
	}
	if expires.Valid {
		if a.PremiumExpiresAt, err = parseTime(expires.String); err != nil {
			return ModAccount{}, err
		}
	}
	a.Credits, a.Gold, a.Bonds, a.FreeXP = intPtr(credits), intPtr(gold), intPtr(bonds), intPtr(fxp)
	a.Premium, a.PremiumType, a.WotPlus = boolPtr(premium), intPtr(premType), boolPtr(wotPlus)
	if errs != "" {
		a.Errors = strings.Split(errs, "\n")
	}
	return a, nil
}

// ModVehicles returns one dump's vehicles, by tank_id.
func (db *DB) ModVehicles(ctx context.Context, snapshotID int64) ([]ModVehicle, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT tank_id, name, user_name, tier, xp, elite, rented, moving_avg_damage
		FROM mod_vehicles WHERE snapshot_id = ? ORDER BY tank_id`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("reading mod vehicles: %w", err)
	}
	defer rows.Close()
	var out []ModVehicle
	for rows.Next() {
		var (
			v                 ModVehicle
			xp, elite, avgDmg sql.NullInt64
			rented            int
		)
		if err := rows.Scan(&v.TankID, &v.Name, &v.UserName, &v.Tier, &xp, &elite, &rented, &avgDmg); err != nil {
			return nil, fmt.Errorf("reading mod vehicles: %w", err)
		}
		v.XP, v.Elite, v.MovingAvgDamage, v.Rented = intPtr(xp), boolPtr(elite), intPtr(avgDmg), rented != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullBool(p *bool) any {
	if p == nil {
		return nil
	}
	return boolToInt(*p)
}

func intPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func boolPtr(n sql.NullInt64) *bool {
	if !n.Valid {
		return nil
	}
	v := n.Int64 != 0
	return &v
}

func nonNilSkills(s []CrewSkill) []CrewSkill {
	if s == nil {
		return []CrewSkill{}
	}
	return s
}

func nonNilBonus(m map[string][]string) map[string][]string {
	if m == nil {
		return map[string][]string{}
	}
	return m
}

// ModLoadout returns one dump's fitted items for a vehicle, by kind and slot.
func (db *DB) ModLoadout(ctx context.Context, snapshotID int64, tankID int) ([]LoadoutSlot, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT slot_kind, slot_index, item_id, item_name, user_name, kind, count
		FROM loadouts WHERE snapshot_id = ? AND tank_id = ?
		ORDER BY CASE slot_kind WHEN 'equipment' THEN 0 WHEN 'shell' THEN 1 WHEN 'consumable' THEN 2 ELSE 3 END,
		         slot_index`, snapshotID, tankID)
	if err != nil {
		return nil, fmt.Errorf("reading loadout of %d: %w", tankID, err)
	}
	defer rows.Close()
	var out []LoadoutSlot
	for rows.Next() {
		s := LoadoutSlot{TankID: tankID}
		var count sql.NullInt64
		if err := rows.Scan(&s.Kind, &s.Index, &s.ItemID, &s.ItemName, &s.UserName, &s.ShellKind, &count); err != nil {
			return nil, fmt.Errorf("reading loadout of %d: %w", tankID, err)
		}
		s.Count = intPtr(count)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ModCrew returns one dump's crew seats for a vehicle, by slot.
func (db *DB) ModCrew(ctx context.Context, snapshotID int64, tankID int) ([]CrewSeat, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT slot, role, skills, bonus_skills FROM mod_crew
		WHERE snapshot_id = ? AND tank_id = ? ORDER BY slot`, snapshotID, tankID)
	if err != nil {
		return nil, fmt.Errorf("reading crew of %d: %w", tankID, err)
	}
	defer rows.Close()
	var out []CrewSeat
	for rows.Next() {
		c := CrewSeat{TankID: tankID}
		var skills, bonus string
		if err := rows.Scan(&c.Slot, &c.Role, &skills, &bonus); err != nil {
			return nil, fmt.Errorf("reading crew of %d: %w", tankID, err)
		}
		if err := json.Unmarshal([]byte(skills), &c.Skills); err != nil {
			return nil, fmt.Errorf("crew of %d, seat %d: %w", tankID, c.Slot, err)
		}
		if err := json.Unmarshal([]byte(bonus), &c.BonusSkills); err != nil {
			return nil, fmt.Errorf("crew of %d, seat %d: %w", tankID, c.Slot, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ModMarks returns one dump's Marks of Excellence, keyed by tank_id.
func (db *DB) ModMarks(ctx context.Context, snapshotID int64) (map[int]MarkProgress, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT tank_id, marks, percent FROM moe_progression WHERE snapshot_id = ?`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("reading marks: %w", err)
	}
	defer rows.Close()
	out := map[int]MarkProgress{}
	for rows.Next() {
		var m MarkProgress
		var pct sql.NullFloat64
		if err := rows.Scan(&m.TankID, &m.Marks, &pct); err != nil {
			return nil, fmt.Errorf("reading marks: %w", err)
		}
		m.Percent = pct.Float64
		out[m.TankID] = m
	}
	return out, rows.Err()
}
