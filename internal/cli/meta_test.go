package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// stubMoE points `meta moe` at a local server serving the recorded page.
func stubMoE(t *testing.T) {
	t.Helper()
	page, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "meta", "tomato-moe.html"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(page) }))
	t.Cleanup(server.Close)
	orig := moeClient
	moeClient = func() *meta.MoEClient { return &meta.MoEClient{URL: server.URL, HTTP: server.Client()} }
	t.Cleanup(func() { moeClient = orig })
}

func TestMetaMoE(t *testing.T) {
	stubMoE(t)
	env, buf := envWithCache(t, map[string]time.Duration{"wg:tanks/stats": time.Minute})

	ctx := context.Background()
	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertVehicles(ctx, []store.Vehicle{
		{TankID: 6769, Name: "Vz. 71 Tesák", ShortName: "Tesák", Tier: 10, Type: "lightTank", SyncedAt: testNow},
		{TankID: 2433, Name: "Kranvagn", ShortName: "Kranvagn", Tier: 10, Type: "heavyTank", SyncedAt: testNow},
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := Run(ctx, env, []string{"meta", "moe", "Tesak"}); err != nil {
		t.Fatalf("meta moe Tesak = %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{`"thresholds":{"100":5026,"65":2198,"85":3311,"95":4242}`, "no random battles recorded", `"age_seconds":0`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}

	if err := Run(ctx, env, []string{"meta", "moe", "Kranvagn"}); err == nil || !strings.Contains(err.Error(), "no row") {
		t.Errorf("meta moe Kranvagn = %v, want a no-row error", err)
	}
	if err := Run(ctx, env, []string{"meta", "moe"}); !errors.Is(err, ErrUsage) {
		t.Errorf("meta moe with no tank = %v, want ErrUsage", err)
	}
}
