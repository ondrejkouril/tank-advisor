package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Vehicle is a row of the reference vehicle list.
type Vehicle struct {
	TankID      int
	Name        string
	ShortName   string
	Tier        int
	Type        string
	Nation      string
	Tag         string
	IsPremium   bool
	IsGift      bool
	IsWheeled   bool
	PriceCredit int
	PriceGold   int
	SyncedAt    time.Time
}

// Edge is a tech-tree link.
type Edge struct {
	From   int
	To     int
	XPCost int
	Source string
}

// UpsertVehicles writes the reference vehicle list.
//
// Rows are updated rather than replaced wholesale, because a vehicle removed
// from the game still appears in the player's history and deleting it would
// orphan stats that are perfectly real.
func (db *DB) UpsertVehicles(ctx context.Context, vehicles []Vehicle) (int, error) {
	if len(vehicles) == 0 {
		return 0, nil
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storing vehicles: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vehicles(tank_id, name, short_name, tier, type, nation, tag,
			is_premium, is_gift, is_wheeled, price_credit, price_gold, synced_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tank_id) DO UPDATE SET
			name = excluded.name, short_name = excluded.short_name,
			tier = excluded.tier, type = excluded.type, nation = excluded.nation,
			tag = excluded.tag, is_premium = excluded.is_premium,
			is_gift = excluded.is_gift, is_wheeled = excluded.is_wheeled,
			price_credit = excluded.price_credit, price_gold = excluded.price_gold,
			synced_at = excluded.synced_at`)
	if err != nil {
		return 0, fmt.Errorf("storing vehicles: %w", err)
	}
	defer stmt.Close()

	for _, v := range vehicles {
		if _, err := stmt.ExecContext(ctx,
			v.TankID, v.Name, v.ShortName, v.Tier, v.Type, v.Nation, v.Tag,
			boolToInt(v.IsPremium), boolToInt(v.IsGift), boolToInt(v.IsWheeled),
			v.PriceCredit, v.PriceGold, formatTime(v.SyncedAt),
		); err != nil {
			return 0, fmt.Errorf("storing vehicle %d (%s): %w", v.TankID, v.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storing vehicles: %w", err)
	}
	return len(vehicles), nil
}

// ReplaceEdgesFromSource swaps in a complete set of edges for one source.
//
// It is scoped by source so that re-syncing the API cannot delete the overlay's
// hand-maintained edges, which exist precisely because the API is missing
// something.
func (db *DB) ReplaceEdgesFromSource(ctx context.Context, source string, edges []Edge) (int, error) {
	if source == "" {
		return 0, fmt.Errorf("edges need a source")
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storing edges: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM vehicle_edges WHERE source = ?`, source); err != nil {
		return 0, fmt.Errorf("clearing %s edges: %w", source, err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vehicle_edges(from_tank_id, to_tank_id, xp_cost, source)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(from_tank_id, to_tank_id, source) DO UPDATE SET xp_cost = excluded.xp_cost`)
	if err != nil {
		return 0, fmt.Errorf("storing edges: %w", err)
	}
	defer stmt.Close()

	for _, e := range edges {
		if e.From == 0 || e.To == 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, e.From, e.To, e.XPCost, source); err != nil {
			return 0, fmt.Errorf("storing edge %d->%d: %w", e.From, e.To, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storing edges: %w", err)
	}
	return len(edges), nil
}

// VehicleCount returns how many vehicles are known.
func (db *DB) VehicleCount(ctx context.Context) (int, error) {
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM vehicles`).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting vehicles: %w", err)
	}
	return n, nil
}

// EdgeCount returns how many tech-tree edges are known, optionally for one
// source.
func (db *DB) EdgeCount(ctx context.Context, source string) (int, error) {
	query := `SELECT COUNT(*) FROM vehicle_edges`
	args := []any{}
	if source != "" {
		query += ` WHERE source = ?`
		args = append(args, source)
	}

	var n int
	if err := db.sql.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting edges: %w", err)
	}
	return n, nil
}

// VehicleCountsByTier reports how many vehicles exist at each tier.
//
// This is how the Tier XI question gets answered: if the API does not return
// tier 11 vehicles, the count is simply absent, and the overlay has to supply
// those research paths instead.
func (db *DB) VehicleCountsByTier(ctx context.Context) (map[int]int, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT tier, COUNT(*) FROM vehicles GROUP BY tier ORDER BY tier`)
	if err != nil {
		return nil, fmt.Errorf("counting vehicles by tier: %w", err)
	}
	defer rows.Close()

	counts := map[int]int{}
	for rows.Next() {
		var tier, n int
		if err := rows.Scan(&tier, &n); err != nil {
			return nil, fmt.Errorf("counting vehicles by tier: %w", err)
		}
		counts[tier] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("counting vehicles by tier: %w", err)
	}
	return counts, nil
}

// VehicleByID returns one vehicle.
func (db *DB) VehicleByID(ctx context.Context, tankID int) (Vehicle, error) {
	row := db.sql.QueryRowContext(ctx, vehicleSelect+` WHERE tank_id = ?`, tankID)
	return scanVehicle(row)
}

// AmbiguousNameError reports that a name matches more than one vehicle.
//
// The vehicle list has genuine collisions - two tier VII "IS-2"s, two "T-34-85
// Rudy"s - so picking one silently would attach a player's stats or goals to
// the wrong tank. The caller has to disambiguate, normally with a tank_id.
type AmbiguousNameError struct {
	Name       string
	Candidates []Vehicle
}

func (e *AmbiguousNameError) Error() string {
	parts := make([]string, 0, len(e.Candidates))
	for _, v := range e.Candidates {
		parts = append(parts, fmt.Sprintf("%s (tank_id %d, tier %d %s, %s)",
			v.Name, v.TankID, v.Tier, v.Type, v.Nation))
	}
	return fmt.Sprintf("%q matches %d vehicles: %s", e.Name, len(e.Candidates), strings.Join(parts, "; "))
}

// VehicleByName resolves a vehicle by its full or short name.
//
// An exact, case-insensitive match wins. Failing that, names are compared with
// punctuation and spacing ignored, which is how people actually write them:
// "Obj 277" for the API's "Obj. 277". The loose pass is only a fallback because
// it conflates real, distinct vehicles - "T29" and "T-29", "T34" and "T-34" -
// that an exact match keeps apart.
//
// More than one match at either stage is an *AmbiguousNameError; none is
// ErrNotFound.
func (db *DB) VehicleByName(ctx context.Context, name string) (Vehicle, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Vehicle{}, ErrNotFound
	}

	exact, err := db.vehicles(ctx, vehicleSelect+`
		WHERE name = ? COLLATE NOCASE OR short_name = ? COLLATE NOCASE
		ORDER BY tier, tank_id`, trimmed, trimmed)
	if err != nil {
		return Vehicle{}, err
	}
	if v, err := single(trimmed, exact); !errors.Is(err, ErrNotFound) {
		return v, err
	}

	key := looseKey(trimmed)
	if key == "" {
		return Vehicle{}, ErrNotFound
	}
	all, err := db.vehicles(ctx, vehicleSelect+` ORDER BY tier, tank_id`)
	if err != nil {
		return Vehicle{}, err
	}
	var loose []Vehicle
	for _, v := range all {
		if looseKey(v.Name) == key || looseKey(v.ShortName) == key {
			loose = append(loose, v)
		}
	}
	return single(trimmed, loose)
}

// single returns the one vehicle in matches, or says why there is not one.
func single(name string, matches []Vehicle) (Vehicle, error) {
	switch len(matches) {
	case 0:
		return Vehicle{}, ErrNotFound
	case 1:
		return matches[0], nil
	default:
		return Vehicle{}, &AmbiguousNameError{Name: name, Candidates: matches}
	}
}

// looseKey reduces a name to its lower-cased letters and digits, with
// diacritics removed: people type "Tesak" and "Selma" for the API's "Tesák" and
// "Šelma", and "Chatillon" for "Châtillon". Decomposing to NFD splits each
// accented letter into its base and a combining mark, and the marks are not
// letters, so they fall out with the punctuation.
func looseKey(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SuggestVehicleNames returns names close to the given one, for a "did you
// mean" on a misspelled overlay entry.
func (db *DB) SuggestVehicleNames(ctx context.Context, name string, limit int) ([]string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	rows, err := db.sql.QueryContext(ctx,
		`SELECT DISTINCT name FROM vehicles WHERE name LIKE ? COLLATE NOCASE LIMIT ?`,
		"%"+trimmed+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("suggesting vehicle names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("suggesting vehicle names: %w", err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("suggesting vehicle names: %w", err)
	}

	// A substring match finds nothing for a transposition or a wrong word, so
	// fall back to comparing against every name.
	if len(names) == 0 {
		return db.suggestByDistance(ctx, trimmed, limit)
	}
	return names, nil
}

func (db *DB) suggestByDistance(ctx context.Context, name string, limit int) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT name FROM vehicles`)
	if err != nil {
		return nil, fmt.Errorf("suggesting vehicle names: %w", err)
	}
	defer rows.Close()

	type scored struct {
		name     string
		distance int
	}
	var candidates []scored

	lower := strings.ToLower(name)
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("suggesting vehicle names: %w", err)
		}
		d := editDistance(lower, strings.ToLower(n))
		// Anything further away than this is noise rather than a suggestion.
		if d <= len(lower)/2+2 {
			candidates = append(candidates, scored{name: n, distance: d})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("suggesting vehicle names: %w", err)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		return candidates[i].name < candidates[j].name
	})

	// Distinct vehicles can share a name (two "IS-2"s), and a suggestion list
	// repeating one is noise.
	names := make([]string, 0, limit)
	seen := map[string]bool{}
	for _, c := range candidates {
		if len(names) == limit {
			break
		}
		if seen[c.name] {
			continue
		}
		seen[c.name] = true
		names = append(names, c.name)
	}
	return names, nil
}

// editDistance is Levenshtein distance, used only to rank name suggestions.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}

	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

// NextVehicles returns what can be researched from a vehicle.
func (db *DB) NextVehicles(ctx context.Context, tankID int) ([]Edge, error) {
	return db.edges(ctx, `SELECT from_tank_id, to_tank_id, xp_cost, source
		FROM vehicle_edges WHERE from_tank_id = ? ORDER BY to_tank_id, source`, tankID)
}

// PreviousVehicles returns what a vehicle can be researched from.
func (db *DB) PreviousVehicles(ctx context.Context, tankID int) ([]Edge, error) {
	return db.edges(ctx, `SELECT from_tank_id, to_tank_id, xp_cost, source
		FROM vehicle_edges WHERE to_tank_id = ? ORDER BY from_tank_id, source`, tankID)
}

func (db *DB) edges(ctx context.Context, query string, args ...any) ([]Edge, error) {
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading edges: %w", err)
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.From, &e.To, &e.XPCost, &e.Source); err != nil {
			return nil, fmt.Errorf("reading edges: %w", err)
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading edges: %w", err)
	}
	return edges, nil
}

const vehicleSelect = `
	SELECT tank_id, name, short_name, tier, type, nation, tag,
	       is_premium, is_gift, is_wheeled, price_credit, price_gold, synced_at
	FROM vehicles`

func (db *DB) vehicles(ctx context.Context, query string, args ...any) ([]Vehicle, error) {
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading vehicles: %w", err)
	}
	defer rows.Close()

	var out []Vehicle
	for rows.Next() {
		v, err := scanVehicle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading vehicles: %w", err)
	}
	return out, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanVehicle(row rowScanner) (Vehicle, error) {
	var (
		v         Vehicle
		premium   int
		gift      int
		wheeled   int
		syncedAt  string
		scanError error
	)
	scanError = row.Scan(&v.TankID, &v.Name, &v.ShortName, &v.Tier, &v.Type, &v.Nation, &v.Tag,
		&premium, &gift, &wheeled, &v.PriceCredit, &v.PriceGold, &syncedAt)
	if errors.Is(scanError, sql.ErrNoRows) {
		return Vehicle{}, ErrNotFound
	}
	if scanError != nil {
		return Vehicle{}, fmt.Errorf("reading vehicle: %w", scanError)
	}

	v.IsPremium = premium != 0
	v.IsGift = gift != 0
	v.IsWheeled = wheeled != 0

	var err error
	if v.SyncedAt, err = parseTime(syncedAt); err != nil {
		return Vehicle{}, err
	}
	return v, nil
}
