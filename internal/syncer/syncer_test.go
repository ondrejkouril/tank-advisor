package syncer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

var testNow = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

// testAccountID matches the account id baked into the fixtures.
const testAccountID = 512345678

func fixture(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "wg", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return raw
}

func openTestDB(t *testing.T) *store.DB {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "wotctx.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newTestSyncer wires a syncer to a handler with all waiting disabled.
func newTestSyncer(t *testing.T, db *store.DB, handler http.HandlerFunc) *Syncer {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// The client stamps a snapshot with when the request was actually made, so
	// it needs the same frozen clock as the syncer's policy decisions.
	client, err := wg.New("eu", "deadbeefcafebabe0123456789abcdef",
		wg.WithBaseURL(server.URL),
		wg.WithSleeper(func(context.Context, time.Duration) error { return nil }),
		wg.WithRequestsPerSecond(0),
		wg.WithClock(func() time.Time { return testNow }),
	)
	if err != nil {
		t.Fatalf("wg.New: %v", err)
	}

	return &Syncer{
		DB:          db,
		Config:      config.Default(),
		WG:          client,
		AccountID:   testAccountID,
		AccessToken: "test-token",
		Now:         func() time.Time { return testNow },
	}
}

// endpointOf recovers the endpoint name from a request path, so handlers can
// route the way the client addresses them.
func endpointOf(r *http.Request) string {
	return strings.TrimPrefix(strings.Trim(r.URL.Path, "/"), "wot/")
}

// requestLog counts requests per endpoint, so a test can assert that one source
// was skipped without caring what the others did.
type requestLog struct {
	mu     sync.Mutex
	counts map[string]int
}

func newRequestLog() *requestLog {
	return &requestLog{counts: map[string]int{}}
}

func (l *requestLog) record(endpoint string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.counts[endpoint]++
}

func (l *requestLog) count(endpoint string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[endpoint]
}

// fixtureFor maps each synced endpoint to the fixture that answers it.
func fixtureFor(endpoint string) string {
	switch endpoint {
	case "account/info":
		return "account-info.json"
	case "tanks/stats":
		return "tanks-stats.json"
	case "account/tanks":
		return "account-tanks.json"
	case "account/achievements":
		return "account-achievements.json"
	case "tanks/achievements":
		return "tanks-achievements.json"
	case "encyclopedia/personalmissions":
		return "encyclopedia-personalmissions.json"
	case "encyclopedia/achievements":
		return "encyclopedia-achievements.json"
	default:
		return ""
	}
}

// allSources answers every Wargaming endpoint wotctx syncs, from fixtures.
// overrides replaces the handler for named endpoints, which is how a test makes
// exactly one source fail.
func allSources(t *testing.T, log *requestLog, overrides map[string]http.HandlerFunc) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		endpoint := endpointOf(r)
		if log != nil {
			log.record(endpoint)
		}
		if handler, ok := overrides[endpoint]; ok {
			handler(w, r)
			return
		}
		if endpoint == "encyclopedia/vehicles" {
			singlePage(t)(w, r)
			return
		}
		if name := fixtureFor(endpoint); name != "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write(fixture(t, name))
			return
		}
		t.Errorf("unexpected request to %q", endpoint)
		w.Write([]byte(`{"status":"error","error":{"message":"METHOD_NOT_FOUND","code":404,"field":"","value":""}}`))
	}
}

// serveFixture answers with one fixture at HTTP 200, which is how Wargaming
// replies even to failures.
func serveFixture(t *testing.T, name string) http.HandlerFunc {
	body := fixture(t, name)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}

// singlePage serves the vehicle fixture on page 1 and an empty page after, so
// paging terminates without eleven fixtures.
//
// The account-scoped fixture is used here rather than the generic one, so that
// the vehicles, the garage and the per-tank statistics all describe the same
// account. Without that the "unidentified garage vehicle" test cannot tell a
// deliberately absent vehicle from a fixture that simply forgot one.
func singlePage(t *testing.T) http.HandlerFunc {
	body := fixture(t, "encyclopedia-vehicles-account.json")
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_no") == "1" {
			w.Write(body)
			return
		}
		w.Write([]byte(`{"status":"ok","meta":{"count":0,"page_total":1,"total":0,"page":2},"data":{}}`))
	}
}

func TestSyncWGStoresEverySource(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, nil))

	report, err := s.SyncWG(ctx)
	if err != nil {
		t.Fatalf("SyncWG: %v", err)
	}
	if len(report.Caveats) != 0 {
		t.Fatalf("caveats = %v, want none", report.Caveats)
	}

	for _, key := range []string{
		"wg:encyclopedia/vehicles",
		"wg:encyclopedia/achievements",
		"wg:account/info",
		"wg:tanks/stats",
		"wg:account/achievements",
		"wg:tanks/achievements",
	} {
		if !contains(report.Synced, key) {
			t.Errorf("%s was not synced; Synced = %v", key, report.Synced)
		}
	}

	if n, _ := db.VehicleCount(ctx); n != 4 {
		t.Errorf("stored %d vehicles, want 4", n)
	}
	if n, _ := db.AchievementDefCount(ctx); n != 3 {
		t.Errorf("stored %d achievement definitions, want 3", n)
	}
}

func TestSyncAccountInfoStoresResourcesGarageAndBoosters(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, nil))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	state, err := db.LatestAccountState(ctx)
	if err != nil {
		t.Fatalf("LatestAccountState: %v", err)
	}
	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"credits", state.Credits, 1202384},
		{"gold", state.Gold, 3373},
		{"bonds", state.Bonds, 11556},
		{"free xp", state.FreeXP, 41172},
		{"global rating", state.GlobalRating, 7767},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	if state.IsPremium {
		t.Error("is_premium = true, want false from the fixture")
	}
	if state.BattleLifeTime != 4715100*time.Second {
		t.Errorf("battle life time = %s", state.BattleLifeTime)
	}

	garage, err := db.LatestGarage(ctx)
	if err != nil {
		t.Fatalf("LatestGarage: %v", err)
	}
	if len(garage) != 5 {
		t.Errorf("garage = %v, want 5 entries", garage)
	}

	boosters, err := db.LatestBoosters(ctx)
	if err != nil {
		t.Fatalf("LatestBoosters: %v", err)
	}
	if len(boosters) != 2 {
		t.Fatalf("boosters = %+v, want 2", boosters)
	}
	// A reserve with no expiry must read as unset rather than as the epoch.
	for _, b := range boosters {
		if b.Kind == "crew_xp" && !b.ExpiresAt.IsZero() {
			t.Errorf("crew_xp expiry = %s, want zero for an absent expiration_time", b.ExpiresAt)
		}
	}
}

// TestUnresolvedGarageIdsAreSeparated covers a measured property of the live
// account: private.garage returns ids the encyclopedia does not describe and
// the client does not show. They are disclosed, but not counted as owned.
func TestUnresolvedGarageIdsAreSeparated(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, nil))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	// The fixture garage holds five ids, of which 99999 has no vehicle row.
	garage, err := db.Garage(ctx)
	if err != nil {
		t.Fatalf("Garage: %v", err)
	}
	if garage.Total() != 5 {
		t.Errorf("Total = %d, want the 5 ids the API reported", garage.Total())
	}
	if len(garage.Identified) != 4 {
		t.Errorf("Identified = %v, want the 4 described vehicles", garage.Identified)
	}
	if len(garage.Unresolved) != 1 || garage.Unresolved[0] != 99999 {
		t.Errorf("Unresolved = %v, want [99999]", garage.Unresolved)
	}
}

func TestSyncTankStatsMergesMasteryAndSkipsUnplayedModes(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, nil))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	stats, err := db.LatestTankStats(ctx, wg.ModeRandom)
	if err != nil {
		t.Fatalf("LatestTankStats: %v", err)
	}
	// The fixture has three tanks but one has zero battles, which is omitted so
	// that "never played" and "played and scored nothing" stay distinct.
	if len(stats) != 2 {
		t.Fatalf("stored %d random rows, want 2: %+v", len(stats), stats)
	}

	byTank := map[int]store.TankStats{}
	for _, s := range stats {
		byTank[s.TankID] = s
	}

	amx := byTank[4929]
	if amx.Battles != 529 {
		t.Errorf("AMX 13 90 battles = %d, want 529", amx.Battles)
	}
	if got := amx.DPG(); got < 1119 || got > 1121 {
		t.Errorf("AMX 13 90 DPG = %.1f, want about 1120", got)
	}
	// Mastery comes from account/tanks, a separate endpoint, and must be merged
	// onto the statistics row.
	if amx.MarkOfMastery != 4 {
		t.Errorf("AMX 13 90 mastery = %d, want 4 (Ace Tanker)", amx.MarkOfMastery)
	}
	if byTank[2417].MarkOfMastery != 3 {
		t.Errorf("TVP mastery = %d, want 3", byTank[2417].MarkOfMastery)
	}
}

// TestAssistIsOnlyInTheAllMode pins the measured API behaviour: the random
// block carries no assist fields, so a zero there means "not provided". An
// answer that presented it as a random-battles assist figure would understate
// every scout the player owns.
func TestAssistIsOnlyInTheAllMode(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, nil))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	all, err := db.LatestTankStatsFor(ctx, 4929, wg.ModeAll)
	if err != nil {
		t.Fatalf("LatestTankStatsFor(all): %v", err)
	}
	if all.AvgDamageAssisted == 0 {
		t.Error("the all mode carries no assist figure, but the fixture provides one")
	}
	if all.AvgDamageAssistedRadio == 0 || all.AvgDamageAssistedTrack == 0 {
		t.Error("the assist breakdown did not survive into the all mode")
	}

	random, err := db.LatestTankStatsFor(ctx, 4929, wg.ModeRandom)
	if err != nil {
		t.Fatalf("LatestTankStatsFor(random): %v", err)
	}
	if random.AvgDamageAssisted != 0 {
		t.Errorf("random assist = %.1f; the API does not provide one, so this is invented",
			random.AvgDamageAssisted)
	}
}

func TestSyncAchievementsJoinToTheirDefinitions(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, nil))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	account, err := db.LatestAccountAchievements(ctx)
	if err != nil {
		t.Fatalf("LatestAccountAchievements: %v", err)
	}
	if len(account) != 9 {
		t.Errorf("stored %d account achievements, want 9: %+v", len(account), account)
	}

	byCode := map[string]store.AchievementWithDef{}
	for _, a := range account {
		byCode[a.Code] = a
	}

	// A code with a definition reads as a name and a condition, which is what
	// makes "which medals am I close to" answerable.
	warrior := byCode["warrior"]
	if warrior.Count != 12 {
		t.Errorf("warrior count = %d, want 12", warrior.Count)
	}
	if !warrior.HasDef || warrior.Def.Name != "Top Gun" {
		t.Errorf("warrior definition = %+v, want the Top Gun label", warrior.Def)
	}
	if warrior.Def.Condition == "" {
		t.Error("warrior has no earning condition")
	}

	// A code with no definition is still reported: the count is real even when
	// the encyclopedia does not describe it.
	sniper := byCode["sniper"]
	if sniper.Count != 48 {
		t.Errorf("sniper count = %d, want 48", sniper.Count)
	}
	if sniper.HasDef {
		t.Error("sniper has a definition; the fixture does not provide one")
	}

	// The three API blocks land under distinct kinds.
	kinds := map[string]int{}
	for _, a := range account {
		kinds[a.Kind]++
	}
	for _, kind := range []string{wg.KindAchievement, wg.KindMaxSeries, wg.KindFrags} {
		if kinds[kind] == 0 {
			t.Errorf("no achievements stored under kind %q", kind)
		}
	}

	tank, err := db.LatestTankAchievements(ctx, 4929)
	if err != nil {
		t.Fatalf("LatestTankAchievements: %v", err)
	}
	if len(tank) != 4 {
		t.Errorf("stored %d achievements for tank 4929, want 4: %+v", len(tank), tank)
	}
}

// TestZeroAchievementCountsAreNotStored: storing a zero would imply the
// achievement was measured and not earned, when in fact it was never returned.
func TestZeroAchievementCountsAreNotStored(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, map[string]http.HandlerFunc{
		"account/achievements": func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"status":"ok","data":{"512345678":{"achievements":{"warrior":0,"sniper":5},"max_series":{},"frags":{}}}}`))
		},
	}))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	got, err := db.LatestAccountAchievements(ctx)
	if err != nil {
		t.Fatalf("LatestAccountAchievements: %v", err)
	}
	if len(got) != 1 || got[0].Code != "sniper" {
		t.Errorf("stored %+v, want only the non-zero count", got)
	}
}

// TestMissingPrivateBlockIsACaveat covers a sync with no usable token: the
// request still succeeds, so the absence has to be announced rather than
// reported as zero credits.
func TestMissingPrivateBlockIsACaveat(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, map[string]http.HandlerFunc{
		"account/info": func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"status":"ok","data":{"512345678":{"account_id":512345678,"nickname":"example_player","global_rating":7767}}}`))
		},
	}))
	s.AccessToken = ""

	report, err := s.SyncWG(ctx)
	if err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	var found bool
	for _, c := range report.Caveats {
		if strings.Contains(c, "private data unavailable") {
			found = true
		}
	}
	if !found {
		t.Errorf("caveats = %v, want one about the missing private block", report.Caveats)
	}
	// The public part still landed, so the source counts as synced.
	if !contains(report.Synced, "wg:account/info") {
		t.Errorf("Synced = %v, want account/info despite the missing private block", report.Synced)
	}
}

// TestSyncRecordsSnapshotsVerbatim is the store's half of the contract: the raw
// body must be kept so a parser change never costs another request.
func TestSyncRecordsSnapshotsVerbatim(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, nil))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	for _, endpoint := range []string{"account/info", "tanks/stats", "account/tanks", "encyclopedia/vehicles"} {
		snap, err := db.LatestUsableSnapshot(ctx, "wg", endpoint)
		if err != nil {
			t.Errorf("no usable snapshot for %s: %v", endpoint, err)
			continue
		}
		if len(snap.Raw) == 0 {
			t.Errorf("%s snapshot holds no body", endpoint)
		}
		if !snap.RequestedAt.Equal(testNow) {
			t.Errorf("%s RequestedAt = %s, want the injected clock", endpoint, snap.RequestedAt)
		}
	}
}

// TestDryRunTouchesNothing is the plan step 4 criterion that had to wait for a
// client to exist.
func TestDryRunTouchesNothing(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	var calls atomic.Int32
	s := newTestSyncer(t, db, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Write(fixture(t, "encyclopedia-vehicles-page.json"))
	})
	s.DryRun = true

	report, err := s.SyncWG(ctx)
	if err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	if got := calls.Load(); got != 0 {
		t.Errorf("a dry run made %d requests, want 0", got)
	}
	if len(report.Synced) != 7 {
		t.Errorf("Synced = %v, want all seven sources it would fetch", report.Synced)
	}
	if n, _ := db.VehicleCount(ctx); n != 0 {
		t.Errorf("a dry run wrote %d vehicles", n)
	}
	if _, err := db.LatestSyncRun(ctx); err == nil {
		t.Error("a dry run recorded a sync run")
	}
	if _, err := db.LatestSnapshot(ctx, "wg", "encyclopedia/vehicles"); err == nil {
		t.Error("a dry run recorded a snapshot")
	}
}

// TestDryRunWorksWithNoCacheOrCredentials: reporting intent must not require
// the things doing the work would.
func TestDryRunWorksWithNoCacheOrCredentials(t *testing.T) {
	s := &Syncer{
		Config: config.Default(),
		Now:    func() time.Time { return testNow },
		DryRun: true,
		// No DB, no WG client - as on a machine that has never synced.
	}

	report, err := s.SyncWG(context.Background())
	if err != nil {
		t.Fatalf("SyncWG: %v", err)
	}
	if len(report.Caveats) != 0 {
		t.Errorf("caveats = %v, want none for a dry run", report.Caveats)
	}
	if len(report.Synced) != 7 {
		t.Errorf("Synced = %v, want all seven sources", report.Synced)
	}
	if !report.OK() {
		t.Error("OK() = false for a dry run that found work to do")
	}
}

func TestMissingClientIsACaveatNotACrash(t *testing.T) {
	db := openTestDB(t)
	s := &Syncer{DB: db, Config: config.Default(), Now: func() time.Time { return testNow }}

	report, err := s.SyncWG(context.Background())
	if err != nil {
		t.Fatalf("SyncWG = %v, want a report rather than an error", err)
	}
	if len(report.Caveats) != 1 {
		t.Fatalf("caveats = %v, want one", report.Caveats)
	}
	if report.OK() {
		t.Error("OK() = true with nothing synced")
	}
}

// TestFreshDataIsSkipped covers the TTL, which is the only real defence for the
// request budget now that Wargaming is known to ignore If-None-Match.
func TestFreshDataIsSkipped(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	log := newRequestLog()
	s := newTestSyncer(t, db, allSources(t, log, nil))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("first SyncWG: %v", err)
	}
	firstVehicles := log.count("encyclopedia/vehicles")
	firstAccount := log.count("account/info")
	if firstVehicles == 0 || firstAccount == 0 {
		t.Fatal("the first sync fetched nothing")
	}

	// Ten minutes later: inside the 1h account TTL and the 7-day encyclopedia
	// TTL, so neither should be refetched.
	s.Now = func() time.Time { return testNow.Add(10 * time.Minute) }
	report, err := s.SyncWG(ctx)
	if err != nil {
		t.Fatalf("second SyncWG: %v", err)
	}

	if got := log.count("encyclopedia/vehicles"); got != firstVehicles {
		t.Errorf("encyclopedia was refetched inside its TTL (%d -> %d)", firstVehicles, got)
	}
	if got := log.count("account/info"); got != firstAccount {
		t.Errorf("account/info was refetched inside its TTL (%d -> %d)", firstAccount, got)
	}
	if len(report.Synced) != 0 {
		t.Errorf("Synced = %v, want nothing", report.Synced)
	}
	if len(report.Skipped) != 7 {
		t.Errorf("Skipped = %v, want all seven sources", report.Skipped)
	}
}

// TestTTLsDifferPerSource: account state goes stale in an hour while the
// encyclopedia lasts a week, so an hourly sync should refetch one and not the
// other.
func TestTTLsDifferPerSource(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	log := newRequestLog()
	s := newTestSyncer(t, db, allSources(t, log, nil))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("first SyncWG: %v", err)
	}
	vehiclesBefore := log.count("encyclopedia/vehicles")
	accountBefore := log.count("account/info")

	s.Now = func() time.Time { return testNow.Add(2 * time.Hour) }
	report, err := s.SyncWG(ctx)
	if err != nil {
		t.Fatalf("second SyncWG: %v", err)
	}

	if got := log.count("account/info"); got <= accountBefore {
		t.Error("account/info was not refetched after its 1h TTL expired")
	}
	if got := log.count("encyclopedia/vehicles"); got != vehiclesBefore {
		t.Error("the encyclopedia was refetched well inside its 7-day TTL")
	}
	if !contains(report.Skipped, "wg:encyclopedia/vehicles") {
		t.Errorf("Skipped = %v, want the encyclopedia", report.Skipped)
	}
	if !contains(report.Synced, "wg:account/info") {
		t.Errorf("Synced = %v, want account/info", report.Synced)
	}
}

func TestForceIgnoresTheTTL(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	log := newRequestLog()
	s := newTestSyncer(t, db, allSources(t, log, nil))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("first SyncWG: %v", err)
	}
	before := log.count("encyclopedia/vehicles")

	s.Force = true
	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("forced SyncWG: %v", err)
	}
	if log.count("encyclopedia/vehicles") <= before {
		t.Error("--full did not refetch")
	}
}

// TestOneFailingSourceDoesNotStopTheRest is the degradation rule: a sync that
// got most of the way is more useful than one that reports nothing.
func TestOneFailingSourceDoesNotStopTheRest(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, map[string]http.HandlerFunc{
		"tanks/achievements": serveFixture(t, "error-source-not-available.json"),
	}))

	report, err := s.SyncWG(ctx)
	if err != nil {
		t.Fatalf("SyncWG = %v, want a report with a caveat", err)
	}

	if len(report.Caveats) != 1 {
		t.Fatalf("caveats = %v, want exactly one", report.Caveats)
	}
	if !strings.Contains(report.Caveats[0], "tanks/achievements") {
		t.Errorf("caveat %q does not name the failing source", report.Caveats[0])
	}
	// Everything else still landed.
	for _, key := range []string{"wg:encyclopedia/vehicles", "wg:account/info", "wg:tanks/stats"} {
		if !contains(report.Synced, key) {
			t.Errorf("%s did not sync despite an unrelated failure", key)
		}
	}
	if !report.OK() {
		t.Error("OK() = false for a run that synced six of seven sources")
	}

	// The failure is recorded as evidence, but never as usable data.
	snap, err := db.LatestSnapshot(ctx, "wg", "tanks/achievements")
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if snap.WGError == "" {
		t.Error("the snapshot did not record the API error")
	}
	if _, err := db.LatestUsableSnapshot(ctx, "wg", "tanks/achievements"); err == nil {
		t.Error("an error body was treated as usable data")
	}

	// And the caveat survives in the run record, so doctor can surface it.
	run, err := db.LatestSyncRun(ctx)
	if err != nil {
		t.Fatalf("LatestSyncRun: %v", err)
	}
	if len(run.NoteLines()) != 1 {
		t.Errorf("run notes = %v, want the caveat", run.NoteLines())
	}
}

// TestMasteryFailureDegradesToACaveat: statistics are the substance, so losing
// the separate mastery request must not lose the statistics with it.
func TestMasteryFailureDegradesToACaveat(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, map[string]http.HandlerFunc{
		"account/tanks": serveFixture(t, "error-source-not-available.json"),
	}))

	report, err := s.SyncWG(ctx)
	if err != nil {
		t.Fatalf("SyncWG: %v", err)
	}
	if !contains(report.Synced, "wg:tanks/stats") {
		t.Errorf("Synced = %v, want tanks/stats despite the mastery failure", report.Synced)
	}

	var found bool
	for _, c := range report.Caveats {
		if strings.Contains(c, "account/tanks") && strings.Contains(c, "mastery") {
			found = true
		}
	}
	if !found {
		t.Errorf("caveats = %v, want one about mastery", report.Caveats)
	}

	// The statistics are there; only the badge is missing.
	stats, err := db.LatestTankStatsFor(ctx, 4929, wg.ModeRandom)
	if err != nil {
		t.Fatalf("LatestTankStatsFor: %v", err)
	}
	if stats.Battles != 529 {
		t.Errorf("battles = %d, want the statistics to have survived", stats.Battles)
	}
	if stats.MarkOfMastery != 0 {
		t.Errorf("mastery = %d, want 0 when the badge request failed", stats.MarkOfMastery)
	}
}

func TestSyncPagesThroughEveryPage(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	var pagesServed atomic.Int32
	s := newTestSyncer(t, db, allSources(t, nil, map[string]http.HandlerFunc{
		"encyclopedia/vehicles": func(w http.ResponseWriter, r *http.Request) {
			pagesServed.Add(1)
			page := r.URL.Query().Get("page_no")
			id := 1000 + int(pagesServed.Load())
			fmt.Fprintf(w, `{"status":"ok","meta":{"count":1,"page_total":3,"total":3,"page":%s},
				"data":{"%d":{"tank_id":%d,"name":"Tank %d","tier":8,"type":"mediumTank","nation":"uk"}}}`,
				page, id, id, id)
		},
	}))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}
	if got := pagesServed.Load(); got != 3 {
		t.Errorf("served %d pages, want 3", got)
	}
	if n, _ := db.VehicleCount(ctx); n != 3 {
		t.Errorf("stored %d vehicles, want 3", n)
	}
}

// TestVehiclesWithNullBodiesAreSkipped: asked for a hidden vehicle, the API
// returns the id as a key with a null body. Storing a nameless tier-0 row would
// put something in the reference list no question could use.
func TestVehiclesWithNullBodiesAreSkipped(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestSyncer(t, db, allSources(t, nil, map[string]http.HandlerFunc{
		"encyclopedia/vehicles": func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"status":"ok","meta":{"page_total":1,"total":2},
				"data":{"41553":null,"19281":{"tank_id":19281,"name":"Concept No. 5","tier":10,"type":"mediumTank","nation":"uk"}}}`))
		},
	}))

	if _, err := s.SyncWG(ctx); err != nil {
		t.Fatalf("SyncWG: %v", err)
	}

	if n, _ := db.VehicleCount(ctx); n != 1 {
		t.Errorf("stored %d vehicles, want only the described one", n)
	}
	if _, err := db.VehicleByID(ctx, 41553); err == nil {
		t.Error("a null-bodied vehicle was stored")
	}
}

func TestCoverageSummary(t *testing.T) {
	coverage := TierCoverage{
		CountsByTier: map[int]int{1: 12, 10: 122, 11: 28},
		MaxTier:      11,
		Total:        162,
	}
	if got, want := coverage.Summary(), "1:12 10:122 11:28"; got != want {
		t.Errorf("Summary = %q, want %q", got, want)
	}
	if !coverage.HasTier(11) {
		t.Error("HasTier(11) = false")
	}
	if coverage.HasTier(9) {
		t.Error("HasTier(9) = true for an absent tier")
	}

	if got := (TierCoverage{}).Summary(); got != "no vehicles" {
		t.Errorf("empty Summary = %q", got)
	}
}

func TestCoverageOnAnEmptyStore(t *testing.T) {
	coverage, err := Coverage(context.Background(), openTestDB(t))
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if coverage.Total != 0 || coverage.MaxTier != 0 {
		t.Errorf("Coverage = %+v, want zero", coverage)
	}
}

func TestOneLineFlattensMultilineErrors(t *testing.T) {
	// Sync run notes are newline-separated, so a multi-line caveat would be
	// read back as several.
	if got := oneLine("first\nsecond\r\nthird"); strings.Contains(got, "\n") {
		t.Errorf("oneLine left a newline: %q", got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestConcurrentSyncsFetchOnce: two processes syncing one cache at once - two
// sessions starting together, or the session-start hook and wot_sync - must
// not both fetch and store the same data. The second waits for the first and
// then finds every source fresh.
func TestConcurrentSyncsFetchOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wotctx.db")
	openAt := func() *store.DB {
		db, err := store.Open(ctx, path)
		if err != nil {
			t.Fatalf("store.Open: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}

	// The first sync holds its run open until the second has tried to begin
	// one and been told to wait, so the two really do overlap.
	firstInside, secondWaiting := make(chan struct{}), make(chan struct{})
	var once sync.Once
	first := newTestSyncer(t, openAt(), allSources(t, nil, map[string]http.HandlerFunc{
		"encyclopedia/vehicles": func(w http.ResponseWriter, r *http.Request) {
			once.Do(func() { close(firstInside) })
			select {
			case <-secondWaiting:
			case <-time.After(5 * time.Second):
				t.Error("the second sync never waited for the first")
			}
			singlePage(t)(w, r)
		},
	}))

	secondLog := newRequestLog()
	second := newTestSyncer(t, openAt(), allSources(t, secondLog, nil))
	second.LockWait = time.Minute
	second.Sleep = func(time.Duration) { time.Sleep(20 * time.Millisecond) }
	var waitOnce sync.Once
	second.Log = func(line string) {
		if strings.Contains(line, "another sync is running") {
			waitOnce.Do(func() { close(secondWaiting) })
		}
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := first.Sync(ctx, OnlyWG)
		firstDone <- err
	}()
	<-firstInside

	report, err := second.Sync(ctx, OnlyWG)
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if err := <-firstDone; err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	if len(report.Synced) != 0 || len(report.Skipped) != 7 {
		t.Errorf("second sync: Synced = %v, Skipped = %v; want nothing fetched and all seven fresh",
			report.Synced, report.Skipped)
	}
	for _, endpoint := range []string{"encyclopedia/vehicles", "account/info", "tanks/stats"} {
		if n := secondLog.count(endpoint); n != 0 {
			t.Errorf("the second sync requested %s %d time(s)", endpoint, n)
		}
	}
}

// TestBusySyncGivesUp: with no time to wait, a sync that finds another
// running says so instead of fetching.
func TestBusySyncGivesUp(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if _, err := db.BeginSyncRun(ctx, testNow.Add(-time.Minute)); err != nil {
		t.Fatalf("BeginSyncRun: %v", err)
	}

	log := newRequestLog()
	s := newTestSyncer(t, db, allSources(t, log, nil))
	if _, err := s.Sync(ctx, OnlyWG); !errors.Is(err, store.ErrSyncRunning) {
		t.Fatalf("Sync during another run = %v, want store.ErrSyncRunning", err)
	}
	if n := log.count("encyclopedia/vehicles"); n != 0 {
		t.Errorf("the refused sync still made %d request(s)", n)
	}
}

// TestCancelledSyncReleasesTheLock: a sync stopped part-way must close its run,
// or every sync for the next few minutes would wait on one that is gone.
func TestCancelledSyncReleasesTheLock(t *testing.T) {
	db := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newTestSyncer(t, db, allSources(t, nil, map[string]http.HandlerFunc{
		"encyclopedia/vehicles": func(w http.ResponseWriter, r *http.Request) {
			cancel()
			singlePage(t)(w, r)
		},
	}))
	if _, err := s.Sync(ctx, OnlyWG); err == nil {
		t.Fatal("Sync after cancellation = nil, want an error")
	}

	run, err := db.LatestSyncRun(context.Background())
	if err != nil {
		t.Fatalf("LatestSyncRun: %v", err)
	}
	if !run.Finished() || run.OK {
		t.Errorf("cancelled run = %+v, want finished and not ok", run)
	}
	if _, err := db.BeginSyncRun(context.Background(), testNow); err != nil {
		t.Errorf("BeginSyncRun after a cancelled sync = %v, want the lock free", err)
	}
}
