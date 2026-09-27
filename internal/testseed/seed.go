// Package testseed builds the small, fully known account that the query and
// brief tests are pinned against. It lives outside _test files so both
// packages can share it; nothing else imports it.
//
// The account has three tank-statistics snapshots - 40 days, 10 days and one
// hour before Now - so lifetime, a 30-day window with a baseline and a 60-day
// window without one each take their own path.
package testseed

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// Now is the fixed clock every seeded timestamp is relative to.
var Now = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

// Fixture is a seeded cache and its overlay.
type Fixture struct {
	DB          *store.DB
	OverlayPath string
}

// Vehicle ids. Real where the vehicle is real; the unresolved garage id is
// one the encyclopedia does not describe, as on the live account.
const (
	Blesk      = 4721  // tier VIII light, owned
	Selma      = 6257  // tier IX light, next research from the Blesk
	Tesak      = 6769  // tier X light, goal target via the Šelma
	AMX1390    = 4929  // tier IX light, owned, lots of battles
	AMX13105   = 17217 // tier X light, next research from the AMX 13 90
	Kranvagn   = 2433  // tier X heavy, researched not bought
	Obj261     = 8705  // tier X SPG, owned
	ConquerGC  = 9999  // tier X SPG, next research from the Obj. 261 in this dataset
	Leox       = 68673 // tier IX medium, owned, no expected values
	Unresolved = 41553
)

// Seed builds the account in a temporary directory. The database is closed
// when the test ends.
func Seed(t testing.TB) Fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	db, err := store.Open(ctx, filepath.Join(dir, "wotctx.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	must := func(_ int, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	synced := Now.Add(-2 * time.Hour)
	v := func(id int, name, short string, tier int, typ, nation string, price int) store.Vehicle {
		return store.Vehicle{TankID: id, Name: name, ShortName: short, Tier: tier, Type: typ,
			Nation: nation, PriceCredit: price, SyncedAt: synced}
	}
	must(db.UpsertVehicles(ctx, []store.Vehicle{
		v(Blesk, "Vz. 64 Blesk", "Blesk", 8, "lightTank", "czech", 2500000),
		v(Selma, "LPT-67 Šelma", "Šelma", 9, "lightTank", "czech", 3500000),
		v(Tesak, "Vz. 71 Tesák", "Tesák", 10, "lightTank", "czech", 6100000),
		v(AMX1390, "AMX 13 90", "AMX 13 90", 9, "lightTank", "france", 3500000),
		v(AMX13105, "AMX 13 105", "AMX 13 105", 10, "lightTank", "france", 6100000),
		v(Kranvagn, "Kranvagn", "Kranvagn", 10, "heavyTank", "sweden", 6100000),
		v(Obj261, "Object 261", "Obj. 261", 10, "SPG", "ussr", 6100000),
		v(ConquerGC, "Conqueror Gun Carriage", "Conqueror GC", 10, "SPG", "uk", 6100000),
		v(Leox, "Leox", "Leox", 9, "mediumTank", "italy", 3700000),
	}))
	edges := []store.Edge{
		{From: Blesk, To: Selma, XPCost: 185490},
		{From: Selma, To: Tesak, XPCost: 242560},
		{From: AMX1390, To: AMX13105, XPCost: 261000},
		{From: Obj261, To: ConquerGC, XPCost: 250000},
	}
	must(db.ReplaceEdgesFromSource(ctx, wg.EdgeSourceNextTanks, edges))
	must(db.ReplaceEdgesFromSource(ctx, wg.EdgeSourcePricesXP, edges)) // the API reports each twice

	put := func(endpoint string, at time.Time) int64 {
		id, err := db.PutSnapshot(ctx, store.Snapshot{Source: "wg", Endpoint: endpoint, RequestedAt: at, HTTPStatus: 200, Raw: []byte(`{}`)})
		if err != nil {
			t.Fatalf("PutSnapshot: %v", err)
		}
		return id
	}
	put("encyclopedia/vehicles", synced)

	acct := put("account/info", Now.Add(-time.Hour))
	if err := db.PutAccountState(ctx, store.AccountState{
		SnapshotID: acct, ObservedAt: Now.Add(-time.Hour),
		Credits: 4000000, Gold: 3373, Bonds: 11561, FreeXP: 41294,
		IsPremium: false, LastBattleTime: Now.Add(-3 * time.Hour),
	}, []int{Blesk, AMX1390, Obj261, Leox, Unresolved}, []store.Booster{
		{Kind: "121002", Count: 104, State: "INACTIVE"},
		{Kind: "12006", Count: 1, State: "ACTIVE", ExpiresAt: Now.Add(30 * time.Minute)},
	}); err != nil {
		t.Fatalf("PutAccountState: %v", err)
	}
	put("account/tanks", Now.Add(-time.Hour))

	// Personal missions, shaped like the live account on 2026-09-18: in the
	// T 55A operation HT-10 is done while HT-9 is still open, so completion is
	// not in id order; status 301 belongs to a campaign the encyclopedia does
	// not describe.
	put("encyclopedia/personalmissions", synced)
	pm := func(id, op int, opName string, set int, name, class, primary string) store.PersonalMission {
		return store.PersonalMission{MissionID: id, CampaignID: 1, Campaign: "Long-Awaited Backup",
			OperationID: op, Operation: opName, SetID: set, Name: name, Class: class,
			MinTier: 6, MaxTier: 10, Primary: primary, Secondary: "Survive the battle"}
	}
	must(db.ReplacePersonalMissions(ctx, []store.PersonalMission{
		pm(151, 3, "T 55A", 1, "LT-1: For Victory!", "lightTank", "Be among the top 3 players on your team by experience earned"),
		pm(165, 3, "T 55A", 1, "LT-15: The Aggressive Recon Specialist", "lightTank", "Damage caused (including damage caused with your assistance) must total 7000 HP."),
		pm(174, 3, "T 55A", 2, "HT-9: A Crushing Blow", "heavyTank", "Destroy an enemy vehicle by ramming"),
		pm(175, 3, "T 55A", 2, "HT-10: Hold the Line", "heavyTank", "Block 3000 HP of damage"),
		pm(226, 4, "Object 260", 1, "LT-1: For Victory!", "lightTank", "Finish the battle as the top player on your team by experience earned"),
	}, synced))
	must(db.PutMissionStatuses(ctx, acct, map[int]string{
		151: "ALL_REWARDS_GOTTEN", 175: "MAIN_REWARD_GOTTEN", 301: "ALL_REWARDS_GOTTEN",
	}))

	// row builds a random-mode row with every total WN8 and the rollups need.
	row := func(snap int64, id, battles, wins, dmg, frags, spots, def, surv, xp, radio, track int) store.TankStats {
		return store.TankStats{SnapshotID: snap, TankID: id, Mode: "random", Battles: battles, Wins: wins,
			DamageDealt: dmg, Frags: frags, Spotted: spots, DroppedCapturePoints: def, Survived: surv,
			XP: xp, RadioAssistedDamage: radio, TrackAssistedDamage: track, MarkOfMastery: 2}
	}
	s0 := put("tanks/stats", Now.Add(-40*24*time.Hour))
	s1 := put("tanks/stats", Now.Add(-10*24*time.Hour))
	s2 := put("tanks/stats", Now.Add(-time.Hour))
	must(db.PutTankStats(ctx, []store.TankStats{
		row(s0, AMX1390, 500, 245, 560000, 470, 1130, 300, 150, 389000, 372000, 26800),
		row(s0, Blesk, 40, 20, 60000, 30, 70, 20, 12, 25000, 30000, 2000),
		row(s0, Obj261, 100, 52, 90000, 70, 5, 10, 45, 50000, 1000, 6000),
		row(s1, AMX1390, 520, 256, 582000, 490, 1175, 310, 156, 404500, 387000, 27900),
		row(s1, Blesk, 50, 25, 75000, 38, 90, 25, 15, 31000, 37500, 2500),
		row(s1, Obj261, 100, 52, 90000, 70, 5, 10, 45, 50000, 1000, 6000),
		row(s2, AMX1390, 529, 259, 592480, 498, 1196, 316, 158, 411560, 393853, 28397),
		row(s2, Blesk, 54, 27, 81000, 41, 97, 27, 16, 33500, 40500, 2700),
		row(s2, Obj261, 100, 52, 90000, 70, 5, 10, 45, 50000, 1000, 6000),
		row(s2, Leox, 2, 2, 3820, 1, 2, 0, 1, 2083, 134, 455),
		{SnapshotID: s2, TankID: AMX1390, Mode: "all", Battles: 540, Wins: 264,
			AvgDamageAssisted: 790.4, AvgDamageBlocked: 18.1},
	}))

	xvm, err := db.PutSnapshot(ctx, store.Snapshot{Source: "xvm", Endpoint: "wn8exp", RequestedAt: Now.Add(-30 * time.Minute), HTTPStatus: 200, Raw: []byte(`{}`)})
	if err != nil || xvm == 0 {
		t.Fatalf("PutSnapshot xvm: %v", err)
	}
	must(db.ReplaceWN8Expected(ctx, "2026-09-12", Now.Add(-30*time.Minute), []store.WN8Expected{
		{TankID: AMX1390, Damage: 957.199, Frags: 0.759, Spot: 2.391, Def: 0.763, WinRate: 51.246},
		{TankID: Blesk, Damage: 1300, Frags: 0.8, Spot: 1.9, Def: 0.6, WinRate: 50.5},
		{TankID: Obj261, Damage: 1100, Frags: 0.7, Spot: 0.1, Def: 0.2, WinRate: 50.8},
		// No entry for the Leox, as XVM's pre-split file lacked it.
	}))

	run, err := db.BeginSyncRun(ctx, Now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("BeginSyncRun: %v", err)
	}
	if err := db.FinishSyncRun(ctx, run, Now.Add(-time.Hour), true,
		[]string{"wg:account/achievements: 504 SOURCE_NOT_AVAILABLE"}); err != nil {
		t.Fatalf("FinishSyncRun: %v", err)
	}

	// updated_at is a day before Now and so, necessarily, in the past: the
	// overlay warns on a future stamp against the real clock.
	overlayPath := filepath.Join(dir, "wot-overlay.yaml")
	if err := os.WriteFile(overlayPath, []byte(`version: 1
updated_at: 2026-09-17T10:00:00Z
premium: {premium_account: true, wot_plus: true}
researched_not_bought: [Kranvagn]
xp_goals:
  - {target: Selma, via: Blesk, xp_required: 185490, xp_banked: 166295}
  - {target: Tesak, via: Selma, xp_required: 242560, xp_banked: 0}
preferences:
  avoid_classes: [SPG]
  class_rank: {mediumTank: 1, lightTank: 2, AT-SPG: 3, heavyTank: 3}
  free_xp_max_tier: 8
  improvement_focus: [lightTank]
constraints:
  credit_buffer: 500000
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return Fixture{DB: db, OverlayPath: overlayPath}
}
