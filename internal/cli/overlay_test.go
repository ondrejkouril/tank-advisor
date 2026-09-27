package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

const validOverlay = `version: 1
updated_at: 2026-09-17T18:02:00Z
premium: {premium_account: true, wot_plus: true}
researched_not_bought: [Obj 277, Executor]
research_paths:
  - {from: Obj 277, to: Executor, xp_cost: 400000}
`

// writeOverlay puts content at the env's default overlay location.
func writeOverlay(t *testing.T, env *Env, content string) string {
	t.Helper()
	path := env.Config.OverlayFile(env.Paths)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// seedCatalog gives the env's cache two vehicles and a garage holding one.
func seedCatalog(t *testing.T, env *Env) {
	t.Helper()
	ctx := context.Background()

	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer db.Close()

	if _, err := db.UpsertVehicles(ctx, []store.Vehicle{
		{TankID: 22017, Name: "Object 277", ShortName: "Obj. 277", Tier: 10, Type: "heavyTank", SyncedAt: testNow},
		{TankID: 26705, Name: "Executor", ShortName: "Executor", Tier: 11, Type: "mediumTank", SyncedAt: testNow},
	}); err != nil {
		t.Fatalf("UpsertVehicles: %v", err)
	}
	id, err := db.PutSnapshot(ctx, store.Snapshot{
		Source: "wg", Endpoint: "account/info", RequestedAt: testNow, HTTPStatus: 200, Raw: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	if err := db.PutAccountState(ctx, store.AccountState{SnapshotID: id, ObservedAt: testNow}, []int{26705}, nil); err != nil {
		t.Fatalf("PutAccountState: %v", err)
	}
}

func TestOverlayPathPrintsTheConfiguredPath(t *testing.T) {
	env, buf := newTestEnv(t)
	env.Config.OverlayPath = filepath.Join(t.TempDir(), "elsewhere.yaml")

	if err := Run(context.Background(), env, []string{"overlay", "path"}); err != nil {
		t.Fatalf("overlay path = %v", err)
	}
	if !strings.HasPrefix(buf.String(), env.Config.OverlayPath+"\n") {
		t.Errorf("output = %q, want the configured path first", buf.String())
	}
	if !strings.Contains(buf.String(), "does not exist") {
		t.Errorf("output = %q, want a note that the file is missing", buf.String())
	}
}

func TestOverlayValidateShowsResolutionAndStaleness(t *testing.T) {
	env, buf := newTestEnv(t)
	seedCatalog(t, env)
	writeOverlay(t, env, validOverlay)

	if err := Run(context.Background(), env, []string{"overlay", "validate"}); err != nil {
		t.Fatalf("overlay validate = %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"Obj 277 -> Object 277 (tank_id 22017",
		"[in garage]",
		"stale: Executor is already in the garage",
		"Premium Account yes, WoT Plus yes",
		"valid, 1 warning(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestOverlayValidateFailsOnAMalformedFile is the plan step 8 exit-status
// criterion.
func TestOverlayValidateFailsOnAMalformedFile(t *testing.T) {
	for name, content := range map[string]string{
		"syntax":       "version: [1\n",
		"schema":       "version: 1\n",
		"unknown tank": "version: 1\nupdated_at: 2026-09-17\nresearched_not_bought: [Obj 2777]\n",
	} {
		t.Run(name, func(t *testing.T) {
			env, buf := newTestEnv(t)
			seedCatalog(t, env)
			writeOverlay(t, env, content)

			err := Run(context.Background(), env, []string{"overlay", "validate"})
			if err == nil {
				t.Fatalf("overlay validate on a %s error = nil\n%s", name, buf.String())
			}
			if !strings.Contains(buf.String(), "invalid:") {
				t.Errorf("output does not say invalid:\n%s", buf.String())
			}
		})
	}
}

func TestOverlayValidateNeedsAFile(t *testing.T) {
	env, _ := newTestEnv(t)
	err := Run(context.Background(), env, []string{"overlay", "validate"})
	if err == nil || !strings.Contains(err.Error(), "no overlay at") {
		t.Errorf("overlay validate with no file = %v, want a 'no overlay' error", err)
	}
}

// TestOverlayValidateDoesNotCreateTheCache: validating a file must not leave a
// database behind on a machine that has not synced.
func TestOverlayValidateDoesNotCreateTheCache(t *testing.T) {
	env, buf := newTestEnv(t)
	writeOverlay(t, env, validOverlay)

	if err := Run(context.Background(), env, []string{"overlay", "validate"}); err != nil {
		t.Fatalf("overlay validate = %v", err)
	}
	if env.HasStore() {
		t.Error("overlay validate created the cache")
	}
	if !strings.Contains(buf.String(), "not checked") {
		t.Errorf("output does not admit names were unchecked:\n%s", buf.String())
	}
}

func TestDoctorOverlayCheck(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		env, buf := newTestEnv(t)
		seedAllCredentials(t, env)
		checks, _ := runDoctorJSON(t, env, buf)
		c := checks["overlay"]
		if c.Status != statusWarn || !strings.Contains(c.Detail, "premium status is unknown") || c.Fix == "" {
			t.Errorf("overlay check = %+v", c)
		}
	})

	t.Run("invalid warns, not fails", func(t *testing.T) {
		env, buf := newTestEnv(t)
		seedAllCredentials(t, env)
		writeOverlay(t, env, "version: 1\n")
		checks, _ := runDoctorJSON(t, env, buf)
		if c := checks["overlay"]; c.Status != statusWarn || !strings.Contains(c.Detail, "invalid") {
			t.Errorf("overlay check = %+v", c)
		}
	})

	t.Run("stale is reported", func(t *testing.T) {
		env, buf := newTestEnv(t)
		seedAllCredentials(t, env)
		seedCatalog(t, env)
		writeOverlay(t, env, validOverlay)
		checks, _ := runDoctorJSON(t, env, buf)
		c := checks["overlay"]
		if c.Status != statusWarn || !strings.Contains(c.Detail, "1 stale") {
			t.Errorf("overlay check = %+v", c)
		}
	})

	t.Run("clean", func(t *testing.T) {
		env, buf := newTestEnv(t)
		seedAllCredentials(t, env)
		seedCatalog(t, env)
		writeOverlay(t, env, strings.Replace(validOverlay, ", Executor]", "]", 1))
		checks, _ := runDoctorJSON(t, env, buf)
		if c := checks["overlay"]; c.Status != statusOK {
			t.Errorf("overlay check = %+v", c)
		}
	})
}

func TestSyncLoadsOverlayEdges(t *testing.T) {
	ctx := context.Background()
	env, _ := newTestEnv(t)
	seedCatalog(t, env)
	writeOverlay(t, env, validOverlay)

	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer db.Close()

	edgeCount := func() int {
		n, err := db.EdgeCount(ctx, wg.EdgeSourceOverlay)
		if err != nil {
			t.Fatalf("EdgeCount: %v", err)
		}
		return n
	}

	if caveat, err := loadOverlayEdges(ctx, env, db); err != nil || caveat != "" {
		t.Fatalf("loadOverlayEdges = (%q, %v)", caveat, err)
	}
	if n := edgeCount(); n != 1 {
		t.Fatalf("overlay edges = %d, want 1", n)
	}

	// A broken overlay keeps the edges the last valid one supplied.
	writeOverlay(t, env, "version: 1\n")
	if caveat, err := loadOverlayEdges(ctx, env, db); err != nil || !strings.Contains(caveat, "invalid") {
		t.Fatalf("loadOverlayEdges on an invalid file = (%q, %v)", caveat, err)
	}
	if n := edgeCount(); n != 1 {
		t.Errorf("overlay edges = %d after an invalid overlay, want 1 kept", n)
	}

	// Deleting the overlay removes them.
	if err := os.Remove(env.Config.OverlayFile(env.Paths)); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := loadOverlayEdges(ctx, env, db); err != nil {
		t.Fatalf("loadOverlayEdges with no file: %v", err)
	}
	if n := edgeCount(); n != 0 {
		t.Errorf("overlay edges = %d with no overlay, want 0", n)
	}
}
