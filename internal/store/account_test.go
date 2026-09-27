package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAccountStateRoundTrips(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	id := putSnapshotAt(t, db, "account/info", base)

	want := AccountState{
		SnapshotID: id, ObservedAt: base,
		Credits: 1202384, Gold: 3373, Bonds: 11556, FreeXP: 41172,
		IsPremium: true, PremiumExpiresAt: base.Add(30 * 24 * time.Hour),
		GlobalRating: 7767, LastBattleTime: base.Add(-2 * time.Hour),
		BattleLifeTime: 4715100 * time.Second,
	}
	if err := db.PutAccountState(ctx, want, []int{4929, 2417}, []Booster{
		{Kind: "credits", Count: 3, State: "INACTIVE", ExpiresAt: base.Add(time.Hour)},
	}); err != nil {
		t.Fatalf("PutAccountState: %v", err)
	}

	got, err := db.LatestAccountState(ctx)
	if err != nil {
		t.Fatalf("LatestAccountState: %v", err)
	}
	if got.Credits != want.Credits || got.Gold != want.Gold || got.Bonds != want.Bonds || got.FreeXP != want.FreeXP {
		t.Errorf("resources did not round-trip: %+v", got)
	}
	if !got.IsPremium {
		t.Error("is_premium did not round-trip")
	}
	if !got.PremiumExpiresAt.Equal(want.PremiumExpiresAt) {
		t.Errorf("premium expiry = %s, want %s", got.PremiumExpiresAt, want.PremiumExpiresAt)
	}
	if got.BattleLifeTime != want.BattleLifeTime {
		t.Errorf("battle life time = %s, want %s", got.BattleLifeTime, want.BattleLifeTime)
	}

	garage, err := db.LatestGarage(ctx)
	if err != nil {
		t.Fatalf("LatestGarage: %v", err)
	}
	if len(garage) != 2 || garage[0] != 2417 {
		t.Errorf("garage = %v, want [2417 4929] sorted", garage)
	}

	boosters, err := db.LatestBoosters(ctx)
	if err != nil {
		t.Fatalf("LatestBoosters: %v", err)
	}
	if len(boosters) != 1 || boosters[0].Count != 3 {
		t.Errorf("boosters = %+v", boosters)
	}
}

// TestUnknownTimesAreStoredAsNull: the zero instant would read back as the year
// 1, which looks like data. NULL reads back as "not known".
func TestUnknownTimesAreStoredAsNull(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	id := putSnapshotAt(t, db, "account/info", base)

	if err := db.PutAccountState(ctx, AccountState{
		SnapshotID: id, ObservedAt: base, Credits: 100,
		// No premium expiry and no last battle: a brand-new account.
	}, nil, []Booster{{Kind: "crew_xp", Count: 1, State: "ACTIVE"}}); err != nil {
		t.Fatalf("PutAccountState: %v", err)
	}

	got, err := db.LatestAccountState(ctx)
	if err != nil {
		t.Fatalf("LatestAccountState: %v", err)
	}
	if !got.PremiumExpiresAt.IsZero() {
		t.Errorf("premium expiry = %s, want zero", got.PremiumExpiresAt)
	}
	if !got.LastBattleTime.IsZero() {
		t.Errorf("last battle = %s, want zero", got.LastBattleTime)
	}

	boosters, err := db.LatestBoosters(ctx)
	if err != nil {
		t.Fatalf("LatestBoosters: %v", err)
	}
	if len(boosters) != 1 || !boosters[0].ExpiresAt.IsZero() {
		t.Errorf("booster expiry = %+v, want zero", boosters)
	}
}

// TestAccountStateHistoryIsKept is what makes resource trends a by-product of
// syncing rather than a separate feature.
func TestAccountStateHistoryIsKept(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	for i, at := range []time.Time{base.Add(-48 * time.Hour), base.Add(-24 * time.Hour), base} {
		id := putSnapshotAt(t, db, "account/info", at)
		if err := db.PutAccountState(ctx, AccountState{
			SnapshotID: id, ObservedAt: at, Credits: 1000000 + i*500000, FreeXP: 40000 + i*1000,
		}, nil, nil); err != nil {
			t.Fatalf("PutAccountState: %v", err)
		}
	}

	latest, err := db.LatestAccountState(ctx)
	if err != nil {
		t.Fatalf("LatestAccountState: %v", err)
	}
	if latest.Credits != 2000000 {
		t.Errorf("latest credits = %d, want 2000000", latest.Credits)
	}

	// A day-old baseline supports "credits are up 500k since yesterday".
	before, err := db.AccountStateAtOrBefore(ctx, base.Add(-20*time.Hour))
	if err != nil {
		t.Fatalf("AccountStateAtOrBefore: %v", err)
	}
	if before.Credits != 1500000 {
		t.Errorf("baseline credits = %d, want 1500000", before.Credits)
	}

	// And with nothing old enough, it says so rather than using the oldest.
	if _, err := db.AccountStateAtOrBefore(ctx, base.Add(-100*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("AccountStateAtOrBefore = %v, want ErrNotFound", err)
	}
}

func TestGarageSplitsIdentifiedFromUnresolved(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedVehicles(t, db) // 19281, 26705, 22017, 3329

	id := putSnapshotAt(t, db, "account/info", base)
	if err := db.PutAccountState(ctx,
		AccountState{SnapshotID: id, ObservedAt: base},
		[]int{19281, 26705, 41553, 71457}, nil); err != nil {
		t.Fatalf("PutAccountState: %v", err)
	}

	got, err := db.Garage(ctx)
	if err != nil {
		t.Fatalf("Garage: %v", err)
	}

	// Identified is the truthful garage size: it matched a counted garage,
	// while the id total did not.
	if len(got.Identified) != 2 || got.Identified[0] != 19281 || got.Identified[1] != 26705 {
		t.Errorf("Identified = %v, want [19281 26705]", got.Identified)
	}
	// Unresolved is disclosed rather than dropped, but must not be counted as
	// owned - the client does not show these.
	if len(got.Unresolved) != 2 || got.Unresolved[0] != 41553 || got.Unresolved[1] != 71457 {
		t.Errorf("Unresolved = %v, want [41553 71457]", got.Unresolved)
	}
	if got.Total() != 4 {
		t.Errorf("Total = %d, want the 4 ids the API reported", got.Total())
	}

	// The raw accessor still returns everything the API said, for provenance.
	raw, err := db.LatestGarage(ctx)
	if err != nil {
		t.Fatalf("LatestGarage: %v", err)
	}
	if len(raw) != 4 {
		t.Errorf("LatestGarage = %v, want all four reported ids", raw)
	}
}

func TestAccountLookupsReportNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.LatestAccountState(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestAccountState = %v, want ErrNotFound", err)
	}
	if _, err := db.LatestGarage(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestGarage = %v, want ErrNotFound", err)
	}
	if _, err := db.LatestBoosters(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestBoosters = %v, want ErrNotFound", err)
	}
	if _, err := db.Garage(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("Garage = %v, want ErrNotFound", err)
	}
}

func TestPutAccountStateNeedsASnapshot(t *testing.T) {
	err := openTestDB(t).PutAccountState(context.Background(), AccountState{ObservedAt: base}, nil, nil)
	if err == nil {
		t.Error("PutAccountState with no snapshot id = nil, want an error")
	}
}

func TestAchievementsJoinToDefinitions(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.UpsertAchievementDefs(ctx, []AchievementDef{{
		Code: "warrior", Name: "Top Gun", Description: "Destroy the most vehicles.",
		Condition: "Destroy 6 or more vehicles.", Section: "battle", SyncedAt: base,
	}}); err != nil {
		t.Fatalf("UpsertAchievementDefs: %v", err)
	}
	if n, _ := db.AchievementDefCount(ctx); n != 1 {
		t.Errorf("definition count = %d, want 1", n)
	}

	id := putSnapshotAt(t, db, "account/achievements", base)
	if _, err := db.PutAccountAchievements(ctx, id, []AchievementCount{
		{Kind: "achievement", Code: "warrior", Count: 12},
		{Kind: "achievement", Code: "sniper", Count: 48},
		{Kind: "max_series", Code: "maxKillingSeries", Count: 6},
	}); err != nil {
		t.Fatalf("PutAccountAchievements: %v", err)
	}

	got, err := db.LatestAccountAchievements(ctx)
	if err != nil {
		t.Fatalf("LatestAccountAchievements: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d achievements, want 3", len(got))
	}

	byCode := map[string]AchievementWithDef{}
	for _, a := range got {
		byCode[a.Code] = a
	}
	if !byCode["warrior"].HasDef || byCode["warrior"].Def.Name != "Top Gun" {
		t.Errorf("warrior = %+v, want the definition attached", byCode["warrior"])
	}
	// A code with no definition is reported anyway: the count is real even if
	// the encyclopedia does not describe it.
	if byCode["sniper"].HasDef {
		t.Error("sniper reports a definition it does not have")
	}
	if byCode["sniper"].Count != 48 {
		t.Errorf("sniper count = %d, want 48", byCode["sniper"].Count)
	}
}

func TestTankAchievementsAreScopedToTheTank(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	id := putSnapshotAt(t, db, "tanks/achievements", base)
	if _, err := db.PutTankAchievements(ctx, id, []AchievementCount{
		{TankID: 4929, Kind: "achievement", Code: "warrior", Count: 2},
		{TankID: 4929, Kind: "achievement", Code: "sniper", Count: 9},
		{TankID: 2417, Kind: "achievement", Code: "sniper", Count: 3},
		{TankID: 0, Kind: "achievement", Code: "orphan", Count: 1}, // skipped
	}); err != nil {
		t.Fatalf("PutTankAchievements: %v", err)
	}

	amx, err := db.LatestTankAchievements(ctx, 4929)
	if err != nil {
		t.Fatalf("LatestTankAchievements: %v", err)
	}
	if len(amx) != 2 {
		t.Errorf("got %d achievements for 4929, want 2: %+v", len(amx), amx)
	}
	for _, a := range amx {
		if a.TankID != 4929 {
			t.Errorf("achievement %s has tank %d", a.Code, a.TankID)
		}
	}

	tvp, err := db.LatestTankAchievements(ctx, 2417)
	if err != nil {
		t.Fatalf("LatestTankAchievements: %v", err)
	}
	if len(tvp) != 1 {
		t.Errorf("got %d achievements for 2417, want 1", len(tvp))
	}
}

func TestAchievementWritesNeedASnapshot(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.PutAccountAchievements(ctx, 0, []AchievementCount{{Kind: "a", Code: "b", Count: 1}}); err == nil {
		t.Error("PutAccountAchievements with no snapshot = nil, want an error")
	}
	if _, err := db.PutTankAchievements(ctx, 0, []AchievementCount{{TankID: 1, Kind: "a", Code: "b", Count: 1}}); err == nil {
		t.Error("PutTankAchievements with no snapshot = nil, want an error")
	}
}
