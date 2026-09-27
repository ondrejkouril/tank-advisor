package syncer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// modFixture copies the real, cut-down dump to where a syncer will read it.
func modFixture(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "mod", "garage.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "mod", "garage.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// modSyncer reads only the dump: no Wargaming client, no XVM.
func modSyncer(db *store.DB, dump string) *Syncer {
	return &Syncer{
		DB: db, Config: config.Default(), AccountID: testAccountID,
		Now: func() time.Time { return testNow }, ModDump: dump,
	}
}

func TestSyncModStoresTheDump(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	report, err := modSyncer(db, modFixture(t)).Sync(ctx, OnlyMod)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(report.Synced) != 1 || report.Synced[0] != "mod:garage" || len(report.Caveats) != 0 {
		t.Fatalf("report = %+v", report)
	}

	acct, err := db.LatestModAccount(ctx)
	if err != nil {
		t.Fatalf("LatestModAccount: %v", err)
	}
	if want := time.Date(2026, 9, 25, 20, 38, 53, 0, time.UTC); !acct.CapturedAt.Equal(want) {
		t.Errorf("CapturedAt = %s", acct.CapturedAt)
	}
	if *acct.FreeXP != 253317 || !*acct.WotPlus || !*acct.Premium || acct.GameVersion != "2.4.0.1" {
		t.Errorf("account = %+v", acct)
	}
	snap, err := db.LatestSnapshot(ctx, "mod", "garage")
	if err != nil || !snap.RequestedAt.Equal(acct.CapturedAt) {
		t.Errorf("snapshot requested_at = %v (%v), want the capture time", snap.RequestedAt, err)
	}

	vehicles, err := db.ModVehicles(ctx, acct.SnapshotID)
	if err != nil || len(vehicles) != 5 {
		t.Fatalf("ModVehicles = %d, %v", len(vehicles), err)
	}
	byID := map[int]store.ModVehicle{}
	for _, v := range vehicles {
		byID[v.TankID] = v
	}
	if *byID[6257].XP != 90237 || byID[6257].Rented {
		t.Errorf("Šelma = %+v", byID[6257])
	}
	if !byID[34849].Rented {
		t.Error("the TS-54 rental is not marked rented")
	}

	marks, _ := db.ModMarks(ctx, acct.SnapshotID)
	if m := marks[16897]; m.Marks != 1 || m.Percent != 75.15 {
		t.Errorf("Object 140 marks = %+v", m)
	}

	loadout, err := db.ModLoadout(ctx, acct.SnapshotID, 2417)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, s := range loadout {
		kinds = append(kinds, s.Kind)
	}
	// Three pieces of equipment, three shell types, three consumables, and
	// no directive: the TVP as the owner checked it in the client.
	if got := strings.Join(kinds, " "); got != "equipment equipment equipment shell shell shell consumable consumable consumable" {
		t.Errorf("TVP loadout kinds = %s", got)
	}
	if s := loadout[3]; s.ShellKind != "ARMOR_PIERCING_CR" || *s.Count != 28 {
		t.Errorf("first shell = %+v", s)
	}

	crew, _ := db.ModCrew(ctx, acct.SnapshotID, 6257)
	if len(crew) != 3 || crew[0].Role != "commander" || len(crew[0].Skills) != 5 {
		t.Errorf("Šelma crew = %+v", crew)
	}
}

// The dump is taken only when the client has written a newer one.
func TestSyncModSkipsAnUnchangedDump(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := modSyncer(db, modFixture(t))
	if _, err := s.Sync(ctx, OnlyMod); err != nil {
		t.Fatal(err)
	}
	report, err := s.Sync(ctx, OnlyMod)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Synced) != 0 || len(report.Skipped) != 1 {
		t.Errorf("second sync: %+v, want the dump skipped", report)
	}
}

// Without the mod, a sync is what it was before phase 4.
func TestSyncWithoutADumpIsQuiet(t *testing.T) {
	db := openTestDB(t)
	report, err := modSyncer(db, filepath.Join(t.TempDir(), "absent.json")).Sync(context.Background(), OnlyMod)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Synced)+len(report.Skipped)+len(report.Caveats) != 0 {
		t.Errorf("report = %+v, want nothing at all", report)
	}
}

func TestSyncModRefusesAnotherAccount(t *testing.T) {
	db := openTestDB(t)
	s := modSyncer(db, modFixture(t))
	s.AccountID = 1
	report, err := s.Sync(context.Background(), OnlyMod)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Caveats) != 1 || !strings.Contains(report.Caveats[0], "not 1") {
		t.Errorf("caveats = %v", report.Caveats)
	}
	if _, err := db.LatestModAccount(context.Background()); err == nil {
		t.Error("another account's dump was stored")
	}
}

func TestSyncModCarriesTheModsErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garage.json")
	os.WriteFile(path, []byte(`{"schema": 1, "captured_at": "2026-09-25T20:38:53Z", "account_id": 512345678,
		"vehicles": [], "errors": ["vehicle 2 crew: AttributeError()"]}`), 0o644)
	report, err := modSyncer(openTestDB(t), path).Sync(context.Background(), OnlyMod)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Caveats) != 1 || !strings.Contains(report.Caveats[0], "vehicle 2 crew") {
		t.Errorf("caveats = %v", report.Caveats)
	}
}

// --only mod makes no request; --only wg does not read the dump.
func TestOnlySeparatesTheModFromTheAPI(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	log := newRequestLog()
	s := newTestSyncer(t, db, allSources(t, log, nil))
	s.ModDump = modFixture(t)

	if _, err := s.Sync(ctx, OnlyMod); err != nil {
		t.Fatal(err)
	}
	if n := log.count("account/info"); n != 0 {
		t.Errorf("--only mod made %d Wargaming request(s)", n)
	}

	db2 := openTestDB(t)
	s2 := newTestSyncer(t, db2, allSources(t, nil, nil))
	s2.ModDump = s.ModDump
	report, err := s2.Sync(ctx, OnlyWG)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range append(report.Synced, report.Skipped...) {
		if k == "mod:garage" {
			t.Error("--only wg read the mod dump")
		}
	}
}
