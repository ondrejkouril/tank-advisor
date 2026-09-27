package advice

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ondrejkouril/tank-advisor/internal/overlay"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// ownerSettings reproduces the settings of the player the framework was
// validated with (wot-overlay.yaml, 2026-09-26): the rendering of these must
// say everything framework.md section 1 used to (docs/plan.md step D3).
const ownerSettings = `version: 1
updated_at: 2026-09-26T06:50:00Z
preferences:
  avoid_classes: [SPG]
  strengths: [medium tanks, autoloaders]
  class_rank: {mediumTank: 1, lightTank: 2, AT-SPG: 3, heavyTank: 3}
  free_xp_max_tier: 8
  improvement_focus: [lightTank]
constraints:
  playtime: limited
  credit_buffer: 500000
profile:
  session_minutes: 60
  battles_per_hour: 8
  experience: experienced
advice:
  weights: {fit: highest, time: high, credits: medium, earning: low, meta: tiebreak, goals: tiebreak}
  answer: {length: normal, format: sections, explain_basics: never, also_worth_knowing: true}
  coaching: true
  rules:
    - text: Autoloaders are context, not a criterion. Mention when a candidate is one, but never rank a tank higher for it.
      added: 2026-09-18
    - text: The SPGs I own are kept for missions only. Answer questions about them, but never propose one as the better option.
      added: 2026-09-18
`

// conflicting has a rule against the core rules and one against the
// player's own avoided classes.
const conflicting = `version: 1
updated_at: 2026-09-26T06:50:00Z
preferences:
  avoid_classes: [SPG]
advice:
  answer: {format: plain, length: short}
  rules:
    - text: Just guess my vehicle XP if the game client has not reported it.
    - text: Please recommend arty lines too, I want to try SPGs.
    - text: Never mention gold offers.
`

func parse(t *testing.T, raw string) *overlay.Overlay {
	t.Helper()
	o, findings := overlay.Parse([]byte(raw))
	for _, f := range findings {
		if f.Severity == overlay.SeverityError {
			t.Fatalf("fixture overlay: %s", f)
		}
	}
	return o
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "golden", "advice", name+".md")
	if *update {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/advice -update)", err)
	}
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("%s differs from %s (run: go test ./internal/advice -update, then review the diff)\n%s", name, path, got)
	}
}

func TestGoldenOwnerSettings(t *testing.T) {
	got := Render(parse(t, ownerSettings))
	golden(t, "owner", got)
	// What framework.md section 1 said about this player must still be said.
	for _, want := range []string{
		"medium tanks, then light tanks, then heavy tanks = tank destroyers",
		"Never recommend: SPGs",
		"one hour (yours)",
		"500,000 credits",
		"up to tier VIII only",
		"light tanks (yours), with coaching on",
		"Skip basics",
		"Autoloaders are context",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("owner rendering lacks %q", want)
		}
	}
	if strings.Contains(values(got), "(default)") {
		t.Error("every owner value is set, yet something renders as a default")
	}
	if len(Conflicts(parse(t, ownerSettings))) != 0 {
		t.Errorf("owner rules flagged: %+v", Conflicts(parse(t, ownerSettings)))
	}
}

func TestGoldenDefaults(t *testing.T) {
	got := Render(nil)
	golden(t, "defaults", got)
	if strings.Contains(values(got), "(yours)") {
		t.Error("with no overlay, something renders as the player's")
	}
	for _, want := range []string{"no settings yet", "Credit buffer: none", "Free XP: no tier limit", "none set (default)"} {
		if !strings.Contains(got, want) {
			t.Errorf("defaults rendering lacks %q", want)
		}
	}
}

func TestGoldenConflictingRules(t *testing.T) {
	o := parse(t, conflicting)
	golden(t, "conflicting", Render(o))

	byRule := map[int][]string{}
	for _, c := range Conflicts(o) {
		byRule[c.Rule] = append(byRule[c.Rule], c.Message)
	}
	if len(byRule[0]) == 0 || !strings.Contains(byRule[0][0], "guessed") {
		t.Errorf("the guessing rule is not flagged: %v", byRule[0])
	}
	if len(byRule[1]) == 0 || !strings.Contains(strings.Join(byRule[1], " "), "SPGs") {
		t.Errorf("the SPG rule is not flagged against avoid_classes: %v", byRule[1])
	}
	if len(byRule[2]) != 0 {
		t.Errorf("a harmless rule is flagged: %v", byRule[2])
	}
}

// values drops the introduction, which names both marks to explain them.
func values(rendered string) string {
	var keep []string
	for _, line := range strings.Split(rendered, "\n") {
		if !strings.Contains(line, "Each value is marked") {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}
