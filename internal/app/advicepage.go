package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/advice"
	"github.com/ondrejkouril/tank-advisor/internal/overlay"
	"github.com/ondrejkouril/tank-advisor/internal/yamledit"
)

// AdviceSettings are the Advice page's fields (docs/spec-desktop.md section
// 8.4). Each holds exactly what the overlay sets, and its zero value where the
// overlay sets nothing, so the default applies. Saving a page that was not
// changed therefore changes nothing in the file.
type AdviceSettings struct {
	// About you.
	SessionMinutes   int            `json:"sessionMinutes"`
	BattlesPerHour   int            `json:"battlesPerHour"`
	Experience       string         `json:"experience"`
	ClassRank        map[string]int `json:"classRank"`
	AvoidClasses     []string       `json:"avoidClasses"`
	ImprovementFocus []string       `json:"improvementFocus"`
	Strengths        []string       `json:"strengths"`
	// What matters when choosing.
	Weights       map[string]string `json:"weights"`
	FreeXPMaxTier int               `json:"freeXPMaxTier"`
	CreditBuffer  int               `json:"creditBuffer"`
	// How answers look.
	Length           string `json:"length"`
	Format           string `json:"format"`
	ExplainBasics    string `json:"explainBasics"`
	AlsoWorthKnowing *bool  `json:"alsoWorthKnowing"`
	Coaching         *bool  `json:"coaching"`
	// Your rules.
	Rules []RuleSetting `json:"rules"`
}

// RuleSetting is one of the player's rules. Its field order is the file's.
type RuleSetting struct {
	Text  string `json:"text" yaml:"text"`
	Added string `json:"added" yaml:"added"`
}

// AdvicePage is everything the Advice page shows.
type AdvicePage struct {
	Path string `json:"path"`
	// Version identifies the file the page loaded; a save against another
	// version is refused (section 5.5).
	Version  string         `json:"version"`
	Problem  string         `json:"problem,omitempty"`
	Settings AdviceSettings `json:"settings"`
	// Defaults are what applies where nothing is set.
	Defaults AdviceSettings `json:"defaults"`
	Options  AdviceOptions  `json:"options"`
	Preview  AdvicePreview  `json:"preview"`
}

// AdviceOptions are the values each field offers.
type AdviceOptions struct {
	Classes       []Choice `json:"classes"`
	Factors       []Choice `json:"factors"`
	Levels        []Choice `json:"levels"`
	Experiences   []Choice `json:"experiences"`
	Lengths       []Choice `json:"lengths"`
	Formats       []Choice `json:"formats"`
	Basics        []Choice `json:"basics"`
	MaxRules      int      `json:"maxRules"`
	MaxRuleLength int      `json:"maxRuleLength"`
}

// AdvicePreview is "What Claude reads": the rendered Your advice section for
// the settings on the page, saved or not, and what is wrong with them.
type AdvicePreview struct {
	Text      string            `json:"text"`
	Conflicts []advice.Conflict `json:"conflicts"`
	Errors    []string          `json:"errors"`
}

var adviceOptions = AdviceOptions{
	Classes: classChoices,
	Factors: []Choice{
		{"fit", "Fit to you"}, {"time", "Time to get it"}, {"credits", "Credit cost"},
		{"earning", "Earning once owned"}, {"meta", "Strength in the meta"}, {"goals", "Your XP goals"},
	},
	Levels: []Choice{
		{"highest", "Highest"}, {"high", "High"}, {"medium", "Medium"}, {"low", "Low"},
		{"tiebreak", "Tie-break"}, {"ignore", "Ignore"},
	},
	Experiences: []Choice{{"auto", "From my statistics"}, {"new", "New"}, {"returning", "Returning"}, {"experienced", "Experienced"}},
	Lengths:     []Choice{{"short", "Short"}, {"normal", "Normal"}, {"detailed", "Detailed"}},
	Formats:     []Choice{{"sections", "Sections"}, {"plain", "Plain paragraphs"}},
	Basics:      []Choice{{"auto", "From my experience"}, {"always", "Always"}, {"never", "Never"}},
	MaxRules:    overlay.MaxRules, MaxRuleLength: overlay.MaxRuleLength,
}

func adviceDefaults() AdviceSettings {
	also, coaching := advice.DefaultAlsoLine, advice.DefaultCoaching
	return AdviceSettings{
		SessionMinutes: advice.DefaultSessionMinutes, BattlesPerHour: advice.DefaultBattlesPerHour,
		Experience: advice.DefaultExperience, Weights: advice.DefaultWeights,
		Length: advice.DefaultLength, Format: advice.DefaultFormat, ExplainBasics: advice.DefaultBasics,
		AlsoWorthKnowing: &also, Coaching: &coaching,
		ClassRank: map[string]int{}, AvoidClasses: []string{}, ImprovementFocus: []string{}, Strengths: []string{}, Rules: []RuleSetting{},
	}
}

// settingsOf reads the page's fields out of an overlay.
func settingsOf(o *overlay.Overlay) AdviceSettings {
	s := AdviceSettings{
		SessionMinutes: o.Profile.SessionMinutes, BattlesPerHour: o.Profile.BattlesPerHour, Experience: o.Profile.Experience,
		ClassRank: o.Preferences.ClassRank, AvoidClasses: o.Preferences.AvoidClasses,
		ImprovementFocus: o.Preferences.ImprovementFocus, Strengths: o.Preferences.Strengths,
		Weights: o.Advice.Weights, FreeXPMaxTier: o.Preferences.FreeXPMaxTier, CreditBuffer: o.Constraints.CreditBuffer,
		Length: o.Advice.Answer.Length, Format: o.Advice.Answer.Format, ExplainBasics: o.Advice.Answer.ExplainBasics,
		AlsoWorthKnowing: o.Advice.Answer.AlsoWorthKnowing, Coaching: o.Advice.Coaching,
	}
	for _, r := range o.Advice.Rules {
		s.Rules = append(s.Rules, RuleSetting{Text: r.Text, Added: r.Added})
	}
	// JSON arrays and objects rather than null, for the page.
	if s.ClassRank == nil {
		s.ClassRank = map[string]int{}
	}
	if s.Weights == nil {
		s.Weights = map[string]string{}
	}
	for _, l := range []*[]string{&s.AvoidClasses, &s.ImprovementFocus, &s.Strengths} {
		if *l == nil {
			*l = []string{}
		}
	}
	if s.Rules == nil {
		s.Rules = []RuleSetting{}
	}
	return s
}

// updates turns the page's fields into overlay updates. An empty field
// removes its key, so the default applies again.
func (a AdviceSettings) updates() []yamledit.Update {
	orNil := func(v any, empty bool) any {
		if empty {
			return nil
		}
		return v
	}
	var strengths []string
	for _, s := range a.Strengths {
		if s = strings.TrimSpace(s); s != "" {
			strengths = append(strengths, s)
		}
	}
	var rules []RuleSetting
	for _, r := range a.Rules {
		if r.Text = strings.TrimSpace(r.Text); r.Text == "" {
			continue
		}
		if r.Added == "" {
			r.Added = time.Now().Format(time.DateOnly)
		}
		rules = append(rules, r)
	}
	weights := map[string]string{}
	for f, l := range a.Weights {
		if l != "" {
			weights[f] = l
		}
	}
	ranks := map[string]int{}
	for c, r := range a.ClassRank {
		if r > 0 {
			ranks[c] = r
		}
	}
	return []yamledit.Update{
		{Key: "profile.session_minutes", Value: orNil(a.SessionMinutes, a.SessionMinutes == 0)},
		{Key: "profile.battles_per_hour", Value: orNil(a.BattlesPerHour, a.BattlesPerHour == 0)},
		{Key: "profile.experience", Value: orNil(a.Experience, a.Experience == "")},
		{Key: "preferences.class_rank", Value: orNil(ranks, len(ranks) == 0)},
		{Key: "preferences.avoid_classes", Value: orNil(a.AvoidClasses, len(a.AvoidClasses) == 0)},
		{Key: "preferences.improvement_focus", Value: orNil(a.ImprovementFocus, len(a.ImprovementFocus) == 0)},
		{Key: "preferences.strengths", Value: orNil(strengths, len(strengths) == 0)},
		{Key: "preferences.free_xp_max_tier", Value: orNil(a.FreeXPMaxTier, a.FreeXPMaxTier == 0)},
		{Key: "constraints.credit_buffer", Value: orNil(a.CreditBuffer, a.CreditBuffer == 0)},
		{Key: "advice.weights", Value: orNil(weights, len(weights) == 0)},
		{Key: "advice.answer.length", Value: orNil(a.Length, a.Length == "")},
		{Key: "advice.answer.format", Value: orNil(a.Format, a.Format == "")},
		{Key: "advice.answer.explain_basics", Value: orNil(a.ExplainBasics, a.ExplainBasics == "")},
		{Key: "advice.answer.also_worth_knowing", Value: orNil(a.AlsoWorthKnowing, a.AlsoWorthKnowing == nil)},
		{Key: "advice.coaching", Value: orNil(a.Coaching, a.Coaching == nil)},
		{Key: "advice.rules", Value: orNil(rules, len(rules) == 0)},
	}
}

// AdvicePage loads the Advice page.
func (s *Service) AdvicePage(context.Context) AdvicePage {
	_, f := s.loadOverlay()
	page := AdvicePage{Path: f.path, Version: f.version, Problem: f.problem, Defaults: adviceDefaults(), Options: adviceOptions}
	page.Settings = settingsOf(f.parsed)
	page.Preview = s.previewFrom(f, page.Settings)
	return page
}

// PreviewAdvice renders what Claude would read with these settings, from the
// very file a save would write, without writing it.
func (s *Service) PreviewAdvice(_ context.Context, settings AdviceSettings) AdvicePreview {
	_, f := s.loadOverlay()
	return s.previewFrom(f, settings)
}

func (s *Service) previewFrom(f overlayFile, settings AdviceSettings) AdvicePreview {
	pv := AdvicePreview{Conflicts: []advice.Conflict{}, Errors: []string{}}
	if f.problem != "" {
		pv.Errors = []string{f.problem}
		pv.Text = advice.Render(f.parsed)
		return pv
	}
	p := s.prepareOverlay(f, settings.updates())
	if len(p.errs) > 0 {
		pv.Errors = p.errs
	}
	o := p.parsed
	if o == nil {
		o = f.parsed
	}
	pv.Text = advice.Render(o)
	pv.Conflicts = advice.Conflicts(o)
	if pv.Conflicts == nil {
		pv.Conflicts = []advice.Conflict{}
	}
	return pv
}

// saveAdvicePage is the page's Save. arg is {"version": ..., "settings": ...}.
func (s *Service) saveAdvicePage(ctx context.Context, arg string) Result {
	var req struct {
		Version  string         `json:"version"`
		Settings AdviceSettings `json:"settings"`
	}
	if err := json.Unmarshal([]byte(arg), &req); err != nil {
		return Result{Message: "unreadable settings: " + err.Error()}
	}
	for _, c := range req.Settings.AvoidClasses {
		if !slices.ContainsFunc(classChoices, func(ch Choice) bool { return ch.Value == c }) {
			return Result{Message: c + " is not a class"}
		}
	}
	return s.saveOverlay(ctx, req.Version, req.Settings.updates(), nil)
}
