package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// seedWN8 gives the env's cache expected values of the given version and one
// rated plus one unrated tank.
func seedWN8(t *testing.T, env *Env, version string) {
	t.Helper()
	ctx := context.Background()
	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer db.Close()

	if _, err := db.ReplaceWN8Expected(ctx, version, testNow.Add(-3*time.Hour), []store.WN8Expected{
		{TankID: 1, Damage: 1400, Frags: 1, Spot: 1.2, Def: 0.8, WinRate: 52},
	}); err != nil {
		t.Fatalf("ReplaceWN8Expected: %v", err)
	}
	id, err := db.PutSnapshot(ctx, store.Snapshot{
		Source: "wg", Endpoint: "tanks/stats", RequestedAt: testNow, HTTPStatus: 200, Raw: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	if _, err := db.PutTankStats(ctx, []store.TankStats{
		// Exactly at expected values, so the rating is 1565.
		{SnapshotID: id, TankID: 1, Mode: "random", Battles: 1000, DamageDealt: 1400000,
			Frags: 1000, Spotted: 1200, DroppedCapturePoints: 800, Wins: 520},
		{SnapshotID: id, TankID: 26705, Mode: "random", Battles: 40, DamageDealt: 80000, Wins: 20},
	}); err != nil {
		t.Fatalf("PutTankStats: %v", err)
	}
}

func TestDoctorWN8Check(t *testing.T) {
	t.Run("no expected values", func(t *testing.T) {
		env, buf := envWithCache(t, map[string]time.Duration{"wg:account/info": time.Minute})
		checks, _ := runDoctorJSON(t, env, buf)
		if c := checks["wn8"]; c.Status != statusWarn || !strings.Contains(c.Fix, "--only wn8") {
			t.Errorf("wn8 check = %+v", c)
		}
	})

	t.Run("current", func(t *testing.T) {
		env, buf := envWithCache(t, map[string]time.Duration{})
		seedWN8(t, env, "2026-09-12")
		checks, _ := runDoctorJSON(t, env, buf)
		c := checks["wn8"]
		for _, want := range []string{
			"XVM expected values 2026-09-12", "account WN8 1565 over 1000 random battles on 1 tanks",
			"1 tank(s) unrated (40 battles)", "no expected values",
		} {
			if !strings.Contains(c.Detail, want) {
				t.Errorf("detail lacks %q: %s", want, c.Detail)
			}
		}
		if c.Status != statusOK {
			t.Errorf("status = %s, want ok: an unrated tank is explained, not a fault", c.Status)
		}
	})

	// A frozen source keeps answering HTTP 200, which is how the pre-split
	// file went unnoticed; only the version stamp gives it away.
	t.Run("frozen source", func(t *testing.T) {
		env, buf := envWithCache(t, map[string]time.Duration{})
		seedWN8(t, env, "2024-09-12")
		checks, _ := runDoctorJSON(t, env, buf)
		if c := checks["wn8"]; c.Status != statusWarn || !strings.Contains(c.Detail, "days old") {
			t.Errorf("wn8 check = %+v", c)
		}
	})
}
