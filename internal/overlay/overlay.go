// Package overlay loads and validates wot-overlay.yaml, the hand-edited file
// holding what no API exposes: premium status, researched-but-unbought tanks,
// XP goals and preferences (docs/spec.md section 5).
//
// Validation happens in two passes. Parse checks the file against the schema
// and needs nothing else. Resolve checks it against the synced data: every tank
// reference must name exactly one vehicle, and entries the garage has overtaken
// are reported as stale. The overlay is meant to drift, so staleness is a
// warning to act on, never an error.
package overlay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Now is the clock the future-timestamp check reads; tests replace it.
var Now = time.Now

// SchemaVersion is the only overlay version this build reads.
const SchemaVersion = 1

// Overlay is the contents of wot-overlay.yaml.
type Overlay struct {
	Version int `yaml:"version"`

	// UpdatedAtRaw is kept as written so a parse failure can quote it.
	// UpdatedAt is the parsed value and becomes meta.overlay_version.
	UpdatedAtRaw string    `yaml:"updated_at"`
	UpdatedAt    time.Time `yaml:"-"`

	// Premium is authoritative over the API's is_premium, which is wrong for
	// this account (docs/spec.md section 3.1). Nil means unknown, never false.
	Premium *Premium `yaml:"premium"`

	ResearchedNotBought []TankRef      `yaml:"researched_not_bought"`
	XPGoals             []XPGoal       `yaml:"xp_goals"`
	ResearchPaths       []ResearchPath `yaml:"research_paths"`
	Preferences         Preferences    `yaml:"preferences"`
	Constraints         Constraints    `yaml:"constraints"`
	Profile             Profile        `yaml:"profile"`
	Advice              Advice         `yaml:"advice"`
	Notes               []string       `yaml:"notes"`
}

// Profile describes the player, for the advice (docs/spec-desktop.md section
// 8.3). Zero values mean "not set": the default applies.
type Profile struct {
	// SessionMinutes is how long one sitting usually is: the unit every time
	// estimate is stated in.
	SessionMinutes int `yaml:"session_minutes"`
	// BattlesPerHour converts battles into time.
	BattlesPerHour int `yaml:"battles_per_hour"`
	// Experience is auto, new, returning or experienced. Auto decides from the
	// data, with thresholds in the framework rather than the code.
	Experience string `yaml:"experience"`
}

// Advice is how the player wants to be advised. Zero values mean the default.
type Advice struct {
	// Weights says how much each factor counts when choosing what to buy or
	// research next (framework section 4.1). Keys are WeightFactors, values
	// WeightLevels.
	Weights map[string]string `yaml:"weights"`
	Answer  AnswerStyle       `yaml:"answer"`
	// Coaching turns on a coaching angle, and unasked progress notes, for the
	// classes in preferences.improvement_focus. Nil means on.
	Coaching *bool `yaml:"coaching"`
	// Rules are the player's own, in their own words.
	Rules []Rule `yaml:"rules"`
}

// AnswerStyle shapes the answers. Empty strings and nil mean the default.
type AnswerStyle struct {
	Length           string `yaml:"length"`         // short | normal | detailed
	Format           string `yaml:"format"`         // sections | plain
	ExplainBasics    string `yaml:"explain_basics"` // auto | always | never
	AlsoWorthKnowing *bool  `yaml:"also_worth_knowing"`
}

// Rule is one of the player's own rules.
type Rule struct {
	Text  string `yaml:"text"`
	Added string `yaml:"added"`
}

// The advice settings' allowed values, in the order the app offers them.
var (
	WeightFactors   = []string{"fit", "time", "credits", "earning", "meta", "goals"}
	WeightLevels    = []string{"highest", "high", "medium", "low", "tiebreak", "ignore"}
	Experiences     = []string{"auto", "new", "returning", "experienced"}
	AnswerLengths   = []string{"short", "normal", "detailed"}
	AnswerFormats   = []string{"sections", "plain"}
	BasicsSettings  = []string{"auto", "always", "never"}
	MaxRules        = 20
	MaxRuleLength   = 300
	sessionMinRange = [2]int{10, 600}
	battlesPerHour  = [2]int{1, 30}
)

// Premium records which premium products the account holds. Each field is a
// pointer because "not stated" must stay distinguishable from "no": an answer
// that assumed no premium would understate every credit and XP figure.
type Premium struct {
	PremiumAccount *bool `yaml:"premium_account"`
	WoTPlus        *bool `yaml:"wot_plus"`
}

// XPGoal is a vehicle being ground towards.
type XPGoal struct {
	Target TankRef `yaml:"target"`
	// Via is the vehicle the target is researched from. Optional.
	Via        TankRef `yaml:"via"`
	XPRequired *int    `yaml:"xp_required"`
	// XPBanked is maintained by hand: no API exposes per-vehicle XP. Nil means
	// unknown, and must be reported that way rather than as zero.
	XPBanked *int `yaml:"xp_banked"`

	// APIXPCost is the research cost the tech tree reports for Via -> Target,
	// filled in by Resolve when that edge exists.
	APIXPCost *int `yaml:"-"`
}

// ResearchPath is a tech-tree edge the API does not supply. Tier XI turned out
// to be exposed, so this is a fallback for a future gap rather than something
// any current line depends on.
type ResearchPath struct {
	From   TankRef `yaml:"from"`
	To     TankRef `yaml:"to"`
	XPCost int     `yaml:"xp_cost"`
}

// Preferences steer recommendations.
type Preferences struct {
	// AvoidClasses holds vehicle types as the API names them (see Classes),
	// canonicalised by Parse. Candidates of these classes are filtered out.
	AvoidClasses []string `yaml:"avoid_classes"`
	Strengths    []string `yaml:"strengths"`

	// ClassRank orders the classes the player likes: 1 is most preferred,
	// and classes may share a rank. Canonicalised by Parse. Absent classes
	// are unranked, not disliked.
	ClassRank map[string]int `yaml:"class_rank"`

	// FreeXPMaxTier is the highest tier the player spends free XP on to
	// research a tank; 0 means no limit. Modules are not affected.
	FreeXPMaxTier int `yaml:"free_xp_max_tier"`

	// ImprovementFocus names classes the player is working to get better at.
	ImprovementFocus []string `yaml:"improvement_focus"`
}

// Constraints bound what a recommendation may ask of the player.
type Constraints struct {
	Playtime string `yaml:"playtime"`

	// CreditBuffer is how many credits must remain after a purchase for it to
	// count as affordable, for equipment and consumables on the new tank.
	CreditBuffer int `yaml:"credit_buffer"`
}

// TankRef is a reference to a vehicle by name or tank_id.
//
// YAML decides which: an unquoted integer is a tank_id, anything else a name.
// Resolve fills in the vehicle it refers to.
type TankRef struct {
	// Input is the reference as written.
	Input string
	// ID is the tank_id: given directly for a numeric reference, otherwise set
	// by Resolve.
	ID int

	// Set by Resolve.
	Resolved bool
	Name     string
	Tier     int
	Type     string
	// Owned reports that the vehicle is in the latest garage.
	Owned bool

	numeric bool
	line    int
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (r *TankRef) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: a tank reference must be a name or a tank_id", node.Line)
	}
	r.line = node.Line
	if node.Tag == "!!null" {
		return nil
	}
	r.Input = strings.TrimSpace(node.Value)
	if node.Tag == "!!int" {
		id, err := strconv.Atoi(node.Value)
		if err != nil {
			return fmt.Errorf("line %d: tank_id %q is not a valid integer", node.Line, node.Value)
		}
		r.ID = id
		r.numeric = true
	}
	return nil
}

// IsZero reports whether the reference was left empty.
func (r TankRef) IsZero() bool {
	return r.Input == ""
}

// String renders the reference for a person: the resolved name where known,
// otherwise what was written.
// IsID reports whether the reference was written as a tank_id rather than a
// name.
func (r TankRef) IsID() bool { return r.numeric }

func (r TankRef) String() string {
	if r.Resolved {
		return r.Name
	}
	return r.Input
}

// Classes are the vehicle types the API reports, which avoid_classes is
// checked against. An unrecognised class would make the candidates filter
// silently do nothing, so it is an error rather than ignored.
var Classes = []string{"lightTank", "mediumTank", "heavyTank", "AT-SPG", "SPG"}

// classAliases maps the common abbreviations onto the API's names.
var classAliases = map[string]string{
	"lt": "lightTank", "light": "lightTank",
	"mt": "mediumTank", "medium": "mediumTank",
	"ht": "heavyTank", "heavy": "heavyTank",
	"td": "AT-SPG", "tank destroyer": "AT-SPG",
	"arty": "SPG", "artillery": "SPG",
}

// CanonicalClass returns the API name for a class, accepting the common
// abbreviations (LT, MT, HT, TD, arty), or "" if it is unknown.
func CanonicalClass(s string) string {
	trimmed := strings.TrimSpace(s)
	for _, c := range Classes {
		if strings.EqualFold(c, trimmed) {
			return c
		}
	}
	return classAliases[strings.ToLower(trimmed)]
}

// Severity grades a finding.
type Severity string

const (
	// SeverityError means the overlay cannot be trusted as written.
	SeverityError Severity = "error"
	// SeverityWarning means it can be used, but something needs attention.
	SeverityWarning Severity = "warning"
)

// Finding is one validation result.
type Finding struct {
	Severity Severity `json:"severity"`
	// Field locates the finding, e.g. "researched_not_bought[4]".
	Field string `json:"field,omitempty"`
	// Line is the line in the file, or 0 when not known.
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

func (f Finding) String() string {
	var where []string
	if f.Field != "" {
		where = append(where, f.Field)
	}
	if f.Line > 0 {
		where = append(where, fmt.Sprintf("line %d", f.Line))
	}
	if len(where) == 0 {
		return fmt.Sprintf("%s: %s", f.Severity, f.Message)
	}
	return fmt.Sprintf("%s: %s: %s", f.Severity, strings.Join(where, ", "), f.Message)
}

// Load reads and parses the overlay at path. A missing file is returned as an
// error wrapping os.ErrNotExist, so callers can tell "no overlay yet" from a
// broken one.
func Load(path string) (*Overlay, []Finding, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	o, findings := Parse(raw)
	return o, findings, nil
}

// Parse decodes an overlay and checks it against the schema. It needs no
// synced data. The returned overlay is nil only when the file could not be
// decoded at all.
func Parse(raw []byte) (*Overlay, []Finding) {
	var o Overlay
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true) // a typo'd key is an error, not a silently ignored one
	if err := dec.Decode(&o); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, []Finding{{Severity: SeverityError, Message: "the file is empty"}}
		}
		return nil, decodeFindings(err)
	}

	var findings []Finding
	add := func(sev Severity, field string, line int, format string, args ...any) {
		findings = append(findings, Finding{Severity: sev, Field: field, Line: line, Message: fmt.Sprintf(format, args...)})
	}

	switch o.Version {
	case SchemaVersion:
	case 0:
		add(SeverityError, "version", 0, "required; this build reads version %d", SchemaVersion)
	default:
		add(SeverityError, "version", 0, "unsupported version %d; this build reads version %d", o.Version, SchemaVersion)
	}

	if o.UpdatedAtRaw == "" {
		add(SeverityError, "updated_at", 0, "required; it becomes the overlay version quoted with every answer")
	} else if t, err := parseTimestamp(o.UpdatedAtRaw); err != nil {
		add(SeverityError, "updated_at", 0, "%q is not a timestamp such as 2026-09-17T18:02:00Z", o.UpdatedAtRaw)
	} else {
		o.UpdatedAt = t
		// A future stamp makes every answer claim a newer overlay than exists,
		// and hides whether banked XP is stale. An hour of slack covers a
		// timestamp written in local time with a Z by mistake.
		if t.After(Now().Add(time.Hour)) {
			add(SeverityWarning, "updated_at", 0, "%s is in the future; use the current UTC time", o.UpdatedAtRaw)
		}
	}

	switch {
	case o.Premium == nil:
		add(SeverityWarning, "premium", 0, "absent, so premium status will be reported as unknown")
	default:
		if o.Premium.PremiumAccount == nil {
			add(SeverityWarning, "premium.premium_account", 0, "absent, so Premium Account status will be reported as unknown")
		}
		if o.Premium.WoTPlus == nil {
			add(SeverityWarning, "premium.wot_plus", 0, "absent, so WoT Plus status will be reported as unknown")
		}
	}

	for i, ref := range o.ResearchedNotBought {
		if ref.IsZero() {
			add(SeverityError, fmt.Sprintf("researched_not_bought[%d]", i), ref.line, "empty entry")
		}
	}

	for i, g := range o.XPGoals {
		field := fmt.Sprintf("xp_goals[%d]", i)
		if g.Target.IsZero() {
			add(SeverityError, field+".target", g.Target.line, "required")
		}
		if g.XPRequired != nil && *g.XPRequired < 0 {
			add(SeverityError, field+".xp_required", 0, "must not be negative, got %d", *g.XPRequired)
		}
		if g.XPBanked != nil && *g.XPBanked < 0 {
			add(SeverityError, field+".xp_banked", 0, "must not be negative, got %d", *g.XPBanked)
		}
		if g.XPRequired != nil && g.XPBanked != nil && *g.XPBanked > *g.XPRequired {
			add(SeverityWarning, field+".xp_banked", 0,
				"%d banked exceeds the %d required; the goal looks reachable already", *g.XPBanked, *g.XPRequired)
		}
	}

	for i, p := range o.ResearchPaths {
		field := fmt.Sprintf("research_paths[%d]", i)
		if p.From.IsZero() {
			add(SeverityError, field+".from", p.From.line, "required")
		}
		if p.To.IsZero() {
			add(SeverityError, field+".to", p.To.line, "required")
		}
		if p.XPCost <= 0 {
			add(SeverityError, field+".xp_cost", 0, "must be positive, got %d", p.XPCost)
		}
	}

	for i, c := range o.Preferences.AvoidClasses {
		canonical := CanonicalClass(c)
		if canonical == "" {
			add(SeverityError, fmt.Sprintf("preferences.avoid_classes[%d]", i), 0,
				"unknown class %q; want one of %s", c, strings.Join(Classes, ", "))
			continue
		}
		o.Preferences.AvoidClasses[i] = canonical
	}

	if len(o.Preferences.ClassRank) > 0 {
		ranks := make(map[string]int, len(o.Preferences.ClassRank))
		for c, rank := range o.Preferences.ClassRank {
			canonical := CanonicalClass(c)
			switch {
			case canonical == "":
				add(SeverityError, "preferences.class_rank", 0,
					"unknown class %q; want one of %s", c, strings.Join(Classes, ", "))
			case rank < 1:
				add(SeverityError, "preferences.class_rank", 0, "rank for %s must be 1 or more, got %d", c, rank)
			default:
				ranks[canonical] = rank
			}
		}
		o.Preferences.ClassRank = ranks
		for _, avoided := range o.Preferences.AvoidClasses {
			if _, ok := ranks[avoided]; ok {
				add(SeverityWarning, "preferences.class_rank", 0,
					"%s is both ranked and in avoid_classes; avoid_classes wins", avoided)
			}
		}
	}

	if t := o.Preferences.FreeXPMaxTier; t < 0 || t > 11 {
		add(SeverityError, "preferences.free_xp_max_tier", 0, "must be 0 (no limit) to 11, got %d", t)
	}

	for i, c := range o.Preferences.ImprovementFocus {
		canonical := CanonicalClass(c)
		if canonical == "" {
			add(SeverityError, fmt.Sprintf("preferences.improvement_focus[%d]", i), 0,
				"unknown class %q; want one of %s", c, strings.Join(Classes, ", "))
			continue
		}
		o.Preferences.ImprovementFocus[i] = canonical
	}

	if o.Constraints.CreditBuffer < 0 {
		add(SeverityError, "constraints.credit_buffer", 0, "must not be negative, got %d", o.Constraints.CreditBuffer)
	}

	checkAdvice(&o, add)
	return &o, findings
}

// checkAdvice validates profile and advice (docs/spec-desktop.md section 8.3).
func checkAdvice(o *Overlay, add func(Severity, string, int, string, ...any)) {
	oneOf := func(field, value string, allowed []string) {
		if value != "" && !slices.Contains(allowed, value) {
			add(SeverityError, field, 0, "%q is not one of %s", value, strings.Join(allowed, ", "))
		}
	}
	inRange := func(field string, value int, r [2]int) {
		if value != 0 && (value < r[0] || value > r[1]) {
			add(SeverityError, field, 0, "must be %d to %d, got %d", r[0], r[1], value)
		}
	}
	inRange("profile.session_minutes", o.Profile.SessionMinutes, sessionMinRange)
	inRange("profile.battles_per_hour", o.Profile.BattlesPerHour, battlesPerHour)
	oneOf("profile.experience", o.Profile.Experience, Experiences)

	for factor, level := range o.Advice.Weights {
		if !slices.Contains(WeightFactors, factor) {
			add(SeverityError, "advice.weights", 0, "unknown factor %q; want one of %s", factor, strings.Join(WeightFactors, ", "))
			continue
		}
		oneOf("advice.weights."+factor, level, WeightLevels)
	}
	oneOf("advice.answer.length", o.Advice.Answer.Length, AnswerLengths)
	oneOf("advice.answer.format", o.Advice.Answer.Format, AnswerFormats)
	oneOf("advice.answer.explain_basics", o.Advice.Answer.ExplainBasics, BasicsSettings)

	if n := len(o.Advice.Rules); n > MaxRules {
		add(SeverityError, "advice.rules", 0, "%d rules; at most %d, so the guide stays short enough to be read in full", n, MaxRules)
	}
	for i, r := range o.Advice.Rules {
		field := fmt.Sprintf("advice.rules[%d]", i)
		text := strings.TrimSpace(r.Text)
		switch {
		case text == "":
			add(SeverityError, field+".text", 0, "empty rule")
		case len([]rune(text)) > MaxRuleLength:
			add(SeverityError, field+".text", 0, "%d characters; at most %d", len([]rune(text)), MaxRuleLength)
		}
		if r.Added != "" {
			if _, err := parseTimestamp(r.Added); err != nil {
				add(SeverityError, field+".added", 0, "%q is not a date such as 2026-09-26", r.Added)
			}
		}
	}
}

// parseTimestamp accepts RFC 3339 or a bare date, which is what a person
// editing the file by hand is likely to write.
func parseTimestamp(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

var (
	lineRe         = regexp.MustCompile(`^line (\d+): (.*)$`)
	unknownFieldRe = regexp.MustCompile(`^field (\S+) not found in type \S+$`)
)

// decodeFindings turns a YAML decode error into findings, one per problem,
// with the Go type names rewritten out of the messages.
func decodeFindings(err error) []Finding {
	var messages []string
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		messages = typeErr.Errors
	} else {
		messages = []string{strings.TrimPrefix(err.Error(), "yaml: ")}
	}

	findings := make([]Finding, 0, len(messages))
	for _, msg := range messages {
		f := Finding{Severity: SeverityError, Message: msg}
		if m := lineRe.FindStringSubmatch(msg); m != nil {
			f.Line, _ = strconv.Atoi(m[1])
			f.Message = m[2]
		}
		if m := unknownFieldRe.FindStringSubmatch(f.Message); m != nil {
			f.Message = fmt.Sprintf("unknown field %q", m[1])
		}
		findings = append(findings, f)
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Line < findings[j].Line })
	return findings
}
