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
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// stubTomato points every tomato.gg fetch at a local server serving the
// recorded pages, chosen by path. It returns the paths requested.
func stubTomato(t *testing.T) *[]string {
	t.Helper()
	read := func(name string) []byte {
		page, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "meta", name))
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	pages := map[string][]byte{
		"/moe/":              read("tomato-moe.html"),
		"/tank-performance/": read("tomato-perf-30.html"),
		"/economics/":        read("tomato-econ-30.html"),
		"/onslaught/":        read("tomato-onslaught.html"),
	}
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		for prefix, page := range pages {
			if strings.HasPrefix(r.URL.Path, prefix) {
				w.Write(page)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	orig := tomatoClient
	client := &meta.Tomato{Base: server.URL, Fetch: &meta.Fetcher{HTTP: server.Client()}}
	tomatoClient = func() *meta.Tomato { return client }
	t.Cleanup(func() { tomatoClient = orig })
	return &requested
}

func TestMetaMoE(t *testing.T) {
	stubTomato(t)
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

func TestMetaPerf(t *testing.T) {
	requested := stubTomato(t)
	env, buf := envWithCache(t, map[string]time.Duration{"wg:tanks/stats": time.Minute})

	ctx := context.Background()
	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertVehicles(ctx, []store.Vehicle{
		{TankID: 6769, Name: "Vz. 71 Tesák", ShortName: "Tesák", Tier: 10, Type: "lightTank", SyncedAt: testNow},
		{TankID: 67361, Name: "Black Rock", ShortName: "Black Rock", Tier: 11, Type: "heavyTank", SyncedAt: testNow},
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := Run(ctx, env, []string{"meta", "perf", "Tesak"}); err != nil {
		t.Fatalf("meta perf Tesak = %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{`"window":"30d"`, `"server":{"battles":`, "win_rate_differential", "/tank-performance/recent/EU/30"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}

	// A second question in the same process reuses the page.
	buf.Reset()
	if err := Run(ctx, env, []string{"meta", "perf", "--tier", "11", "--min-battles", "1"}); err != nil {
		t.Fatalf("meta perf --tier 11 = %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), `"name":"Black Rock"`) {
		t.Errorf("tier XI list lacks the Black Rock:\n%s", buf.String())
	}
	if len(*requested) != 1 {
		t.Errorf("requests = %v, want the page fetched once", *requested)
	}

	for _, args := range [][]string{
		{"meta", "perf"},
		{"meta", "perf", "--days", "14", "Tesak"},
		{"meta", "perf", "--tier", "10", "Tesak"},
		{"meta", "perf", "--tier", "12"},
	} {
		if err := Run(ctx, env, args); !errors.Is(err, ErrUsage) {
			t.Errorf("%v = %v, want ErrUsage", args, err)
		}
	}
}

func TestMetaEcon(t *testing.T) {
	requested := stubTomato(t)
	env, buf := envWithCache(t, map[string]time.Duration{"wg:tanks/stats": time.Minute})

	ctx := context.Background()
	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertVehicles(ctx, []store.Vehicle{
		{TankID: 23329, Name: "Gorilla", ShortName: "Gorilla", Tier: 11, Type: "AT-SPG", SyncedAt: testNow},
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := Run(ctx, env, []string{"meta", "econ", "--account", "premium", "Gorilla"}); err != nil {
		t.Fatalf("meta econ Gorilla = %v\n%s", err, buf.String())
	}
	for _, want := range []string{`"account":"premium"`, `"profit":-11436`, `"earnings":65434`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output lacks %s:\n%s", want, buf.String())
		}
	}
	if got := (*requested)[0]; got != "/economics/premium/30" {
		t.Errorf("requested %s", got)
	}

	for _, args := range [][]string{
		{"meta", "econ"},
		{"meta", "econ", "--account", "prem", "Gorilla"},
		{"meta", "econ", "--days", "7", "Gorilla"},
		{"meta", "econ", "--owned", "Gorilla"},
	} {
		if err := Run(ctx, env, args); !errors.Is(err, ErrUsage) {
			t.Errorf("%v = %v, want ErrUsage", args, err)
		}
	}
}

func TestMetaOnslaught(t *testing.T) {
	stubTomato(t)
	env, buf := envWithCache(t, map[string]time.Duration{"wg:tanks/stats": time.Minute})
	ctx := context.Background()
	if err := Run(ctx, env, []string{"meta", "onslaught", "--class", "HT"}); err != nil {
		t.Fatalf("meta onslaught = %v\n%s", err, buf.String())
	}
	for _, want := range []string{`"season":"azure_phoenix"`, `"name":"IS-7"`, `"top_percent_from"`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output lacks %s:\n%s", want, buf.String())
		}
	}
	if err := Run(ctx, env, []string{"meta", "onslaught", "--class", "XX"}); !errors.Is(err, ErrUsage) {
		t.Errorf("--class XX = %v, want ErrUsage", err)
	}
}

func TestMetaNews(t *testing.T) {
	feed, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "meta", "wot-news-eu.xml"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(feed) }))
	t.Cleanup(server.Close)
	origFetcher, origURL := metaFetcher, newsURL
	fetcher := &meta.Fetcher{HTTP: server.Client()}
	metaFetcher = func() *meta.Fetcher { return fetcher }
	newsURL = func(string) string { return server.URL + "/en/rss/news/" }
	t.Cleanup(func() { metaFetcher, newsURL = origFetcher, origURL })

	env, buf := envWithCache(t, map[string]time.Duration{"wg:tanks/stats": time.Minute})
	ctx := context.Background()
	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RecordGameVersion(ctx, "2.4", time.Time{}, testNow); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := Run(ctx, env, []string{"meta", "news", "--updates", "--since", "2026-10-05"}); err != nil {
		t.Fatalf("meta news = %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{`"server_game_version":{"version":"2.4","recorded_since"`, `"title":"Preload Update 2.4.1 Right Now!"`, `"version":"2.4.1"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Halloween") {
		t.Errorf("--updates kept a general article:\n%s", out)
	}
	if err := Run(ctx, env, []string{"meta", "news", "--since", "last week"}); !errors.Is(err, ErrUsage) {
		t.Errorf("--since 'last week' = %v, want ErrUsage", err)
	}
}

// stubConfigs answers `meta profile` without Wargaming.
type stubConfigs struct{}

func (stubConfigs) ModulesTree(context.Context, int) (map[int]wg.Module, error) {
	return map[int]wg.Module{1: {ModuleID: 1, Type: "vehicleGun", IsDefault: true}}, nil
}
func (stubConfigs) VehicleProfiles(context.Context, int) ([]wg.ProfileRef, error) {
	return []wg.ProfileRef{{ProfileID: "1", IsDefault: true}}, nil
}
func (stubConfigs) VehicleProfile(context.Context, int, string) (wg.Profile, error) {
	p := wg.Profile{ProfileID: "1", HP: 2000}
	p.Gun.Name = "only gun"
	return p, nil
}

func TestMetaProfile(t *testing.T) {
	orig := profileSource
	profileSource = func(*Env) (meta.ProfileSource, error) { return stubConfigs{}, nil }
	t.Cleanup(func() { profileSource = orig })

	env, buf := envWithCache(t, map[string]time.Duration{"wg:tanks/stats": time.Minute})
	ctx := context.Background()
	db, err := env.OpenStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertVehicles(ctx, []store.Vehicle{
		{TankID: 23329, Name: "Gorilla", ShortName: "Gorilla", Tier: 11, Type: "AT-SPG", SyncedAt: testNow},
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := Run(ctx, env, []string{"meta", "profile", "Gorilla"}); err != nil {
		t.Fatalf("meta profile = %v\n%s", err, buf.String())
	}
	if out := buf.String(); !strings.Contains(out, `"gun":"only gun"`) || !strings.Contains(out, "nothing to research") {
		t.Errorf("output:\n%s", out)
	}
	if err := Run(ctx, env, []string{"meta", "profile"}); !errors.Is(err, ErrUsage) {
		t.Errorf("no tank = %v, want ErrUsage", err)
	}
}
