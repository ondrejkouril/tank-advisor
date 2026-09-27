// Package advice renders the player's own advice settings into the words
// Claude reads (docs/spec-desktop.md section 8).
//
// The advice has three layers. The core rules (skills/.../core.md) are fixed:
// they keep an answer true. The player's settings, rendered here from the
// overlay, come next. The framework's defaults (framework.md) apply wherever
// the player has set nothing. The framework holds rules, never values: every
// value it relies on - weights, session length, buffer, free-XP limit, answer
// shape - is a setting, rendered here with its default when the player has not
// set it, and marked as one or the other.
package advice

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/overlay"
)

// Defaults are the values used where the player has set nothing. They are the
// owner's validated values where those are sensible for anyone, and neutral
// otherwise (no class preference, no credit buffer, no free-XP limit).
var (
	DefaultSessionMinutes = 60
	DefaultBattlesPerHour = 8
	DefaultExperience     = "auto"
	DefaultWeights        = map[string]string{
		"fit": "highest", "time": "high", "credits": "medium", "earning": "low", "meta": "tiebreak", "goals": "tiebreak",
	}
	DefaultLength   = "normal"
	DefaultFormat   = "sections"
	DefaultBasics   = "auto"
	DefaultAlsoLine = true
	DefaultCoaching = true
)

// factorNames are the weights' factors as the framework's section 4.1 names them.
var factorNames = map[string]string{
	"fit":     "Fit to the player (class order, own tier VIII+ class performance)",
	"time":    "Time to get it (XP still needed, at the player's own XP rate)",
	"credits": "Credit cost and affordability",
	"earning": "Earning potential once owned",
	"meta":    "Strength in the current meta (server-wide)",
	"goals":   "Alignment with the player's XP goals",
}

var levelNames = map[string]string{
	"highest":  "highest",
	"high":     "high",
	"medium":   "medium",
	"low":      "low",
	"tiebreak": "only a tie-breaker",
	"ignore":   "ignored",
}

var classNames = map[string]string{
	"lightTank": "light tanks", "mediumTank": "medium tanks", "heavyTank": "heavy tanks",
	"AT-SPG": "tank destroyers", "SPG": "SPGs",
}

// Conflict is a player rule that may work against the core rules or the
// player's own settings. The app shows it; the guide restates what applies.
type Conflict struct {
	Rule    int    `json:"rule"` // index into advice.rules
	Message string `json:"message"`
}

// coreConflicts are phrasings that ask for what the core rules forbid:
// guessing figures, or dropping the data age, sample sizes or sources. They
// are deliberately broad; a false alarm costs a warning, a miss costs an
// answer that is quietly less true.
var coreConflicts = []struct {
	re      *regexp.Regexp
	message string
}{
	{regexp.MustCompile(`(?i)\b(guess|make up|invent|assume|estimate)\b`),
		"asks for figures to be guessed; the core rules take numbers only from the data, so this rule cannot apply to figures"},
	{regexp.MustCompile(`(?i)\b(skip|drop|omit|leave out|no|don'?t (give|state|say|mention|show))\b.{0,30}\b(data age|age of the data|battle counts?|sample size|caveats?|sources?|dates?)\b`),
		"asks to leave out data age, sample sizes, caveats or sources; the core rules require them"},
	{regexp.MustCompile(`(?i)\bfrom memory\b`),
		"asks for answers from memory; the core rules take numbers only from the data"},
	{regexp.MustCompile(`(?i)\b(one|single|just (the )?)\s*(metric|number|stat)\b`),
		"asks for verdicts on one metric; the core rules require two agreeing signals"},
}

var recommendWords = regexp.MustCompile(`(?i)\b(recommend|suggest|push|play more|buy|research)\b`)

var classWords = map[string]*regexp.Regexp{
	"lightTank":  regexp.MustCompile(`(?i)\b(light tanks?|lights?|LTs?)\b`),
	"mediumTank": regexp.MustCompile(`(?i)\b(medium tanks?|mediums?|MTs?)\b`),
	"heavyTank":  regexp.MustCompile(`(?i)\b(heavy tanks?|heavies|HTs?)\b`),
	"AT-SPG":     regexp.MustCompile(`(?i)\b(tank destroyers?|TDs?)\b`),
	"SPG":        regexp.MustCompile(`(?i)\b(SPGs?|arty|artillery)\b`),
}

var negation = regexp.MustCompile(`(?i)\b(never|not|no|don'?t|avoid|stop)\b`)

// Conflicts checks the player's rules against the core rules and against the
// player's own class settings.
func Conflicts(o *overlay.Overlay) []Conflict {
	if o == nil {
		return nil
	}
	var out []Conflict
	for i, r := range o.Advice.Rules {
		for _, c := range coreConflicts {
			if c.re.MatchString(r.Text) {
				out = append(out, Conflict{Rule: i, Message: c.message})
				break
			}
		}
		if recommendWords.MatchString(r.Text) && !negation.MatchString(r.Text) {
			for _, avoided := range o.Preferences.AvoidClasses {
				if re := classWords[avoided]; re != nil && re.MatchString(r.Text) {
					out = append(out, Conflict{Rule: i, Message: fmt.Sprintf(
						"asks for %s, which the player's settings say never to recommend; the setting and this rule disagree", classNames[avoided])})
				}
			}
		}
	}
	return out
}

// Render writes the "Your advice" section. o may be nil: with no overlay,
// every value is a default and the section says so.
func Render(o *overlay.Overlay) string {
	if o == nil {
		o = &overlay.Overlay{}
	}
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("## Your advice\n\n")
	if o.UpdatedAt.IsZero() {
		w("The player has no settings yet (no overlay), so every value below is the default.\n\n")
	} else {
		w("The player's settings, from the Tank Advisor app or wot-overlay.yaml (updated %s). ", o.UpdatedAt.Format(time.DateOnly))
	}
	w("Each value is marked *(yours)* when the player set it and *(default)* otherwise. " +
		"These settings override the framework's defaults wherever they speak. They never override the core rules: " +
		"a setting or rule that would make an answer untrue, unsourced or overconfident does not apply, and the answer " +
		"says so in one line.\n\n")

	mark := func(set bool) string {
		if set {
			return "(yours)"
		}
		return "(default)"
	}

	// Who the player is.
	w("### Who the player is\n\n")
	exp, expSet := or(o.Profile.Experience, DefaultExperience)
	switch exp {
	case "auto":
		w("- Experience: decide from the data, as the framework's section 1 says %s.\n", mark(expSet))
	case "new":
		w("- Experience: new to the game %s. Explain terms and mechanics briefly where they matter.\n", mark(expSet))
	case "returning":
		w("- Experience: returning after a break %s. Old lifetime statistics may not reflect current skill; lead with recent windows, and mention game changes since they last played when relevant.\n", mark(expSet))
	case "experienced":
		w("- Experience: experienced %s. Skip basics unless asked.\n", mark(expSet))
	}
	session, sessionSet := orInt(o.Profile.SessionMinutes, DefaultSessionMinutes)
	bph, bphSet := orInt(o.Profile.BattlesPerHour, DefaultBattlesPerHour)
	w("- A session is %s %s. State every time cost in sessions of this length.\n", minutes(session), mark(sessionSet))
	w("- Battles per hour: %d %s. Convert battles to time with it; replace it with a measured rate if the player gives one.\n", bph, mark(bphSet))
	if p := strings.TrimSpace(o.Constraints.Playtime); p != "" {
		w("- Playtime: %s (yours).\n", p)
	}
	w("- Class preference: %s.\n", classOrder(o.Preferences.ClassRank))
	if len(o.Preferences.AvoidClasses) > 0 {
		w("- Never recommend: %s (yours). Questions about owned vehicles of these classes still get answers, but no advice pushes towards playing them.\n",
			classList(o.Preferences.AvoidClasses))
	}
	if len(o.Preferences.Strengths) > 0 {
		w("- Stated strengths: %s (yours). Preferences to respect, not claims to trust: the data decides whether they are strengths (framework section 3.4).\n",
			strings.Join(o.Preferences.Strengths, ", "))
	}
	coaching, coachingSet := orBool(o.Advice.Coaching, DefaultCoaching)
	if len(o.Preferences.ImprovementFocus) > 0 {
		if coaching {
			w("- Improvement focus: %s (yours), with coaching on %s: questions about them get a coaching angle (which ratio to work on, core section 3.3), and progress or regression is noted in one line, unasked, when the data allows.\n",
				classList(o.Preferences.ImprovementFocus), mark(coachingSet))
		} else {
			w("- Improvement focus: %s (yours), coaching off (yours): answer what is asked, no unasked progress notes.\n",
				classList(o.Preferences.ImprovementFocus))
		}
	}

	// What matters when choosing.
	w("\n### What matters when choosing what to buy or research (framework section 4.1)\n\n")
	w("| Factor | Weight |\n|---|---|\n")
	for _, f := range overlay.WeightFactors {
		level, set := o.Advice.Weights[f], true
		if level == "" {
			level, set = DefaultWeights[f], false
		}
		w("| %s | %s %s |\n", factorNames[f], levelNames[level], mark(set))
	}
	w("\n")
	if buf := o.Constraints.CreditBuffer; buf > 0 {
		w("- Credit buffer: %s credits must remain after any purchase (yours). `query candidates` already includes it in `affordable` and `credits_short`; quote those and never add it again.\n", thousands(buf))
	} else {
		w("- Credit buffer: none (default). A purchase is affordable when the credits cover the price.\n")
	}
	if t := o.Preferences.FreeXPMaxTier; t > 0 {
		w("- Free XP researches tanks up to tier %s only (yours); above it, vehicles are ground with vehicle XP, and free XP goes to their modules. `query candidates` applies this as `free_xp_allowed`. Never suggest free XP to research a tank above tier %s; if asked directly, give the arithmetic and say it is outside the player's policy.\n",
			roman(t), roman(t))
	} else {
		w("- Free XP: no tier limit (default). It may research any tank; still say when it would be better kept for a new tank's modules.\n")
	}

	// How answers look.
	w("\n### How answers look\n\n")
	format, formatSet := or(o.Advice.Answer.Format, DefaultFormat)
	if format == "sections" {
		w("- Format: the five parts of framework section 5 (Short answer / Why / Best setup / Watch-outs / Verdict) for recommendations %s.\n", mark(formatSet))
	} else {
		w("- Format: plain prose %s: no headings, and no part labels either (no \"Short answer:\", \"Why:\", \"Best setup:\", \"Watch-outs:\" or \"Verdict:\"). The content the five parts carry is still there, woven into the prose: the decision first, the evidence, the caveats, the confidence.\n", mark(formatSet))
	}
	length, lengthSet := or(o.Advice.Answer.Length, DefaultLength)
	switch length {
	case "short":
		w("- Length: short %s: the decision and the one or two figures behind it; offer detail rather than give it.\n", mark(lengthSet))
	case "normal":
		w("- Length: normal %s.\n", mark(lengthSet))
	case "detailed":
		w("- Length: detailed %s: show the working, the alternatives considered and why they lost.\n", mark(lengthSet))
	}
	basics, basicsSet := or(o.Advice.Answer.ExplainBasics, DefaultBasics)
	switch basics {
	case "auto":
		w("- Basics: explained or skipped according to experience %s.\n", mark(basicsSet))
	case "always":
		w("- Basics: always explain terms and mechanics briefly %s.\n", mark(basicsSet))
	case "never":
		w("- Basics: never explain them unless asked %s.\n", mark(basicsSet))
	}
	also, alsoSet := orBool(o.Advice.Answer.AlsoWorthKnowing, DefaultAlsoLine)
	if also {
		w("- One unasked \"also worth knowing\" line is allowed %s.\n", mark(alsoSet))
	} else {
		w("- No unasked additions %s: answer only what was asked.\n", mark(alsoSet))
	}
	w("- In every format and length, the core rules still hold: data age, battle counts where player figures are used, and the three kinds of evidence kept apart.\n")

	// The player's own rules.
	if len(o.Advice.Rules) > 0 {
		w("\n### The player's own rules\n\n")
		w("In the player's words. Follow them where they apply. They override the framework's defaults, never the core rules.\n\n")
		conflicts := Conflicts(o)
		for i, r := range o.Advice.Rules {
			added := ""
			if r.Added != "" {
				added = " (added " + r.Added + ")"
			}
			w("%d. %s%s\n", i+1, strings.TrimSpace(r.Text), added)
			for _, c := range conflicts {
				if c.Rule == i {
					w("   - Note: this rule %s.\n", c.Message)
				}
			}
		}
	}
	return b.String()
}

func or(v, def string) (string, bool) {
	if v == "" {
		return def, false
	}
	return v, true
}

func orInt(v, def int) (int, bool) {
	if v == 0 {
		return def, false
	}
	return v, true
}

func orBool(v *bool, def bool) (bool, bool) {
	if v == nil {
		return def, false
	}
	return *v, true
}

func minutes(m int) string {
	switch {
	case m%60 == 0 && m == 60:
		return "one hour"
	case m%60 == 0:
		return fmt.Sprintf("%d hours", m/60)
	default:
		return fmt.Sprintf("%d minutes", m)
	}
}

// classOrder renders class_rank as "medium tanks, then light tanks, then heavy
// tanks = tank destroyers".
func classOrder(rank map[string]int) string {
	if len(rank) == 0 {
		return "none set (default): weigh fit by the player's own class performance alone"
	}
	byRank := map[int][]string{}
	var ranks []int
	for _, c := range overlay.Classes {
		if r, ok := rank[c]; ok {
			if _, seen := byRank[r]; !seen {
				ranks = append(ranks, r)
			}
			byRank[r] = append(byRank[r], classNames[c])
		}
	}
	slices.Sort(ranks)
	parts := make([]string, len(ranks))
	for i, r := range ranks {
		parts[i] = strings.Join(byRank[r], " = ")
	}
	return "most preferred first, " + strings.Join(parts, ", then ") + " (yours). Fit outranks meta; say so in one line when the meta pulls the other way"
}

func classList(classes []string) string {
	names := make([]string, len(classes))
	for i, c := range classes {
		names[i] = classNames[c]
	}
	return strings.Join(names, ", ")
}

func thousands(n int) string {
	s := fmt.Sprint(n)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

func roman(t int) string {
	numerals := []string{"", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI"}
	if t >= 1 && t < len(numerals) {
		return numerals[t]
	}
	return fmt.Sprint(t)
}
