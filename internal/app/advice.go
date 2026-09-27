package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/overlay"
	"github.com/ondrejkouril/tank-advisor/internal/yamledit"
)

// Question is one question of the setup's short advice questionnaire
// (docs/spec-desktop.md section 5.1, step 7). The Advice page (plan step D7)
// offers every setting; these are the few that change answers most.
type Question struct {
	// Key is the overlay key it sets, dotted.
	Key     string   `json:"key"`
	Text    string   `json:"text"`
	Help    string   `json:"help,omitempty"`
	Kind    string   `json:"kind"` // choice | number | multi
	Choices []Choice `json:"choices,omitempty"`
	Min     int      `json:"min,omitempty"`
	Max     int      `json:"max,omitempty"`
	// Value is the current setting, or the default when Set is false.
	Value any  `json:"value"`
	Set   bool `json:"set"`
}

// classChoices are the classes as the API names them, in plain words.
var classChoices = []Choice{
	{"lightTank", "Light tanks"}, {"mediumTank", "Medium tanks"}, {"heavyTank", "Heavy tanks"},
	{"AT-SPG", "Tank destroyers"}, {"SPG", "Artillery"},
}

// questions builds the questionnaire from the overlay at path, with the
// current values filled in. A missing or unreadable overlay gives defaults.
func questions(path string) []Question {
	var o overlay.Overlay
	if raw, err := os.ReadFile(path); err == nil {
		if parsed, _ := overlay.Parse(raw); parsed != nil {
			o = *parsed
		}
	}
	qs := []Question{
		{Key: "profile.session_minutes", Text: "How long is a typical session?", Kind: "choice",
			Help:    "Time estimates are given in sessions of this length.",
			Choices: []Choice{{"30", "Half an hour"}, {"60", "An hour"}, {"90", "An hour and a half"}, {"120", "Two hours"}, {"180", "Three hours or more"}},
			Value:   "60"},
		{Key: "profile.battles_per_hour", Text: "About how many battles do you play in an hour?", Kind: "number",
			Help: "Eight is typical. It turns battles into time.", Min: 1, Max: 30, Value: 8},
		{Key: "profile.experience", Text: "How experienced are you?", Kind: "choice",
			Help: "Let your statistics decide, unless they mislead: after a long break, say.",
			Choices: []Choice{{"auto", "Decide from my statistics"}, {"new", "New to the game"},
				{"returning", "Returning after a break"}, {"experienced", "Experienced"}},
			Value: "auto"},
		{Key: "preferences.avoid_classes", Text: "Which classes should never be recommended?", Kind: "multi",
			Help: "Leave all unticked if you play everything.", Choices: classChoices, Value: []string{}},
		{Key: "advice.answer.length", Text: "How long should answers be?", Kind: "choice",
			Choices: []Choice{{"short", "Short"}, {"normal", "Normal"}, {"detailed", "Detailed"}},
			Value:   "normal"},
		{Key: "advice.answer.format", Text: "How should answers be laid out?", Kind: "choice",
			Choices: []Choice{{"sections", "In sections: short answer, why, best setup, watch-outs, verdict"}, {"plain", "As plain paragraphs"}},
			Value:   "sections"},
	}
	current := map[string]any{}
	if o.Profile.SessionMinutes > 0 {
		current["profile.session_minutes"] = strconv.Itoa(o.Profile.SessionMinutes)
	}
	if o.Profile.BattlesPerHour > 0 {
		current["profile.battles_per_hour"] = o.Profile.BattlesPerHour
	}
	if o.Profile.Experience != "" {
		current["profile.experience"] = o.Profile.Experience
	}
	if len(o.Preferences.AvoidClasses) > 0 {
		current["preferences.avoid_classes"] = o.Preferences.AvoidClasses
	}
	if o.Advice.Answer.Length != "" {
		current["advice.answer.length"] = o.Advice.Answer.Length
	}
	if o.Advice.Answer.Format != "" {
		current["advice.answer.format"] = o.Advice.Answer.Format
	}
	for i := range qs {
		if v, ok := current[qs[i].Key]; ok {
			qs[i].Value, qs[i].Set = v, true
			// A session length the list does not offer is still shown.
			if qs[i].Kind == "choice" && !hasChoice(qs[i].Choices, fmt.Sprint(v)) {
				qs[i].Choices = append(qs[i].Choices, Choice{fmt.Sprint(v), fmt.Sprint(v)})
			}
		}
	}
	return qs
}

func hasChoice(cs []Choice, v string) bool {
	return slices.ContainsFunc(cs, func(c Choice) bool { return c.Value == v })
}

// saveAdvice writes the answered questions into the overlay. arg is a JSON
// object of key to answer; a question left out is skipped and keeps its
// current setting. The overlay is written through yaml.v3's nodes, so the
// player's comments and key order survive, and a result that does not
// validate is never written (docs/spec-desktop.md section 5.5).
func (s *Service) saveAdvice(arg string) Result {
	var answers map[string]any
	if err := json.Unmarshal([]byte(arg), &answers); err != nil {
		return Result{Message: "unreadable answers: " + err.Error()}
	}
	env := s.opts.NewEnv(io.Discard, io.Discard)
	path := env.Config.OverlayFile(env.Paths)

	var updates []yamledit.Update
	for _, q := range questions(path) {
		raw, ok := answers[q.Key]
		if !ok {
			continue
		}
		v, err := q.accept(raw)
		if err != nil {
			return Result{Message: err.Error()}
		}
		updates = append(updates, yamledit.Update{Key: q.Key, Value: v})
	}
	if len(updates) == 0 {
		return Result{OK: true, Message: "Nothing changed: the defaults apply."}
	}
	// version and updated_at lead a new file, as in a hand-written one.
	head := []yamledit.Update{}
	if !hasKey(path, "version") {
		head = append(head, yamledit.Update{Key: "version", Value: overlay.SchemaVersion})
	}
	head = append(head, yamledit.Update{Key: "updated_at", Value: time.Now().UTC().Format(time.RFC3339)})
	updates = append(head, updates...)

	err := yamledit.Set(path, func(tmp string) error {
		_, findings, err := overlay.Load(tmp)
		if err != nil {
			return err
		}
		var errs []string
		for _, f := range findings {
			if f.Severity == overlay.SeverityError {
				errs = append(errs, f.String())
			}
		}
		if len(errs) > 0 {
			return errors.New("your settings file has errors, so it was left as it is: " + strings.Join(errs, "; "))
		}
		return nil
	}, updates...)
	if err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true, Message: "Saved to " + path + "."}
}

// accept checks one answer and returns it as the overlay stores it.
func (q Question) accept(raw any) (any, error) {
	switch q.Kind {
	case "choice":
		v := fmt.Sprint(raw)
		if !hasChoice(q.Choices, v) {
			return nil, fmt.Errorf("%q is not an answer to %q", v, q.Text)
		}
		if n, err := strconv.Atoi(v); err == nil {
			return n, nil
		}
		return v, nil
	case "number":
		var n int
		switch x := raw.(type) {
		case float64:
			n = int(x)
		case string:
			var err error
			if n, err = strconv.Atoi(strings.TrimSpace(x)); err != nil {
				return nil, fmt.Errorf("%q needs a whole number", q.Text)
			}
		default:
			return nil, fmt.Errorf("%q needs a whole number", q.Text)
		}
		if n < q.Min || n > q.Max {
			return nil, fmt.Errorf("%q needs a number from %d to %d", q.Text, q.Min, q.Max)
		}
		return n, nil
	case "multi":
		list, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("%q needs a list", q.Text)
		}
		out := []string{}
		for _, item := range list {
			v := fmt.Sprint(item)
			if !hasChoice(q.Choices, v) {
				return nil, fmt.Errorf("%q is not an answer to %q", v, q.Text)
			}
			out = append(out, v)
		}
		if len(out) == 0 {
			return nil, nil // nothing avoided: remove the key
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown question kind %s", q.Kind)
}
