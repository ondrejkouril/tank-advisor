package overlay

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

var base = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

// Real tank ids where the live data was checked; the rest are only required to
// be distinct.
const (
	idType71    = 60001
	idWZ113GFT  = 8497
	idKranvagn  = 60002
	idT110E4    = 60003
	idObject277 = 22017
	idConcept5  = 19281
	idExecutor  = 26705
	idSelma     = 6257
	idTesak     = 6769
	idBlesk     = 4721
)

// openCatalog returns a store holding the vehicles the seed overlay names, the
// Concept No. 5 -> Executor edge, and - when garage is non-nil - a synced
// garage. It mirrors what was measured live: the WZ-113G FT and the Executor
// are both owned, and "IS-2" names two vehicles.
func openCatalog(t *testing.T, garage []int) *store.DB {
	t.Helper()
	ctx := context.Background()

	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "wotctx.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	v := func(id int, name, short string, tier int, typ string) store.Vehicle {
		return store.Vehicle{TankID: id, Name: name, ShortName: short, Tier: tier, Type: typ, Nation: "x", SyncedAt: base}
	}
	if _, err := db.UpsertVehicles(ctx, []store.Vehicle{
		v(idType71, "Type 71", "Type 71", 10, "heavyTank"),
		v(idWZ113GFT, "WZ-113G FT", "WZ-113G FT", 10, "AT-SPG"),
		v(idKranvagn, "Kranvagn", "Kranvagn", 10, "heavyTank"),
		v(idT110E4, "T110E4", "T110E4", 10, "AT-SPG"),
		v(idObject277, "Object 277", "Obj. 277", 10, "heavyTank"),
		v(idConcept5, "Concept No. 5", "Concept 5", 10, "mediumTank"),
		v(idExecutor, "Executor", "Executor", 11, "mediumTank"),
		v(3633, "IS-2", "IS-2", 7, "heavyTank"),
		v(59137, "IS-2", "IS-2", 7, "heavyTank"),
		v(121, "WZ-121", "121", 10, "mediumTank"),
		v(idSelma, "LPT-67 Šelma", "Šelma", 9, "lightTank"),
		v(idTesak, "Vz. 71 Tesák", "Tesák", 10, "lightTank"),
		v(idBlesk, "Vz. 64 Blesk", "Blesk", 8, "lightTank"),
	}); err != nil {
		t.Fatalf("UpsertVehicles: %v", err)
	}
	if _, err := db.ReplaceEdgesFromSource(ctx, wg.EdgeSourceNextTanks, []store.Edge{
		{From: idConcept5, To: idExecutor, XPCost: 325000},
		{From: idSelma, To: idTesak, XPCost: 242560},
		{From: idBlesk, To: idSelma, XPCost: 185490},
	}); err != nil {
		t.Fatalf("ReplaceEdgesFromSource: %v", err)
	}

	if garage != nil {
		id, err := db.PutSnapshot(ctx, store.Snapshot{
			Source: "wg", Endpoint: "account/info", RequestedAt: base, HTTPStatus: 200, Raw: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("PutSnapshot: %v", err)
		}
		if err := db.PutAccountState(ctx, store.AccountState{SnapshotID: id, ObservedAt: base}, garage, nil); err != nil {
			t.Fatalf("PutAccountState: %v", err)
		}
	}
	return db
}

func validate(t *testing.T, content string, cat Catalog) Result {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wot-overlay.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	result, err := Validate(context.Background(), path, cat)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return result
}

// findingFor returns the first finding whose field starts with prefix.
func findingFor(r Result, prefix string) (Finding, bool) {
	for _, f := range r.Findings {
		if strings.HasPrefix(f.Field, prefix) {
			return f, true
		}
	}
	return Finding{}, false
}

const minimal = "version: 1\nupdated_at: 2026-09-17T18:02:00Z\npremium: {premium_account: true, wot_plus: true}\n"

// TestSeedFileValidates is the plan step 8 acceptance criterion, run against
// a real, hand-formatted overlay and the garage as measured on 2026-09-18. The
// repository holds no player's overlay since it was published, so the file is
// the owner's, kept as yamledit's fixture. It looks entries up by name, not
// position.
func TestSeedFileValidates(t *testing.T) {
	db := openCatalog(t, []int{idWZ113GFT, idExecutor, idConcept5, idBlesk})

	result, err := Validate(context.Background(), filepath.Join("..", "yamledit", "testdata", "overlay.yaml"), db)
	if err != nil {
		t.Fatalf("Validate(seed): %v", err)
	}
	if !result.Valid() || len(result.Findings) != 0 {
		t.Fatalf("committed overlay: valid=%v, findings %v; want valid with no warnings",
			result.Valid(), result.Findings)
	}
	if !result.Coverage.NamesChecked || !result.Coverage.GarageChecked {
		t.Errorf("coverage = %+v, want both checks run", result.Coverage)
	}

	o := result.Overlay
	if o.UpdatedAt.IsZero() {
		t.Error("UpdatedAt did not parse")
	}
	if o.Premium == nil || !*o.Premium.PremiumAccount || !*o.Premium.WoTPlus {
		t.Errorf("premium = %+v, want both true", o.Premium)
	}
	// "Obj 277" is how the file spells it; the API says "Object 277".
	found := false
	for _, ref := range o.ResearchedNotBought {
		if ref.Input == "Obj 277" {
			found = true
			if !ref.Resolved || ref.ID != idObject277 {
				t.Errorf("Obj 277 resolved to %+v, want tank_id %d", ref, idObject277)
			}
		}
	}
	if !found {
		t.Error("the committed overlay no longer lists Obj 277; update this test's example")
	}
	if got := o.Preferences.AvoidClasses; len(got) != 1 || got[0] != "SPG" {
		t.Errorf("avoid_classes = %v, want [SPG]", got)
	}
}

// TestUnknownTankFailsWithASuggestion is the second acceptance criterion.
func TestUnknownTankFailsWithASuggestion(t *testing.T) {
	db := openCatalog(t, nil)

	result := validate(t, minimal+"researched_not_bought:\n  - Kranvagen\n", db)
	if result.Valid() {
		t.Fatal("an unknown tank name validated")
	}
	f, ok := findingFor(result, "researched_not_bought[0]")
	if !ok || f.Severity != SeverityError {
		t.Fatalf("findings = %v, want an error on researched_not_bought[0]", result.Findings)
	}
	if !strings.Contains(f.Message, `"Kranvagn"`) {
		t.Errorf("message %q does not suggest Kranvagn", f.Message)
	}
	if f.Line != 5 {
		t.Errorf("line = %d, want 5", f.Line)
	}
}

// TestInGarageEntriesAreStale is the third acceptance criterion: stale is a
// warning, because the overlay is expected to drift.
func TestInGarageEntriesAreStale(t *testing.T) {
	db := openCatalog(t, []int{idWZ113GFT, idExecutor})

	result := validate(t, minimal+`researched_not_bought: [Type 71, WZ-113G FT]
xp_goals:
  - {target: Executor, via: Concept No. 5, xp_required: 325000}
`, db)
	if !result.Valid() {
		t.Fatalf("findings = %v, want valid", result.Findings)
	}

	o := result.Overlay
	if o.ResearchedNotBought[0].Owned || !o.ResearchedNotBought[1].Owned {
		t.Errorf("owned = %v/%v, want false/true", o.ResearchedNotBought[0].Owned, o.ResearchedNotBought[1].Owned)
	}
	for _, field := range []string{"researched_not_bought[1]", "xp_goals[0].target"} {
		f, ok := findingFor(result, field)
		if !ok || f.Severity != SeverityWarning || !strings.Contains(f.Message, "stale") {
			t.Errorf("no stale warning on %s; findings = %v", field, result.Findings)
		}
	}
	if _, ok := findingFor(result, "researched_not_bought[0]"); ok {
		t.Error("Type 71 is not owned but was flagged")
	}
}

// TestMalformedFilesAreInvalid covers the fourth criterion at the package
// level; the CLI test checks the exit status.
func TestMalformedFilesAreInvalid(t *testing.T) {
	cases := map[string]struct {
		content string
		want    string
	}{
		"not yaml":            {"version: [1\n", ""},
		"empty":               {"", "empty"},
		"unknown field":       {minimal + "premuim: {}\n", `unknown field "premuim"`},
		"missing version":     {"updated_at: 2026-09-17\n", "version"},
		"future version":      {"version: 2\nupdated_at: 2026-09-17\n", "unsupported version 2"},
		"missing updated_at":  {"version: 1\n", "updated_at"},
		"bad updated_at":      {"version: 1\nupdated_at: last tuesday\n", "not a timestamp"},
		"tank as a map":       {minimal + "researched_not_bought:\n  - {name: Kranvagn}\n", "name or a tank_id"},
		"negative xp":         {minimal + "xp_goals: [{target: Executor, xp_banked: -5}]\n", "negative"},
		"missing goal target": {minimal + "xp_goals: [{via: Concept No. 5}]\n", "required"},
		"zero path cost":      {minimal + "research_paths: [{from: Concept No. 5, to: Executor}]\n", "positive"},
		"unknown class":       {minimal + "preferences: {avoid_classes: [SPGs]}\n", `unknown class "SPGs"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result := validate(t, tc.content, nil)
			if result.Valid() {
				t.Fatalf("validated; findings = %v", result.Findings)
			}
			var messages []string
			for _, f := range result.Findings {
				if f.Severity == SeverityError {
					messages = append(messages, f.String())
				}
			}
			if joined := strings.Join(messages, "\n"); !strings.Contains(joined, tc.want) {
				t.Errorf("errors do not mention %q:\n%s", tc.want, joined)
			}
		})
	}
}

// TestAbsentPremiumIsUnknownNotFalse: assuming no premium would understate
// every credit and XP figure, so absence has to stay visible.
func TestAbsentPremiumIsUnknownNotFalse(t *testing.T) {
	result := validate(t, "version: 1\nupdated_at: 2026-09-17\npremium: {premium_account: true}\n", openCatalog(t, nil))
	if !result.Valid() {
		t.Fatalf("findings = %v", result.Findings)
	}
	if result.Overlay.Premium.WoTPlus != nil {
		t.Error("an absent wot_plus decoded as a value")
	}
	if f, ok := findingFor(result, "premium.wot_plus"); !ok || f.Severity != SeverityWarning {
		t.Errorf("findings = %v, want a warning on premium.wot_plus", result.Findings)
	}

	none := validate(t, "version: 1\nupdated_at: 2026-09-17\n", openCatalog(t, nil))
	if f, ok := findingFor(none, "premium"); !ok || !strings.Contains(f.Message, "unknown") {
		t.Errorf("findings = %v, want premium reported as unknown", none.Findings)
	}
}

func TestAmbiguousNamesNeedATankID(t *testing.T) {
	db := openCatalog(t, nil)

	result := validate(t, minimal+"researched_not_bought: [IS-2]\n", db)
	f, ok := findingFor(result, "researched_not_bought[0]")
	if !ok || f.Severity != SeverityError || !strings.Contains(f.Message, "tank_id instead") {
		t.Fatalf("findings = %v, want an ambiguity error", result.Findings)
	}

	byID := validate(t, minimal+"researched_not_bought: [59137]\n", db)
	if !byID.Valid() || byID.Overlay.ResearchedNotBought[0].ID != 59137 {
		t.Errorf("tank_id reference: findings = %v", byID.Findings)
	}
}

// TestNumericNamesMustBeQuoted: an unquoted 121 is a tank_id, and the vehicle
// named "121" has a different one.
func TestNumericNamesMustBeQuoted(t *testing.T) {
	db := openCatalog(t, nil)

	quoted := validate(t, minimal+"researched_not_bought: [\"121\"]\n", db)
	if !quoted.Valid() || quoted.Overlay.ResearchedNotBought[0].Name != "WZ-121" {
		t.Errorf("quoted 121: findings = %v", quoted.Findings)
	}

	// 121 happens to be a real id in this catalog, so use one that is not.
	db2 := openCatalog(t, nil)
	if _, err := db2.UpsertVehicles(context.Background(), []store.Vehicle{
		{TankID: 60099, Name: "140", ShortName: "140", Tier: 1, Type: "lightTank", SyncedAt: base},
	}); err != nil {
		t.Fatalf("UpsertVehicles: %v", err)
	}
	bare := validate(t, minimal+"researched_not_bought: [140]\n", db2)
	f, ok := findingFor(bare, "researched_not_bought[0]")
	if !ok || !strings.Contains(f.Message, "quote it") {
		t.Errorf("findings = %v, want a hint to quote the name", bare.Findings)
	}
}

func TestGoalCostIsCheckedAgainstTheTechTree(t *testing.T) {
	db := openCatalog(t, nil)

	result := validate(t, minimal+"xp_goals: [{target: Executor, via: Concept No. 5, xp_required: 300000}]\n", db)
	f, ok := findingFor(result, "xp_goals[0].xp_required")
	if !ok || f.Severity != SeverityWarning || !strings.Contains(f.Message, "325000") {
		t.Errorf("findings = %v, want a warning quoting the API's 325000", result.Findings)
	}

	matching := validate(t, minimal+"xp_goals: [{target: Executor, via: Concept No. 5, xp_required: 325000}]\n", db)
	if f, ok := findingFor(matching, "xp_goals"); ok {
		t.Errorf("finding %v, want none on a goal whose cost matches", f)
	}
	if got := matching.Overlay.XPGoals[0]; got.APIXPCost == nil || *got.APIXPCost != 325000 || got.XPBanked != nil {
		t.Errorf("goal = %+v, want API cost 325000 and banked unknown", got)
	}

	noEdge := validate(t, minimal+"xp_goals: [{target: Executor, via: Kranvagn}]\n", db)
	if f, ok := findingFor(noEdge, "xp_goals[0].via"); !ok || !strings.Contains(f.Message, "no research step") {
		t.Errorf("findings = %v, want a missing-edge warning", noEdge.Findings)
	}
}

func TestResearchPathsBecomeOverlayEdges(t *testing.T) {
	db := openCatalog(t, nil)

	result := validate(t, minimal+`research_paths:
  - {from: Kranvagn, to: Executor, xp_cost: 400000}
  - {from: Concept No. 5, to: Executor, xp_cost: 325000}
`, db)
	if !result.Valid() {
		t.Fatalf("findings = %v", result.Findings)
	}
	edges := result.Overlay.Edges()
	if len(edges) != 2 || edges[0].From != idKranvagn || edges[0].Source != wg.EdgeSourceOverlay {
		t.Errorf("edges = %+v", edges)
	}
	// The second duplicates an API edge, which is worth saying.
	if f, ok := findingFor(result, "research_paths[1]"); !ok || !strings.Contains(f.Message, "redundant") {
		t.Errorf("findings = %v, want a redundancy warning", result.Findings)
	}
}

func TestClassAliasesAreCanonicalised(t *testing.T) {
	result := validate(t, minimal+"preferences: {avoid_classes: [arty, TD, spg]}\n", nil)
	got := result.Overlay.Preferences.AvoidClasses
	want := []string{"SPG", "AT-SPG", "SPG"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("avoid_classes = %v, want %v", got, want)
		}
	}
}

// TestUncheckedIsNotPassed: with nothing synced, validation says what it could
// not check instead of implying the names are fine.
func TestUncheckedIsNotPassed(t *testing.T) {
	result := validate(t, minimal+"researched_not_bought: [Nonexistent Tank]\n", nil)
	if result.Coverage.NamesChecked {
		t.Error("NamesChecked with no catalog")
	}
	if result.Count(SeverityWarning) == 0 {
		t.Errorf("findings = %v, want a warning that names were not checked", result.Findings)
	}

	noGarage := validate(t, minimal+"researched_not_bought: [Kranvagn]\n", openCatalog(t, nil))
	if !noGarage.Coverage.NamesChecked || noGarage.Coverage.GarageChecked {
		t.Errorf("coverage = %+v, want names checked but not the garage", noGarage.Coverage)
	}
}

func TestDuplicatesAreFlagged(t *testing.T) {
	result := validate(t, minimal+"researched_not_bought: [Obj 277, Object 277]\n", openCatalog(t, nil))
	if f, ok := findingFor(result, "researched_not_bought[1]"); !ok || !strings.Contains(f.Message, "twice") {
		t.Errorf("findings = %v, want a duplicate warning", result.Findings)
	}
}

func TestMissingFileIsDistinguishable(t *testing.T) {
	_, err := Validate(context.Background(), filepath.Join(t.TempDir(), "absent.yaml"), nil)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Validate(absent) = %v, want os.ErrNotExist", err)
	}
}

// TestFutureUpdatedAtWarns: the overlay's timestamp is quoted as its version
// with every answer, and dates the hand-kept banked XP.
func TestFutureUpdatedAtWarns(t *testing.T) {
	defer func(orig func() time.Time) { Now = orig }(Now)
	Now = func() time.Time { return time.Date(2026, 9, 18, 7, 28, 0, 0, time.UTC) }

	future := validate(t, "version: 1\nupdated_at: 2026-09-18T16:00:00Z\npremium: {premium_account: true, wot_plus: true}\n", nil)
	if f, ok := findingFor(future, "updated_at"); !ok || f.Severity != SeverityWarning || !strings.Contains(f.Message, "future") {
		t.Errorf("findings = %v, want a future-timestamp warning", future.Findings)
	}
	fine := validate(t, "version: 1\nupdated_at: 2026-09-18T07:00:00Z\npremium: {premium_account: true, wot_plus: true}\n", nil)
	if _, ok := findingFor(fine, "updated_at"); ok {
		t.Errorf("findings = %v, want no warning for a past timestamp", fine.Findings)
	}
}

func TestPreferenceFields(t *testing.T) {
	ok := validate(t, minimal+`preferences:
  avoid_classes: [SPG]
  class_rank: {MT: 1, light: 2, TD: 3, heavyTank: 3}
  free_xp_max_tier: 8
  improvement_focus: [LT]
constraints:
  credit_buffer: 500000
`, nil)
	if !ok.Valid() {
		t.Fatalf("findings = %v", ok.Findings)
	}
	p := ok.Overlay.Preferences
	want := map[string]int{"mediumTank": 1, "lightTank": 2, "AT-SPG": 3, "heavyTank": 3}
	for c, rank := range want {
		if p.ClassRank[c] != rank {
			t.Errorf("class_rank = %v, want aliases canonicalised to %v", p.ClassRank, want)
			break
		}
	}
	if p.FreeXPMaxTier != 8 || len(p.ImprovementFocus) != 1 || p.ImprovementFocus[0] != "lightTank" ||
		ok.Overlay.Constraints.CreditBuffer != 500000 {
		t.Errorf("preferences = %+v, constraints = %+v", p, ok.Overlay.Constraints)
	}

	for name, tc := range map[string]struct{ content, want string }{
		"unknown ranked class": {"preferences: {class_rank: {tanks: 1}}\n", `unknown class "tanks"`},
		"zero rank":            {"preferences: {class_rank: {MT: 0}}\n", "1 or more"},
		"tier out of range":    {"preferences: {free_xp_max_tier: 12}\n", "0 (no limit) to 11"},
		"unknown focus":        {"preferences: {improvement_focus: [scouts]}\n", `unknown class "scouts"`},
		"negative buffer":      {"constraints: {credit_buffer: -1}\n", "must not be negative"},
	} {
		r := validate(t, minimal+tc.content, nil)
		if r.Valid() {
			t.Errorf("%s: validated", name)
			continue
		}
		var msgs []string
		for _, f := range r.Findings {
			msgs = append(msgs, f.String())
		}
		if !strings.Contains(strings.Join(msgs, "\n"), tc.want) {
			t.Errorf("%s: findings %v lack %q", name, msgs, tc.want)
		}
	}

	both := validate(t, minimal+"preferences: {avoid_classes: [SPG], class_rank: {SPG: 4}}\n", nil)
	if f, found := findingFor(both, "preferences.class_rank"); !found || f.Severity != SeverityWarning {
		t.Errorf("findings = %v, want a warning that SPG is both ranked and avoided", both.Findings)
	}
}

// TestAdviceSettingsAreValidated covers profile and advice
// (docs/spec-desktop.md section 8.3): a bad value is an error, so the Advice
// page and a hand edit fail the same way instead of silently doing nothing.
func TestAdviceSettingsAreValidated(t *testing.T) {
	head := "version: 1\nupdated_at: 2026-09-18\n"
	valid := head + `profile: {session_minutes: 60, battles_per_hour: 8, experience: auto}
advice:
  weights: {fit: highest, meta: ignore}
  answer: {length: short, format: plain, explain_basics: always, also_worth_knowing: false}
  coaching: false
  rules:
    - text: Never suggest gold purchases.
      added: 2026-09-26
`
	if _, findings := Parse([]byte(valid)); hasError(findings) {
		t.Fatalf("valid advice rejected: %v", findings)
	}

	long := strings.Repeat("x", MaxRuleLength+1)
	var many strings.Builder
	for range MaxRules + 1 {
		many.WriteString("    - text: a rule\n")
	}
	for name, body := range map[string]string{
		"session too short":  "profile: {session_minutes: 5}\n",
		"battles per hour":   "profile: {battles_per_hour: 99}\n",
		"experience":         "profile: {experience: expert}\n",
		"unknown factor":     "advice: {weights: {luck: high}}\n",
		"unknown level":      "advice: {weights: {fit: huge}}\n",
		"format":             "advice: {answer: {format: bullets}}\n",
		"length":             "advice: {answer: {length: epic}}\n",
		"basics":             "advice: {answer: {explain_basics: sometimes}}\n",
		"empty rule":         "advice: {rules: [{text: '  '}]}\n",
		"long rule":          "advice: {rules: [{text: " + long + "}]}\n",
		"too many rules":     "advice:\n  rules:\n" + many.String(),
		"bad date":           "advice: {rules: [{text: ok, added: yesterday}]}\n",
		"unknown advice key": "advice: {tone: friendly}\n",
	} {
		if _, findings := Parse([]byte(head + body)); !hasError(findings) {
			t.Errorf("%s: accepted", name)
		}
	}
}

func hasError(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}
