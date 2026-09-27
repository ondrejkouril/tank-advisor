package yamledit

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// overlay is the owner's hand-formatted overlay: aligned comments, comment
// continuation lines, blank lines between sections, and a list of rules.
func overlay(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/overlay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func apply(t *testing.T, before string, updates ...Update) string {
	t.Helper()
	out, err := Apply([]byte(before), updates...)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// replaced is before with exactly one occurrence of old replaced; the test
// fails first if old is not there exactly once.
func replaced(t *testing.T, before, old, new string) string {
	t.Helper()
	if n := strings.Count(before, old); n != 1 {
		t.Fatalf("the fixture has %d of %q", n, old)
	}
	return strings.Replace(before, old, new, 1)
}

func same(t *testing.T, got, want string) {
	t.Helper()
	if got == want {
		return
	}
	gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(gl) || i < len(wl); i++ {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			t.Fatalf("first difference at line %d:\n got: %q\nwant: %q\n\nwhole result:\n%s", i+1, g, w, got)
		}
	}
}

func valueAt(t *testing.T, raw string, path ...string) any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("the result does not parse: %v\n%s", err, raw)
	}
	var cur any = m
	for _, p := range path {
		cur = cur.(map[string]any)[p]
	}
	return cur
}

func TestApplyWithNothingChangedChangesNothing(t *testing.T) {
	before := overlay(t)
	var m map[string]any
	yaml.Unmarshal([]byte(before), &m)
	// Every top-level key set to the value it already has.
	var updates []Update
	for k, v := range m {
		updates = append(updates, Update{Key: k, Value: v})
	}
	same(t, apply(t, before, updates...), before)
}

func TestApplyChangesOneScalarLine(t *testing.T) {
	before := overlay(t)
	same(t, apply(t, before, Update{Key: "advice.weights.meta", Value: "low"}),
		replaced(t, before, "    meta: tiebreak\n", "    meta: low\n"))
}

func TestApplyKeepsTheCommentColumn(t *testing.T) {
	before := overlay(t)
	same(t, apply(t, before, Update{Key: "preferences.free_xp_max_tier", Value: 9}),
		replaced(t, before, "  free_xp_max_tier: 8                  # free XP", "  free_xp_max_tier: 9                  # free XP"))
	// A value longer than the gap pushes the comment one space along.
	got := apply(t, before, Update{Key: "constraints.playtime", Value: "limited, mostly evenings after work, sometimes weekends"})
	same(t, got, replaced(t, before, "  playtime: limited                    # one-hour",
		"  playtime: limited, mostly evenings after work, sometimes weekends # one-hour"))
}

func TestApplyKeepsContinuationComments(t *testing.T) {
	before := overlay(t)
	got := apply(t, before, Update{Key: "xp_goals", Value: []any{map[string]any{
		"target": "Tesak", "via": "Selma", "xp_required": 242560, "xp_banked": 35000,
	}}})
	same(t, got, replaced(t, before, "    xp_banked: 0                       # NOT yet read",
		"    xp_banked: 35000                   # NOT yet read"))
}

func TestApplyEditsAnInlineList(t *testing.T) {
	before := overlay(t)
	same(t, apply(t, before, Update{Key: "preferences.avoid_classes", Value: []string{"SPG", "heavyTank"}}),
		replaced(t, before, "  avoid_classes: [SPG]                 # Obj 261", "  avoid_classes: [SPG, heavyTank]      # Obj 261"))
}

func rules(t *testing.T, raw string) []any {
	t.Helper()
	return valueAt(t, raw, "advice", "rules").([]any)
}

func TestApplyEditsOneRule(t *testing.T) {
	before := overlay(t)
	rs := rules(t, before)
	rs[1].(map[string]any)["text"] = "Autoloaders are context only."
	got := apply(t, before, Update{Key: "advice.rules", Value: rs})
	same(t, got, replaced(t, before,
		"    - text: Autoloaders are context, not a criterion. Mention when a candidate is one, but never rank a tank higher for it.\n",
		"    - text: Autoloaders are context only.\n"))
}

func TestApplyAddsDeletesAndReordersRules(t *testing.T) {
	before := overlay(t)
	rs := rules(t, before)
	first := "    - text: A session is one hour, set by Personal Reserves; sometimes two back to back, rarely longer. Offer a two-hour option only as the exception.\n      added: 2026-09-18\n"
	second := "    - text: Autoloaders are context, not a criterion. Mention when a candidate is one, but never rank a tank higher for it.\n      added: 2026-09-18\n"

	// Callers pass structs, whose field order yaml.v3 keeps; a map's it sorts.
	type rule struct {
		Text  string `yaml:"text"`
		Added string `yaml:"added"`
	}
	added := append(rules(t, before), rule{"Keep answers short.", "2026-09-27"})
	got := apply(t, before, Update{Key: "advice.rules", Value: added})
	same(t, got, replaced(t, before, "      added: 2026-09-18\n\nnotes",
		"      added: 2026-09-18\n    - text: Keep answers short.\n      added: \"2026-09-27\"\n\nnotes"))

	got = apply(t, before, Update{Key: "advice.rules", Value: rs[1:]})
	same(t, got, replaced(t, before, first, ""))

	swapped := append([]any{rs[1], rs[0]}, rs[2:]...)
	got = apply(t, before, Update{Key: "advice.rules", Value: swapped})
	same(t, got, replaced(t, before, first+second, second+first))
}

func TestApplyKeepsTheCommentAboveAListItem(t *testing.T) {
	before := overlay(t)
	goals := valueAt(t, before, "xp_goals").([]any)
	extra := map[string]any{"target": "Concept 5", "xp_required": 100000}
	got := apply(t, before, Update{Key: "xp_goals", Value: append(goals, extra)})
	if !strings.Contains(got, "  # The Šelma step was completed") || !strings.Contains(got, "  - target: Tesak                      # Vz. 71") {
		t.Errorf("the first goal lost its comments:\n%s", got)
	}
	if n := len(valueAt(t, got, "xp_goals").([]any)); n != 2 {
		t.Errorf("%d goals, want 2", n)
	}
}

func TestApplyAddsAndRemovesKeys(t *testing.T) {
	before := overlay(t)
	got := apply(t, before, Update{Key: "advice.answer.length", Value: "short"},
		Update{Key: "advice.answer.explain_basics"})
	same(t, got, replaced(t, before, "    length: normal\n    format: sections\n    explain_basics: never\n",
		"    length: short\n    format: sections\n"))

	got = apply(t, before, Update{Key: "profile.battles_per_hour"})
	same(t, got, replaced(t, before, "  battles_per_hour: 8                  # an estimate; replace with a measured rate\n", ""))

	// A key the mapping lacks goes after its last key.
	got = apply(t, before, Update{Key: "constraints.max_tier", Value: 10})
	same(t, got, replaced(t, before, "  credit_buffer: 500000                # credits left after a purchase for it to count affordable\n",
		"  credit_buffer: 500000                # credits left after a purchase for it to count affordable\n  max_tier: 10\n"))
}

func TestApplyAddsASectionToAFileWithoutOne(t *testing.T) {
	before := "# mine\nversion: 1   # keep\n\nnotes: []\n"
	got := apply(t, before, Update{Key: "advice.weights.meta", Value: "low"})
	same(t, got, before+"\nadvice:\n  weights:\n    meta: low\n")
	if valueAt(t, got, "advice", "weights", "meta") != "low" {
		t.Error("wrong value")
	}
}

func TestApplyReplacesAMappingKeyByKey(t *testing.T) {
	before := overlay(t)
	weights := valueAt(t, before, "advice", "weights").(map[string]any)
	weights["credits"] = "high"
	delete(weights, "goals")
	got := apply(t, before, Update{Key: "advice.weights", Value: weights})
	same(t, got, replaced(t, before, "    credits: medium\n    earning: low\n    meta: tiebreak\n    goals: tiebreak\n",
		"    credits: high\n    earning: low\n    meta: tiebreak\n"))
}

func TestApplyTurnsAnEmptyListIntoItems(t *testing.T) {
	before := "version: 1\nnotes: []            # free text\nlast: 1\n"
	got := apply(t, before, Update{Key: "notes", Value: []string{"one"}})
	same(t, got, "version: 1\nnotes: [one]         # free text\nlast: 1\n")
	got = apply(t, before, Update{Key: "notes", Value: []any{map[string]any{"a": 1}}})
	same(t, got, "version: 1\nnotes:               # free text\n  - a: 1\nlast: 1\n")
}

func TestApplyKeepsWindowsLineEndings(t *testing.T) {
	before := "version: 1\r\nname: a   # c\r\n"
	same(t, apply(t, before, Update{Key: "name", Value: "b"}), "version: 1\r\nname: b   # c\r\n")
}

func TestApplyStartsAMissingFile(t *testing.T) {
	got := apply(t, "", Update{Key: "a.b", Value: 1})
	if !reflect.DeepEqual(valueAt(t, got, "a", "b"), 1) {
		t.Errorf("got %q", got)
	}
}
