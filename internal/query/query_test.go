package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/testseed"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var now = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

// Vehicle ids from the shared seed.
const (
	idBlesk    = testseed.Blesk
	idSelma    = testseed.Selma
	idTesak    = testseed.Tesak
	idAMX1390  = testseed.AMX1390
	idKranvagn = testseed.Kranvagn
	idLeox     = testseed.Leox
)

// seed returns a Service over the shared test account.
func seed(t *testing.T) *Service {
	t.Helper()
	fx := testseed.Seed(t)
	return &Service{DB: fx.DB, Config: config.Default(), OverlayPath: fx.OverlayPath,
		Now: func() time.Time { return testseed.Now }}
}

// golden compares an envelope with testdata/golden/query/<name>.json, and
// checks the invariant every query shares: non-empty sources, each with an age.
func golden(t *testing.T, name string, env Envelope, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	if len(env.Meta.Sources) == 0 {
		t.Errorf("%s: meta.sources is empty", name)
	}
	for _, s := range env.Meta.Sources {
		if s.ObservedAt.IsZero() || s.AgeSeconds < 0 {
			t.Errorf("%s: source %+v lacks an age", name, s)
		}
	}

	got, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatalf("%s: marshal: %v", name, err)
	}
	got = append(got, '\n')

	path := filepath.Join("..", "..", "testdata", "golden", "query", name+".json")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run: go test ./internal/query -update)", name, err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Errorf("%s: output differs from %s (run: go test ./internal/query -update, then review the diff)\n%s",
			name, path, got)
	}
}

func TestGoldenResources(t *testing.T) {
	env, err := seed(t).Resources(context.Background())
	golden(t, "resources", env, err)
}

func TestGoldenGarage(t *testing.T) {
	s := seed(t)
	env, err := s.Garage(context.Background(), GarageFilter{})
	golden(t, "garage", env, err)
	env, err = s.Garage(context.Background(), GarageFilter{Tier: 9, Class: "lightTank"})
	golden(t, "garage-tier9-light", env, err)
}

func TestGoldenTank(t *testing.T) {
	env, err := seed(t).Tank(context.Background(), "AMX 13 90")
	golden(t, "tank-amx-13-90", env, err)
}

func TestGoldenPerformance(t *testing.T) {
	s := seed(t)
	for _, tc := range []struct {
		by, window string
	}{
		{ByClass, "lifetime"}, {ByTier, "30d"}, {ByNation, "60d"},
	} {
		w, err := ParseWindow(tc.window)
		if err != nil {
			t.Fatal(err)
		}
		env, err := s.Performance(context.Background(), tc.by, w, 0)
		golden(t, "performance-"+tc.by+"-"+tc.window, env, err)
	}
}

func TestGoldenSessions(t *testing.T) {
	s := seed(t)
	env, err := s.Sessions(context.Background(), Window{Days: 30})
	golden(t, "sessions-30d", env, err)
	env, err = s.Sessions(context.Background(), Window{Days: 90})
	golden(t, "sessions-90d", env, err)
}

func TestGoldenCandidates(t *testing.T) {
	s := seed(t)
	env, err := s.Candidates(context.Background(), 0)
	golden(t, "candidates", env, err)
	env, err = s.Candidates(context.Background(), 7000000)
	golden(t, "candidates-budget", env, err)
}

// TestCandidatesExcludeAvoidedClasses is a plan step 9 acceptance criterion:
// with the seed overlay's avoid_classes: [SPG], no SPG is offered.
func TestCandidatesExcludeAvoidedClasses(t *testing.T) {
	env, err := seed(t).Candidates(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	c := env.Data.(Candidates)
	for _, cand := range c.Candidates {
		if cand.Class == "SPG" {
			t.Errorf("SPG offered: %+v", cand)
		}
	}
	if c.ExcludedByPreference == 0 {
		t.Error("nothing excluded, so the dataset did not exercise the filter")
	}

	byID := map[int]Candidate{}
	for _, cand := range c.Candidates {
		byID[cand.TankID] = cand
	}
	if k := byID[idKranvagn]; k.Origin != OriginResearched || k.Researched == nil || !*k.Researched || k.XPCost != 0 {
		t.Errorf("Kranvagn = %+v, want researched with no XP cost", k)
	}
	// The Šelma is one goal's target and the next goal's step; target wins.
	// 166,295 of 185,490 is banked, so 19,195 remains. The 41,294 free XP would
	// cover it, but the Šelma is tier IX and the player's policy spends free XP
	// on research only up to tier VIII, so it is not offered.
	if s := byID[idSelma]; s.Origin != OriginNextResearch || s.Goal != "target" || s.XPCost != 185490 ||
		s.XPRemaining == nil || *s.XPRemaining != 19195 || s.FreeXPAllowed || s.FreeXPCovers ||
		s.Researched != nil || s.ClassRank != 2 {
		t.Errorf("Šelma = %+v, want 19,195 XP remaining, free XP not allowed at tier IX, class rank 2", s)
	}
	// The 500,000 buffer counts: 4,000,000 credits buys the 3,500,000 Šelma
	// exactly, and leaves the 6,100,000 Kranvagn 2,600,000 short, not 2,100,000.
	if s := byID[idSelma]; !s.Affordable || s.CreditsShort != 0 {
		t.Errorf("Šelma affordability = %v, short %d; want affordable at exactly price + buffer", s.Affordable, s.CreditsShort)
	}
	if k := byID[idKranvagn]; k.Affordable || k.CreditsShort != 2600000 {
		t.Errorf("Kranvagn short %d, want 2,600,000 including the buffer", k.CreditsShort)
	}
	// Reached through the unowned Šelma: the rest of that step plus its own.
	if ts := byID[idTesak]; ts.Goal != "target" || ts.XPCost != 242560 || len(ts.Via) != 1 || ts.Via[0].Owned ||
		ts.PathXP != 19195+242560 || ts.FreeXPCovers {
		t.Errorf("Tesák = %+v, want path_xp 261,755 through the unowned Šelma", ts)
	}
	for _, id := range []int{idAMX1390, idBlesk, idLeox} {
		if _, ok := byID[id]; ok {
			t.Errorf("owned tank %d offered as a candidate", id)
		}
	}
}

// TestTankAcceptsNameAndID is a plan step 9 acceptance criterion.
func TestTankAcceptsNameAndID(t *testing.T) {
	s := seed(t)
	byName, err := s.Tank(context.Background(), "amx 13 90")
	if err != nil {
		t.Fatal(err)
	}
	byID, err := s.Tank(context.Background(), "4929")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(byName)
	b, _ := json.Marshal(byID)
	if !bytes.Equal(a, b) {
		t.Errorf("by name and by id differ:\n%s\n%s", a, b)
	}

	if _, err := s.Tank(context.Background(), "Tesak"); err != nil {
		t.Errorf("Tank(Tesak) = %v; diacritic folding should resolve it", err)
	}
	_, err = s.Tank(context.Background(), "Kranvagen")
	var unknown *ErrUnknownTank
	if !errors.As(err, &unknown) || len(unknown.Suggestions) == 0 || unknown.Suggestions[0] != "Kranvagn" {
		t.Errorf("Tank(Kranvagen) = %v, want an unknown-tank error suggesting Kranvagn", err)
	}
}

// TestWindowsNeverFallBackToAShorterSpan: with history reaching back 40 days,
// a 60-day window is unavailable rather than quietly answered with 40.
func TestWindowsNeverFallBackToAShorterSpan(t *testing.T) {
	s := seed(t)
	env, err := s.Performance(context.Background(), ByClass, Window{Days: 60}, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := env.Data.(Performance)
	if p.Available || p.Total != nil || !strings.Contains(p.Reason, "not yet: 40.0 days") {
		t.Errorf("60d = available %v, reason %q", p.Available, p.Reason)
	}

	env, err = s.Performance(context.Background(), ByClass, Window{Days: 30}, 0)
	if err != nil {
		t.Fatal(err)
	}
	p = env.Data.(Performance)
	// The baseline is the 40-day snapshot: the newest at or before the cutoff.
	// The span says so rather than claiming 30.
	if !p.Available || p.Span == nil || p.Span.Days < 39.9 || p.Span.Days > 40 {
		t.Errorf("30d span = %+v, want the 40-day baseline reported as such", p.Span)
	}
}

// TestAssistIsLabelled: every result carrying random-battle assist says what
// it is, so it is never mistaken for the in-game figure.
func TestAssistIsLabelled(t *testing.T) {
	s := seed(t)
	ctx := context.Background()
	check := func(name string, env Envelope, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range env.Meta.Caveats {
			if c == AssistCaveat {
				return
			}
		}
		t.Errorf("%s: no assist caveat in %v", name, env.Meta.Caveats)
	}
	env, err := s.Garage(ctx, GarageFilter{})
	check("garage", env, err)
	env, err = s.Tank(ctx, "AMX 13 90")
	check("tank", env, err)
	env, err = s.Performance(ctx, ByClass, Window{Lifetime: true}, 0)
	check("performance", env, err)
}

// TestPremiumNeverComesFromTheAPI: the seeded API row says no premium; the
// overlay says both. The overlay wins, and with no overlay it is unknown.
func TestPremiumNeverComesFromTheAPI(t *testing.T) {
	s := seed(t)
	env, err := s.Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := env.Data.(Resources).Premium
	if p.Source != "overlay" || p.PremiumAccount == nil || !*p.PremiumAccount {
		t.Errorf("premium = %+v, want true from the overlay", p)
	}

	s.OverlayPath = filepath.Join(t.TempDir(), "absent.yaml")
	env, err = s.Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p = env.Data.(Resources).Premium
	if p.Source != "unknown" || p.PremiumAccount != nil || p.WoTPlus != nil {
		t.Errorf("premium with no overlay = %+v, want unknown", p)
	}
}

// TestSyncCaveatsReachAffectedQueries: the seeded sync noted an achievements
// failure; queries that do not use achievements must not repeat it.
func TestSyncCaveatsReachAffectedQueries(t *testing.T) {
	env, err := seed(t).Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range env.Meta.Caveats {
		if strings.Contains(c, "achievements") {
			t.Errorf("resources carries an unrelated caveat: %q", c)
		}
	}
}

func TestParseWindow(t *testing.T) {
	for in, want := range map[string]Window{"lifetime": {Lifetime: true}, "30d": {Days: 30}, "7d": {Days: 7}} {
		if got, err := ParseWindow(in); err != nil || got != want {
			t.Errorf("ParseWindow(%q) = %+v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "30", "0d", "-1d", "1w", "month"} {
		if _, err := ParseWindow(bad); err == nil {
			t.Errorf("ParseWindow(%q) = nil error", bad)
		}
	}
}

// TestMoEBoundsCombinedDamage: the player's figure is a range because marks
// take the larger assist per battle, which totals cannot give. Hand-checked
// for the seeded Blesk: damage 81,000, radio 40,500, track 2,700 over 54.
func TestMoEBoundsCombinedDamage(t *testing.T) {
	s := seed(t)
	table := meta.MoETable{
		URL:     "https://tomato.gg/moe/eu",
		Updated: testseed.Now.Add(-2 * time.Hour),
		ByTank: map[int]meta.MoE{
			idBlesk: {TankID: idBlesk, Name: "Blesk", Tier: 8,
				Thresholds: map[int]int{65: 1451, 85: 2221, 95: 2870, 100: 3425},
				Change30d:  map[int]int{65: 198, 85: 265, 95: 303, 100: 332}},
		},
	}
	env, err := s.MoE(context.Background(), "blesk", &table)
	if err != nil {
		t.Fatal(err)
	}
	m := env.Data.(Marks)
	if m.Lifetime == nil || m.Lifetime.Low != 2250 || m.Lifetime.High != 2300 || m.Lifetime.Battles != 54 {
		t.Errorf("lifetime = %+v, want 2,250 to 2,300 over 54", m.Lifetime)
	}
	// 30 days back reaches the 40-day snapshot: 14 battles, same ratios.
	if m.Recent == nil || m.Recent.Battles != 14 || m.Recent.Low != 2250 || m.Recent.High != 2300 || m.Recent.Span == nil {
		t.Errorf("recent = %+v", m.Recent)
	}
	if m.Thresholds[85] != 2221 {
		t.Errorf("thresholds = %v", m.Thresholds)
	}
	if env.Meta.Sources[0].Name != table.URL || env.Meta.Sources[0].AgeSeconds != 7200 {
		t.Errorf("sources = %+v, want the page first, aged from its own timestamp", env.Meta.Sources)
	}

	if _, err := s.MoE(context.Background(), "Kranvagn", &table); err == nil || !strings.Contains(err.Error(), "no row") {
		t.Errorf("MoE(Kranvagn) = %v, want a no-row error", err)
	}
}

// TestMoEWithTheFetchSwitchedOff: with meta.moe_fetch off the player's own
// figures still come back, and the caveat says why the thresholds do not.
func TestMoEWithTheFetchSwitchedOff(t *testing.T) {
	env, err := seed(t).MoE(context.Background(), "blesk", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := env.Data.(Marks)
	if m.Thresholds != nil || m.Lifetime == nil {
		t.Errorf("marks = %+v, want no thresholds and the lifetime range", m)
	}
	if !strings.Contains(strings.Join(env.Meta.Caveats, "\n"), "meta.moe_fetch") {
		t.Errorf("caveats = %q, want the switch named", env.Meta.Caveats)
	}
	for _, src := range env.Meta.Sources {
		if strings.Contains(src.Name, "tomato.gg") {
			t.Errorf("sources name tomato.gg although nothing was fetched: %+v", src)
		}
	}
}

func TestGoldenMissions(t *testing.T) {
	s := seed(t)
	env, err := s.Missions(context.Background(), "", false)
	golden(t, "missions", env, err)
	env, err = s.Missions(context.Background(), "t 55", false)
	golden(t, "missions-t55a", env, err)
}

// TestMissionsReportOpenNotNext: HT-10 is done while HT-9 is open, as on the
// live account, so the query counts open missions and names the first without
// calling it "next".
func TestMissionsReportOpenNotNext(t *testing.T) {
	env, err := seed(t).Missions(context.Background(), "3", false)
	if err != nil {
		t.Fatal(err)
	}
	m := env.Data.(Missions)
	if m.UndescribedStatuses != 1 || len(m.Operations) != 2 {
		t.Fatalf("missions = %+v", m)
	}
	t55 := m.Operations[0]
	if t55.Operation != "T 55A" || t55.Missions != 4 || t55.Done != 2 || t55.DoneWithHonors != 1 || t55.NotDone != 2 {
		t.Errorf("T 55A = %+v", t55)
	}
	if len(t55.OpenByClass) != 2 || t55.OpenByClass[0].Class != "lightTank" || t55.OpenByClass[0].First.MissionID != 165 ||
		t55.OpenByClass[1].Class != "heavyTank" || t55.OpenByClass[1].Open != 1 || t55.OpenByClass[1].First.MissionID != 174 {
		t.Errorf("open by class = %+v", t55.OpenByClass)
	}
	if m.Detail == nil || m.Detail.Operation != "T 55A" || len(m.Detail.Missions) != 4 ||
		m.Detail.Missions[3].Status != MissionDone {
		t.Errorf("detail = %+v", m.Detail)
	}
}

// TestMissionsOpenOnly: the narrow listing evaluation run 1 lacked.
func TestMissionsOpenOnly(t *testing.T) {
	env, err := seed(t).Missions(context.Background(), "T 55A", true)
	if err != nil {
		t.Fatal(err)
	}
	d := env.Data.(Missions).Detail
	if d == nil || !d.OpenOnly || len(d.Missions) != 2 || d.Missions[0].MissionID != 165 || d.Missions[1].MissionID != 174 {
		t.Errorf("detail = %+v, want only LT-15 and HT-9", d)
	}
}
