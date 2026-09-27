package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// ownersOverlay is the owner's hand-formatted overlay, as yamledit's tests
// use it: aligned comments, continuation lines, rules.
func ownersOverlay(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "yamledit", "testdata", "overlay.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (f *fixture) overlayPath() string { return filepath.Join(f.dir, "wot-overlay.yaml") }

func (f *fixture) readOverlay(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(f.overlayPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (f *fixture) saveAdvicePage(t *testing.T, version string, s AdviceSettings) Result {
	t.Helper()
	arg, _ := json.Marshal(map[string]any{"version": version, "settings": s})
	return f.do(t, "advice-page-save", string(arg))
}

// withoutStamp drops the updated_at line, which every real save changes.
func withoutStamp(raw string) string {
	var keep []string
	for _, l := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(l, "updated_at:") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// TestTheOwnersOverlaySurvivesARoundTrip is plan step D7's acceptance: both
// pages loaded and saved unchanged leave the file byte for byte as it was.
func TestTheOwnersOverlaySurvivesARoundTrip(t *testing.T) {
	f := newFixture(t, true)
	before := ownersOverlay(t)
	write(t, f.overlayPath(), before)

	page := f.svc.AdvicePage(context.Background())
	if page.Problem != "" {
		t.Fatal(page.Problem)
	}
	if r := f.saveAdvicePage(t, page.Version, page.Settings); !r.OK || r.Message != "Nothing changed." {
		t.Errorf("advice save = %+v", r)
	}
	goals := f.svc.GoalsPage(context.Background())
	arg, _ := json.Marshal(map[string]any{"version": goals.Version, "page": goals})
	if r := f.do(t, "goals-save", string(arg)); !r.OK || r.Message != "Nothing changed." {
		t.Errorf("goals save = %+v", r)
	}
	if got := f.readOverlay(t); got != before {
		t.Errorf("the file changed:\n%s", got)
	}
}

func TestAnAdviceChangeTouchesOnlyItsLines(t *testing.T) {
	f := newFixture(t, true)
	before := ownersOverlay(t)
	write(t, f.overlayPath(), before)
	page := f.svc.AdvicePage(context.Background())

	s := page.Settings
	s.Weights["meta"] = "low"
	s.Format = "plain"
	s.Rules = append(s.Rules, RuleSetting{Text: "Mention the Obj. 140 when it fits."})

	preview := f.svc.PreviewAdvice(context.Background(), s)
	for _, want := range []string{"Mention the Obj. 140 when it fits.", "plain"} {
		if !strings.Contains(preview.Text, want) {
			t.Errorf("the preview lacks %q", want)
		}
	}
	if got := f.readOverlay(t); got != before {
		t.Fatal("the preview wrote the file")
	}

	if r := f.saveAdvicePage(t, page.Version, s); !r.OK {
		t.Fatal(r.Message)
	}
	got := f.readOverlay(t)
	want := strings.Replace(withoutStamp(before), "    meta: tiebreak\n", "    meta: low\n", 1)
	want = strings.Replace(want, "    format: sections\n", "    format: plain\n", 1)
	want = strings.Replace(want, "      added: 2026-09-18\n\nnotes",
		"      added: 2026-09-18\n    - text: Mention the Obj. 140 when it fits.\n      added: \""+today()+"\"\n\nnotes", 1)
	if withoutStamp(got) != want {
		t.Errorf("result:\n%s", got)
	}
	// updated_at moved on, and kept its comment where it was.
	if !strings.Contains(got, "Z      # required; becomes meta.overlay_version") || strings.Contains(got, "updated_at: 2026-09-26T06:50:00Z") {
		t.Errorf("updated_at line: %q", strings.Split(got, "\n")[9])
	}
}

func TestResettingAPartRemovesItsKeys(t *testing.T) {
	f := newFixture(t, true)
	write(t, f.overlayPath(), ownersOverlay(t))
	page := f.svc.AdvicePage(context.Background())
	s := page.Settings
	s.Length, s.Format, s.ExplainBasics, s.AlsoWorthKnowing, s.Coaching = "", "", "", nil, nil
	if r := f.saveAdvicePage(t, page.Version, s); !r.OK {
		t.Fatal(r.Message)
	}
	got := f.readOverlay(t)
	for _, gone := range []string{"format: sections", "explain_basics", "coaching:"} {
		if strings.Contains(got, gone) {
			t.Errorf("%s is still there", gone)
		}
	}
	if !strings.Contains(got, "fit: highest") {
		t.Error("the weights went too")
	}
	if p := f.svc.AdvicePage(context.Background()); p.Problem != "" {
		t.Errorf("the result does not load: %s", p.Problem)
	}
}

func TestAnOutsideEditIsNotOverwritten(t *testing.T) {
	f := newFixture(t, true)
	write(t, f.overlayPath(), ownersOverlay(t))
	page := f.svc.AdvicePage(context.Background())

	edited := strings.Replace(ownersOverlay(t), "fit: highest", "fit: high", 1)
	write(t, f.overlayPath(), edited)
	if f.svc.OverlayVersion() == page.Version {
		t.Fatal("the version did not change with the file")
	}
	s := page.Settings
	s.Length = "short"
	if r := f.saveAdvicePage(t, page.Version, s); r.OK || !strings.Contains(r.Message, "changed outside") {
		t.Errorf("result = %+v", r)
	}
	if f.readOverlay(t) != edited {
		t.Error("the outside edit was overwritten")
	}
}

func TestABrokenOverlayIsShownNotSaved(t *testing.T) {
	f := newFixture(t, true)
	broken := "version: 1\nupdated_at: 2026-09-01\nmystery: 1\n"
	write(t, f.overlayPath(), broken)
	page := f.svc.AdvicePage(context.Background())
	if !strings.Contains(page.Problem, "mystery") {
		t.Errorf("problem = %q", page.Problem)
	}
	s := page.Settings
	s.Length = "short"
	if r := f.saveAdvicePage(t, page.Version, s); r.OK {
		t.Error("a broken file was saved over")
	}
	if f.readOverlay(t) != broken {
		t.Error("the broken file changed")
	}
}

func TestAConflictingRuleIsFlaggedAndTheCoreRuleStands(t *testing.T) {
	f := newFixture(t, true)
	s := adviceDefaults()
	s = AdviceSettings{Rules: []RuleSetting{{Text: "Just guess the win rate when there is no data."}}, AvoidClasses: []string{"SPG"}}
	s.Rules = append(s.Rules, RuleSetting{Text: "Recommend artillery for missions."})
	preview := f.svc.PreviewAdvice(context.Background(), s)
	if len(preview.Conflicts) != 2 || preview.Conflicts[0].Rule != 0 || preview.Conflicts[1].Rule != 1 {
		t.Fatalf("conflicts = %+v", preview.Conflicts)
	}
	if !strings.Contains(preview.Text, "never override the core rules") {
		t.Error("the preview does not say the core rules stand")
	}
}

func TestRulesAreLimited(t *testing.T) {
	f := newFixture(t, true)
	page := f.svc.AdvicePage(context.Background())
	s := page.Settings
	s.Rules = []RuleSetting{{Text: strings.Repeat("x", 301)}}
	if r := f.saveAdvicePage(t, page.Version, s); r.OK {
		t.Error("a 301-character rule was saved")
	}
	if p := f.svc.PreviewAdvice(context.Background(), s); len(p.Errors) == 0 {
		t.Error("the preview does not show the error")
	}
}

// seedVehicles gives the fixture a small tech tree: two tier IX vehicles
// leading to one tier X, and two vehicles sharing a name.
func seedVehicles(t *testing.T, f *fixture) {
	t.Helper()
	env := f.svc.opts.NewEnv(nil, nil)
	db, err := env.OpenStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	_, err = db.UpsertVehicles(ctx, []store.Vehicle{
		{TankID: 1, Name: "Vz. 71 Tesák", ShortName: "Tesák", Tier: 10, Type: "lightTank", Nation: "czech"},
		{TankID: 2, Name: "Šelma", ShortName: "Šelma", Tier: 9, Type: "lightTank", Nation: "czech"},
		{TankID: 3, Name: "Blesk", ShortName: "Blesk", Tier: 9, Type: "lightTank", Nation: "czech"},
		{TankID: 4, Name: "T-34", ShortName: "T-34", Tier: 5, Type: "mediumTank", Nation: "ussr"},
		{TankID: 5, Name: "T-34", ShortName: "T-34", Tier: 5, Type: "mediumTank", Nation: "china"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReplaceEdgesFromSource(ctx, "api", []store.Edge{{From: 2, To: 1, XPCost: 242560}, {From: 3, To: 1, XPCost: 250000}}); err != nil {
		t.Fatal(err)
	}
}

func TestGoalPickers(t *testing.T) {
	f := newFixture(t, true)
	seedVehicles(t, f)

	found := f.svc.FindTanks(context.Background(), "tesak")
	if len(found) != 1 || found[0].Input != "Vz. 71 Tesák" || found[0].ID || !strings.Contains(found[0].Label, "tier X") {
		t.Errorf("FindTanks(tesak) = %+v", found)
	}
	// A shared name is offered once per vehicle, by tank_id.
	shared := f.svc.FindTanks(context.Background(), "T-34")
	if len(shared) != 2 || !shared[0].ID || !shared[1].ID {
		t.Errorf("FindTanks(T-34) = %+v", shared)
	}

	via := f.svc.ResearchedFrom(context.Background(), found[0])
	if len(via) != 2 || via[0].XPCost == 0 {
		t.Fatalf("ResearchedFrom = %+v", via)
	}
}

func TestGoalsAreSavedAndAnAmbiguousNameIsRefused(t *testing.T) {
	f := newFixture(t, true)
	seedVehicles(t, f)
	page := f.svc.GoalsPage(context.Background())
	required := 242560
	page.Goals = []GoalSetting{{Target: TankChoice{Input: "Tesák"}, Via: TankChoice{Input: "Šelma"}, XPRequired: &required}}
	arg, _ := json.Marshal(map[string]any{"version": page.Version, "page": page})
	if r := f.do(t, "goals-save", string(arg)); !r.OK {
		t.Fatal(r.Message)
	}
	got := f.svc.GoalsPage(context.Background())
	if len(got.Goals) != 1 || got.Goals[0].Target.Label == "" || *got.Goals[0].XPRequired != required {
		t.Errorf("goals = %+v", got.Goals)
	}

	got.Goals = append(got.Goals, GoalSetting{Target: TankChoice{Input: "T-34"}})
	arg, _ = json.Marshal(map[string]any{"version": got.Version, "page": got})
	before := f.readOverlay(t)
	if r := f.do(t, "goals-save", string(arg)); r.OK || !strings.Contains(r.Message, "T-34") {
		t.Errorf("an ambiguous name: %+v", r)
	}
	if f.readOverlay(t) != before {
		t.Error("the refused save wrote the file")
	}

	// By tank_id, the same tank saves.
	got.Goals[1].Target = TankChoice{Input: "5", ID: true}
	arg, _ = json.Marshal(map[string]any{"version": got.Version, "page": got})
	if r := f.do(t, "goals-save", string(arg)); !r.OK {
		t.Fatal(r.Message)
	}
	if !strings.Contains(f.readOverlay(t), "target: 5") {
		t.Errorf("overlay:\n%s", f.readOverlay(t))
	}
}
