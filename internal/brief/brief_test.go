package brief

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/query"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/testseed"
)

var update = flag.Bool("update", false, "rewrite the golden file")

func service(t *testing.T) *query.Service {
	t.Helper()
	fx := testseed.Seed(t)
	return &query.Service{DB: fx.DB, Config: config.Default(), OverlayPath: fx.OverlayPath,
		Now: func() time.Time { return testseed.Now }}
}

func renderBrief(t *testing.T, opts Options) string {
	t.Helper()
	out, err := Render(context.Background(), service(t), opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}

// TestGoldenBrief is the plan step 10 acceptance criterion: within the cap,
// with a Data age and a Known gaps section, pinned against a reviewed file.
func TestGoldenBrief(t *testing.T) {
	out := renderBrief(t, Options{Title: "example_player (EU)"})

	if n := utf8.RuneCountInString(out); n > DefaultMaxChars {
		t.Errorf("brief is %d characters, over the %d cap", n, DefaultMaxChars)
	}
	for _, want := range []string{"## Data age", "## Known gaps"} {
		if !strings.Contains(out, want) {
			t.Errorf("brief lacks %q", want)
		}
	}

	path := filepath.Join("..", "..", "testdata", "golden", "brief.md")
	if *update {
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/brief -update)", err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != out {
		t.Errorf("brief differs from %s (run: go test ./internal/brief -update, then review the diff)\n%s", path, out)
	}
}

// TestBriefCoversTheSpec checks each thing spec section 7.2 requires is
// present, by content rather than by heading alone.
func TestBriefCoversTheSpec(t *testing.T) {
	out := renderBrief(t, Options{})
	for what, want := range map[string]string{
		"resources":                 "Credits 4,000,000",
		"premium from the overlay":  "Premium Account yes · WoT Plus yes (from the overlay)",
		"garage by tier":            "By tier: X 1 · IX 2 · VIII 1",
		"garage by class":           "By class: MT 1 · LT 2 · SPG 1",
		"per-class performance":     "| **All** |",
		"a 30-day window":           "The 30d column actually covers 40.0 days",
		"strongest tanks":           "**Strongest**",
		"weakest tanks":             "**Weakest**",
		"windowed WN8 with battles": "(29)",
		"a 60-day window not yet":   "not yet: 40.0 days of history",
		"the goal path":             "261,755 XP still needed from owned vehicles, through LPT-67 Šelma",
		"banked XP":                 "19,195 XP left of 185,490",
		"researched, not bought":    "Researched, not bought: Kranvagn",
		"timestamps":                "`wg:tanks/stats` 2026-09-18 11:00 UTC (1h ago)",
		"history":                   "History: 3 snapshot(s) since 2026-08-09",
		"assist label":              "assist figures are random battles only",
		"gaps no query can raise":   "Marks of Excellence",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%s: brief lacks %q", what, want)
		}
	}
}

// TestBriefTruncatesAndSaysSo: a smaller cap is honoured exactly, and every
// cut section names the query that has the rest.
func TestBriefTruncatesAndSaysSo(t *testing.T) {
	for _, limit := range []int{2000, 3000, 5000} {
		out := renderBrief(t, Options{MaxChars: limit})
		if n := utf8.RuneCountInString(out); n > limit {
			t.Errorf("--max-chars %d: brief is %d characters", limit, n)
		}
		if !strings.Contains(out, "cut to fit the brief; run `wotctx") {
			t.Errorf("--max-chars %d: nothing says what was cut\n%s", limit, out)
		}
		for _, heading := range []string{"## Data age", "## Resources", "## Known gaps"} {
			if !strings.Contains(out, heading) {
				t.Errorf("--max-chars %d: %s was squeezed out entirely", limit, heading)
			}
		}
	}
}

func TestNum(t *testing.T) {
	for in, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 1381031: "1,381,031", -4718969: "-4,718,969"} {
		if got := num(in); got != want {
			t.Errorf("num(%d) = %q, want %q", in, got, want)
		}
	}
}

// With a client mod dump, the brief names it as the source of what no API
// has, and premium status comes from the game.
func TestBriefWithAClientDump(t *testing.T) {
	s := service(t)
	ctx := context.Background()
	at := testseed.Now.Add(-2 * time.Hour)
	id, err := s.DB.PutSnapshot(ctx, store.Snapshot{Source: "mod", Endpoint: "garage", RequestedAt: at, HTTPStatus: 200, Raw: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if err := s.DB.PutModGarage(ctx, store.ModGarage{Account: store.ModAccount{
		SnapshotID: id, CapturedAt: at, Premium: &yes, WotPlus: &yes,
	}}); err != nil {
		t.Fatal(err)
	}
	out, err := Render(ctx, s, Options{MaxChars: DefaultMaxChars, Title: "test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"`mod:garage` 2026-09-18 10:00 UTC",
		"(from the client mod)",
		"come from the client mod's dump of 2026-09-18 10:00 UTC",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	if strings.Contains(out, "Not in any API: per-vehicle XP") {
		t.Error("brief still says vehicle XP is in no source")
	}
}
