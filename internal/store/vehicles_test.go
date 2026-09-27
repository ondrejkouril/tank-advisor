package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// sampleVehicles mirrors the shape of real data: a tier XI top of line, its
// tier X parent, and a premium with no research path.
func sampleVehicles() []Vehicle {
	return []Vehicle{
		{TankID: 19281, Name: "Concept No. 5", ShortName: "Concept No. 5", Tier: 10,
			Type: "mediumTank", Nation: "uk", PriceCredit: 6100000, SyncedAt: base},
		{TankID: 26705, Name: "Executor", ShortName: "Executor", Tier: 11,
			Type: "mediumTank", Nation: "uk", PriceCredit: 12000000, SyncedAt: base},
		{TankID: 22017, Name: "Object 277", ShortName: "Obj. 277", Tier: 10,
			Type: "heavyTank", Nation: "ussr", PriceCredit: 6100000, SyncedAt: base},
		{TankID: 3329, Name: "T26E4 SuperPershing", ShortName: "SuperPershing", Tier: 8,
			Type: "mediumTank", Nation: "usa", IsPremium: true, PriceGold: 8200, SyncedAt: base},
	}
}

func seedVehicles(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.UpsertVehicles(context.Background(), sampleVehicles()); err != nil {
		t.Fatalf("UpsertVehicles: %v", err)
	}
}

func TestUpsertVehiclesRoundTrips(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedVehicles(t, db)

	n, err := db.VehicleCount(ctx)
	if err != nil {
		t.Fatalf("VehicleCount: %v", err)
	}
	if n != 4 {
		t.Errorf("VehicleCount = %d, want 4", n)
	}

	got, err := db.VehicleByID(ctx, 26705)
	if err != nil {
		t.Fatalf("VehicleByID: %v", err)
	}
	if got.Name != "Executor" || got.Tier != 11 {
		t.Errorf("vehicle = %+v", got)
	}
	if got.IsPremium {
		t.Error("IsPremium round-tripped as true")
	}

	premium, err := db.VehicleByID(ctx, 3329)
	if err != nil {
		t.Fatalf("VehicleByID: %v", err)
	}
	if !premium.IsPremium || premium.PriceGold != 8200 {
		t.Errorf("premium = %+v", premium)
	}
}

// TestUpsertVehiclesUpdatesRatherThanDuplicates covers a re-sync: a patch can
// rename or re-tier a vehicle, and the row must move rather than multiply.
func TestUpsertVehiclesUpdatesRatherThanDuplicates(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedVehicles(t, db)

	changed := []Vehicle{{
		TankID: 26705, Name: "Executor", ShortName: "Exec", Tier: 11,
		Type: "mediumTank", Nation: "uk", PriceCredit: 13000000, SyncedAt: base.Add(time.Hour),
	}}
	if _, err := db.UpsertVehicles(ctx, changed); err != nil {
		t.Fatalf("UpsertVehicles: %v", err)
	}

	if n, _ := db.VehicleCount(ctx); n != 4 {
		t.Errorf("VehicleCount = %d after a re-sync, want 4", n)
	}
	got, err := db.VehicleByID(ctx, 26705)
	if err != nil {
		t.Fatalf("VehicleByID: %v", err)
	}
	if got.ShortName != "Exec" || got.PriceCredit != 13000000 {
		t.Errorf("row was not updated: %+v", got)
	}
}

func TestUpsertVehiclesAcceptsAnEmptyList(t *testing.T) {
	n, err := openTestDB(t).UpsertVehicles(context.Background(), nil)
	if err != nil {
		t.Fatalf("UpsertVehicles(nil): %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
}

// TestVehicleByNameMatchesShortNames is why the overlay can say "Obj. 277"
// rather than the API's "Object 277".
func TestVehicleByNameMatchesShortNames(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedVehicles(t, db)

	for _, query := range []string{"Object 277", "Obj. 277", "obj. 277", "OBJECT 277"} {
		got, err := db.VehicleByName(ctx, query)
		if err != nil {
			t.Errorf("VehicleByName(%q) = %v", query, err)
			continue
		}
		if got.TankID != 22017 {
			t.Errorf("VehicleByName(%q) = %d, want 22017", query, got.TankID)
		}
	}
}

func TestVehicleByNameTrimsAndReportsMisses(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedVehicles(t, db)

	if got, err := db.VehicleByName(ctx, "  Executor  "); err != nil || got.TankID != 26705 {
		t.Errorf("VehicleByName with surrounding space = (%d, %v)", got.TankID, err)
	}
	if _, err := db.VehicleByName(ctx, "Landkreuzer P1000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("VehicleByName on an unknown name = %v, want ErrNotFound", err)
	}
	if _, err := db.VehicleByName(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("VehicleByName(\"\") = %v, want ErrNotFound", err)
	}
}

// TestVehicleByNameIgnoresPunctuationAsAFallback: people write "Obj 277", the
// API says "Obj. 277".
func TestVehicleByNameIgnoresPunctuationAsAFallback(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedVehicles(t, db)

	for _, query := range []string{"Obj 277", "obj277", "Concept No 5", "concept no.5"} {
		if _, err := db.VehicleByName(ctx, query); err != nil {
			t.Errorf("VehicleByName(%q) = %v, want a match", query, err)
		}
	}
	if _, err := db.VehicleByName(ctx, "..."); !errors.Is(err, ErrNotFound) {
		t.Errorf("VehicleByName(\"...\") = %v, want ErrNotFound", err)
	}
}

// TestVehicleByNameRefusesToGuess pins the collisions measured in the live
// vehicle list: two vehicles share the exact name "IS-2", and "T-34" and "T34"
// are distinct tanks that only differ by punctuation.
func TestVehicleByNameRefusesToGuess(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if _, err := db.UpsertVehicles(ctx, []Vehicle{
		{TankID: 3633, Name: "IS-2", ShortName: "IS-2", Tier: 7, Type: "heavyTank", Nation: "ussr", SyncedAt: base},
		{TankID: 59137, Name: "IS-2", ShortName: "IS-2", Tier: 7, Type: "heavyTank", Nation: "ussr", SyncedAt: base},
		{TankID: 1, Name: "T-34", ShortName: "T-34", Tier: 5, Type: "mediumTank", Nation: "ussr", SyncedAt: base},
		{TankID: 2849, Name: "T34", ShortName: "T34", Tier: 8, Type: "heavyTank", Nation: "usa", SyncedAt: base},
	}); err != nil {
		t.Fatalf("UpsertVehicles: %v", err)
	}

	_, err := db.VehicleByName(ctx, "IS-2")
	var ambiguous *AmbiguousNameError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("VehicleByName(IS-2) = %v, want *AmbiguousNameError", err)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Errorf("candidates = %d, want 2", len(ambiguous.Candidates))
	}
	for _, id := range []string{"3633", "59137"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error %q does not name tank_id %s", err, id)
		}
	}

	// An exact match must win over the loose one, or "T34" would be ambiguous.
	for query, want := range map[string]int{"T34": 2849, "T-34": 1} {
		got, err := db.VehicleByName(ctx, query)
		if err != nil || got.TankID != want {
			t.Errorf("VehicleByName(%q) = (%d, %v), want %d", query, got.TankID, err, want)
		}
	}
	// With no exact match, the loose pass finds both and says so.
	if _, err := db.VehicleByName(ctx, "t 34"); !errors.As(err, &ambiguous) {
		t.Errorf("VehicleByName(\"t 34\") = %v, want *AmbiguousNameError", err)
	}
}

// TestSuggestVehicleNames backs the "did you mean" on a mistyped overlay entry.
func TestSuggestVehicleNames(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedVehicles(t, db)

	t.Run("substring", func(t *testing.T) {
		got, err := db.SuggestVehicleNames(ctx, "Concept", 5)
		if err != nil {
			t.Fatalf("SuggestVehicleNames: %v", err)
		}
		if len(got) == 0 || got[0] != "Concept No. 5" {
			t.Errorf("suggestions = %v, want Concept No. 5 first", got)
		}
	})

	t.Run("typo falls back to edit distance", func(t *testing.T) {
		// No substring match, so this exercises the distance path.
		got, err := db.SuggestVehicleNames(ctx, "Exectuor", 3)
		if err != nil {
			t.Fatalf("SuggestVehicleNames: %v", err)
		}
		if len(got) == 0 {
			t.Fatal("a transposed name produced no suggestions")
		}
		if got[0] != "Executor" {
			t.Errorf("suggestions = %v, want Executor first", got)
		}
	})

	t.Run("nonsense suggests nothing", func(t *testing.T) {
		got, err := db.SuggestVehicleNames(ctx, "zzzzzzzzzzzzzzzzzzzz", 3)
		if err != nil {
			t.Fatalf("SuggestVehicleNames: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("suggestions = %v, want none for nonsense", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got, err := db.SuggestVehicleNames(ctx, "", 3)
		if err != nil || len(got) != 0 {
			t.Errorf("SuggestVehicleNames(\"\") = (%v, %v)", got, err)
		}
	})
}

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"executor", "exectuor", 2},
		{"kranvagn", "kranvagen", 1},
	}
	for _, tc := range cases {
		if got := editDistance(tc.a, tc.b); got != tc.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestVehicleCountsByTier(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedVehicles(t, db)

	counts, err := db.VehicleCountsByTier(ctx)
	if err != nil {
		t.Fatalf("VehicleCountsByTier: %v", err)
	}
	for tier, want := range map[int]int{8: 1, 10: 2, 11: 1} {
		if counts[tier] != want {
			t.Errorf("tier %d count = %d, want %d", tier, counts[tier], want)
		}
	}
	if counts[9] != 0 {
		t.Errorf("tier 9 count = %d, want 0", counts[9])
	}
}

func TestEdgesRoundTripInBothDirections(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedVehicles(t, db)

	edges := []Edge{
		{From: 19281, To: 26705, XPCost: 325000, Source: "next_tanks"},
		{From: 18209, To: 19281, XPCost: 220000, Source: "next_tanks"},
	}
	if _, err := db.ReplaceEdgesFromSource(ctx, "next_tanks", edges); err != nil {
		t.Fatalf("ReplaceEdgesFromSource: %v", err)
	}

	next, err := db.NextVehicles(ctx, 19281)
	if err != nil {
		t.Fatalf("NextVehicles: %v", err)
	}
	if len(next) != 1 || next[0].To != 26705 || next[0].XPCost != 325000 {
		t.Errorf("NextVehicles = %+v", next)
	}

	prev, err := db.PreviousVehicles(ctx, 19281)
	if err != nil {
		t.Fatalf("PreviousVehicles: %v", err)
	}
	if len(prev) != 1 || prev[0].From != 18209 {
		t.Errorf("PreviousVehicles = %+v", prev)
	}
}

// TestReplaceEdgesIsScopedBySource is the point of scoping the delete: a
// successful API sync must not wipe the overlay's hand-maintained edges, which
// exist precisely because the API is missing something.
func TestReplaceEdgesIsScopedBySource(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedVehicles(t, db)

	if _, err := db.ReplaceEdgesFromSource(ctx, "overlay", []Edge{
		{From: 19281, To: 26705, XPCost: 325000},
	}); err != nil {
		t.Fatalf("seeding overlay edges: %v", err)
	}
	if _, err := db.ReplaceEdgesFromSource(ctx, "next_tanks", []Edge{
		{From: 18209, To: 19281, XPCost: 220000},
	}); err != nil {
		t.Fatalf("replacing api edges: %v", err)
	}

	overlayCount, err := db.EdgeCount(ctx, "overlay")
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if overlayCount != 1 {
		t.Errorf("overlay edges = %d after an API sync, want 1 - they were wiped", overlayCount)
	}

	total, err := db.EdgeCount(ctx, "")
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if total != 2 {
		t.Errorf("total edges = %d, want 2", total)
	}
}

// TestReplaceEdgesClearsStaleOnesWithinASource covers a line being rearranged
// by a patch: the old edge must disappear rather than linger.
func TestReplaceEdgesClearsStaleOnesWithinASource(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.ReplaceEdgesFromSource(ctx, "next_tanks", []Edge{
		{From: 1, To: 2, XPCost: 100},
	}); err != nil {
		t.Fatalf("first replace: %v", err)
	}
	if _, err := db.ReplaceEdgesFromSource(ctx, "next_tanks", []Edge{
		{From: 1, To: 3, XPCost: 150},
	}); err != nil {
		t.Fatalf("second replace: %v", err)
	}

	next, err := db.NextVehicles(ctx, 1)
	if err != nil {
		t.Fatalf("NextVehicles: %v", err)
	}
	if len(next) != 1 || next[0].To != 3 {
		t.Errorf("NextVehicles = %+v, want only the current edge", next)
	}
}

func TestReplaceEdgesSkipsIncompleteEdgesAndNeedsASource(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.ReplaceEdgesFromSource(ctx, "", []Edge{{From: 1, To: 2}}); err == nil {
		t.Error("ReplaceEdgesFromSource with no source = nil, want an error")
	}

	if _, err := db.ReplaceEdgesFromSource(ctx, "next_tanks", []Edge{
		{From: 0, To: 2}, {From: 1, To: 0}, {From: 1, To: 2, XPCost: 10},
	}); err != nil {
		t.Fatalf("ReplaceEdgesFromSource: %v", err)
	}
	if n, _ := db.EdgeCount(ctx, "next_tanks"); n != 1 {
		t.Errorf("stored %d edges, want only the complete one", n)
	}
}

func TestVehicleLookupsReportNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.VehicleByID(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("VehicleByID = %v, want ErrNotFound", err)
	}
}

// TestVehicleByNameFoldsDiacritics: the Czech and French lines are full of
// accents nobody types.
func TestVehicleByNameFoldsDiacritics(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if _, err := db.UpsertVehicles(ctx, []Vehicle{
		{TankID: 6769, Name: "Vz. 71 Tesák", ShortName: "Tesák", Tier: 10, Type: "lightTank", SyncedAt: base},
		{TankID: 6257, Name: "LPT-67 Šelma", ShortName: "Šelma", Tier: 9, Type: "lightTank", SyncedAt: base},
		{TankID: 3649, Name: "Bat.-Châtillon 25 t", ShortName: "B-C 25 t", Tier: 10, Type: "mediumTank", SyncedAt: base},
	}); err != nil {
		t.Fatalf("UpsertVehicles: %v", err)
	}
	for query, want := range map[string]int{
		"Tesak": 6769, "tesák": 6769, "vz 71 tesak": 6769,
		"Selma": 6257, "Bat.-Chatillon 25 t": 3649,
	} {
		got, err := db.VehicleByName(ctx, query)
		if err != nil || got.TankID != want {
			t.Errorf("VehicleByName(%q) = (%d, %v), want %d", query, got.TankID, err, want)
		}
	}
}
