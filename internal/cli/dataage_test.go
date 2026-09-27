package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

var testNow = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

// envWithCache returns an env whose cache holds a snapshot per entry in ages,
// each aged by the given duration against a fixed clock, so age assertions are
// exact. A nil map leaves the cache absent entirely.
func envWithCache(t *testing.T, ages map[string]time.Duration) (*Env, *bytes.Buffer) {
	t.Helper()

	env, buf := newTestEnv(t)
	env.Clock = func() time.Time { return testNow }
	seedAllCredentials(t, env)

	if ages == nil {
		return env, buf
	}

	ctx := context.Background()
	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	for key, age := range ages {
		source, endpoint, ok := strings.Cut(key, ":")
		if !ok {
			t.Fatalf("bad source key %q, want source:endpoint", key)
		}
		if _, err := db.PutSnapshot(ctx, store.Snapshot{
			Source:      source,
			Endpoint:    endpoint,
			RequestedAt: testNow.Add(-age),
			HTTPStatus:  200,
			Raw:         []byte(`{}`),
		}); err != nil {
			t.Fatalf("PutSnapshot(%s): %v", key, err)
		}
	}
	return env, buf
}

// TestDataAgeWarnsWithNoCache: a fresh install has not synced yet, which is a
// warning with a fix, not a failure.
func TestDataAgeWarnsWithNoCache(t *testing.T) {
	env, _ := envWithCache(t, nil)

	got := checkDataAge(context.Background(), env)
	if got.Status != statusWarn {
		t.Errorf("status = %q, want %q", got.Status, statusWarn)
	}
	if !strings.Contains(got.Detail, env.Paths.DBFile) {
		t.Errorf("detail %q does not name the missing cache file", got.Detail)
	}
	if got.Fix != "wotctx sync" {
		t.Errorf("Fix = %q, want %q", got.Fix, "wotctx sync")
	}
}

// TestDataAgeDoesNotCreateTheCache: diagnosis must not have side effects.
func TestDataAgeDoesNotCreateTheCache(t *testing.T) {
	env, _ := envWithCache(t, nil)

	if env.HasStore() {
		t.Fatal("cache exists before the check runs")
	}
	checkDataAge(context.Background(), env)
	if env.HasStore() {
		t.Error("checkDataAge created the cache database")
	}
}

func TestDataAgeOKWhenEverythingIsRecent(t *testing.T) {
	env, _ := envWithCache(t, map[string]time.Duration{
		"wg:account/info": 10 * time.Minute,
		"tomato:recents":  25 * time.Minute,
	})

	got := checkDataAge(context.Background(), env)
	if got.Status != statusOK {
		t.Errorf("status = %q, want %q (detail: %s)", got.Status, statusOK, got.Detail)
	}
	for _, want := range []string{"2 sources", "wg:account/info", "tomato:recents"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail %q does not mention %q", got.Detail, want)
		}
	}
	if got.Fix != "" {
		t.Errorf("Fix = %q, want none while data is fresh", got.Fix)
	}
}

// TestDataAgeWarnsPastTheSyncThreshold ties the check to the same threshold the
// skill uses, so the two cannot disagree about whether a sync is due.
func TestDataAgeWarnsPastTheSyncThreshold(t *testing.T) {
	env, _ := envWithCache(t, map[string]time.Duration{
		"wg:account/info": 5 * time.Minute,
		"tomato:recents":  9 * time.Hour, // past the 6h default
	})

	got := checkDataAge(context.Background(), env)
	if got.Status != statusWarn {
		t.Errorf("status = %q, want %q", got.Status, statusWarn)
	}
	if !strings.Contains(got.Detail, "stalest tomato:recents") {
		t.Errorf("detail %q does not identify the stalest source", got.Detail)
	}
	if !strings.Contains(got.Detail, env.Config.Sync.AutoSyncAfter.String()) {
		t.Errorf("detail %q does not name the threshold it applied", got.Detail)
	}
	if got.Fix != "wotctx sync" {
		t.Errorf("Fix = %q, want the sync command", got.Fix)
	}
}

// TestDataAgeHonoursLongTTLs: reference data on a seven-day TTL is not due at
// nine hours, and sync would skip it, so a warning about it could never clear.
func TestDataAgeHonoursLongTTLs(t *testing.T) {
	env, _ := envWithCache(t, map[string]time.Duration{
		"wg:account/info":          5 * time.Minute,
		"wg:encyclopedia/vehicles": 9 * time.Hour,
		"xvm:wn8exp":               3 * 24 * time.Hour,
	})
	if got := checkDataAge(context.Background(), env); got.Status != statusOK {
		t.Errorf("status = %q (%s), want ok while every source is inside its TTL", got.Status, got.Detail)
	}

	past, _ := envWithCache(t, map[string]time.Duration{
		"wg:account/info":          5 * time.Minute,
		"wg:encyclopedia/vehicles": 8 * 24 * time.Hour,
	})
	got := checkDataAge(context.Background(), past)
	if got.Status != statusWarn || !strings.Contains(got.Detail, "wg:encyclopedia/vehicles") {
		t.Errorf("check = %+v, want a warning naming the encyclopedia once past its TTL", got)
	}
}

// TestDataAgeSurfacesSyncCaveats: a known gap from the last sync has to reach
// the user, since that is the difference between "no loadouts" and "loadouts
// unavailable because the mod never uploaded".
func TestDataAgeSurfacesSyncCaveats(t *testing.T) {
	env, _ := envWithCache(t, map[string]time.Duration{"wg:account/info": time.Minute})

	ctx := context.Background()
	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	id, err := db.BeginSyncRun(ctx, testNow.Add(-time.Minute))
	if err != nil {
		t.Fatalf("BeginSyncRun: %v", err)
	}
	if err := db.FinishSyncRun(ctx, id, testNow, true, []string{
		"tomato:equipment unavailable: 403 (mod data not uploaded)",
	}); err != nil {
		t.Fatalf("FinishSyncRun: %v", err)
	}
	db.Close()

	got := checkDataAge(ctx, env)
	if !strings.Contains(got.Detail, "1 caveat") {
		t.Errorf("detail %q does not mention the sync caveat", got.Detail)
	}
}

// TestDataAgeWarnsAfterAFailedSync covers the case where data is technically
// fresh but the last run could not complete.
func TestDataAgeWarnsAfterAFailedSync(t *testing.T) {
	env, _ := envWithCache(t, map[string]time.Duration{"wg:account/info": time.Minute})

	ctx := context.Background()
	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	id, err := db.BeginSyncRun(ctx, testNow.Add(-time.Minute))
	if err != nil {
		t.Fatalf("BeginSyncRun: %v", err)
	}
	if err := db.FinishSyncRun(ctx, id, testNow, false, []string{"wg: no access token"}); err != nil {
		t.Fatalf("FinishSyncRun: %v", err)
	}
	db.Close()

	if got := checkDataAge(ctx, env); got.Status != statusWarn {
		t.Errorf("status = %q, want %q after a failed sync", got.Status, statusWarn)
	}
}

func TestDataAgeReportsAnEmptyCache(t *testing.T) {
	env, _ := envWithCache(t, nil)

	ctx := context.Background()
	db, err := env.OpenStore(ctx) // creates the file with no rows
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	db.Close()

	got := checkDataAge(ctx, env)
	if got.Status != statusWarn {
		t.Errorf("status = %q, want %q", got.Status, statusWarn)
	}
	if !strings.Contains(got.Detail, "no data") {
		t.Errorf("detail %q does not say the cache is empty", got.Detail)
	}
}

func TestHumanizeAge(t *testing.T) {
	cases := []struct {
		seconds int64
		want    string
	}{
		{5, "just now"},
		{59, "just now"},
		{60, "1m ago"},
		{3540, "59m ago"},
		{3600, "1h ago"},
		{47 * 3600, "47h ago"},
		{48 * 3600, "2d ago"},
		{10 * 24 * 3600, "10d ago"},
	}
	for _, tc := range cases {
		if got := humanizeAge(tc.seconds); got != tc.want {
			t.Errorf("humanizeAge(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}
