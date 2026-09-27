// Package brief renders the account as one compact Markdown document
// (docs/spec.md section 7.2): the orientation a session reads first, before
// asking narrow questions of the query layer.
//
// Two rules shape it. The total is capped - 10,000 characters by default -
// and each section has a fixed share of that cap, so one long section can
// never crowd out the others; a section that overflows is cut at a line
// boundary with a note saying what was dropped and which query has the rest.
// And every number comes from the query layer, whose caveats are collected
// into a Known gaps section rather than lost.
package brief

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ondrejkouril/tank-advisor/internal/query"
)

// CurrentTier is the tier from which play counts as current: class figures in
// the brief start here, because lifetime totals otherwise mix in years-old
// low-tier games (framework §3.2).
const CurrentTier = 8

// DefaultMaxChars is the spec's hard limit.
const DefaultMaxChars = 10000

// Options controls rendering.
type Options struct {
	// MaxChars caps the whole document, in characters (runes).
	MaxChars int
	// Title names the account, e.g. "Nickname (EU)".
	Title string
	// MinBattles is the smallest sample a tank needs to appear among the
	// strongest and weakest. Below it, a rating is mostly noise.
	MinBattles int
}

// section is one part of the brief with its share of the budget.
type section struct {
	title string
	share float64 // fraction of MaxChars
	lines []string
	// more names the query that has everything a truncated section dropped.
	more string
}

// Render builds the brief.
func Render(ctx context.Context, s *query.Service, opts Options) (string, error) {
	if opts.MaxChars <= 0 {
		opts.MaxChars = DefaultMaxChars
	}
	if opts.MinBattles <= 0 {
		opts.MinBattles = s.Config.Confidence.VeryLowBelow
	}
	b := &builder{s: s, ctx: ctx, opts: opts, sources: map[string]query.Source{}}

	if err := b.collect(); err != nil {
		return "", err
	}

	sections := []*section{
		b.dataAge(),
		b.resources(),
		b.garage(),
		b.performance(),
		b.tanks(),
		b.goals(),
		b.gaps(),
	}
	return b.assemble(sections), nil
}

type builder struct {
	s    *query.Service
	ctx  context.Context
	opts Options
	now  time.Time

	res        *query.Resources
	garageData *query.Garage
	perf       map[string]query.Performance // by window
	allTiers   query.Performance
	lines      map[string]query.TankLines // by window
	candidates *query.Candidates
	history    query.History

	sources        map[string]query.Source
	overlayVersion string
	caveats        []string
}

var windows = []string{"lifetime", "30d", "60d"}

// collect runs every query the brief draws on, keeping their provenance.
func (b *builder) collect() error {
	b.now = time.Now().UTC()
	if b.s.Now != nil {
		b.now = b.s.Now().UTC()
	}
	ctx, s := b.ctx, b.s

	env, err := s.Resources(ctx)
	if err != nil {
		return err
	}
	b.absorb(env)
	if r, ok := env.Data.(query.Resources); ok {
		b.res = &r
	}

	env, err = s.Garage(ctx, query.GarageFilter{})
	if err != nil {
		return err
	}
	b.absorb(env)
	if g, ok := env.Data.(query.Garage); ok {
		b.garageData = &g
	}

	b.perf = map[string]query.Performance{}
	b.lines = map[string]query.TankLines{}
	for _, name := range windows {
		w, _ := query.ParseWindow(name)
		env, err := s.Performance(ctx, query.ByClass, w, CurrentTier)
		if err != nil {
			return err
		}
		b.absorb(env)
		b.perf[name] = env.Data.(query.Performance)

		lines, err := s.TankLines(ctx, w)
		if err != nil {
			return err
		}
		b.lines[name] = lines
	}

	// The all-tier lifetime total, for one line of context beside the tier
	// VIII+ table.
	env, err = s.Performance(ctx, query.ByClass, query.Window{Lifetime: true}, 0)
	if err != nil {
		return err
	}
	b.absorb(env)
	b.allTiers = env.Data.(query.Performance)

	env, err = s.Candidates(ctx, 0)
	if err != nil {
		return err
	}
	b.absorb(env)
	if c, ok := env.Data.(query.Candidates); ok {
		b.candidates = &c
	}

	b.history, err = s.History(ctx)
	return err
}

// absorb keeps an envelope's provenance: the newest observation per source,
// the overlay version, and every caveat once.
func (b *builder) absorb(env query.Envelope) {
	for _, src := range env.Meta.Sources {
		if cur, ok := b.sources[src.Name]; !ok || src.ObservedAt.After(cur.ObservedAt) {
			b.sources[src.Name] = src
		}
	}
	if env.Meta.OverlayVersion != "" {
		b.overlayVersion = env.Meta.OverlayVersion
	}
	for _, c := range env.Meta.Caveats {
		if !contains(b.caveats, c) {
			b.caveats = append(b.caveats, c)
		}
	}
}

func (b *builder) dataAge() *section {
	sec := &section{title: "Data age", share: 0.09, more: "wotctx doctor"}
	names := make([]string, 0, len(b.sources))
	for name := range b.sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		src := b.sources[name]
		line := fmt.Sprintf("- `%s` %s (%s)", name, src.ObservedAt.Format("2006-01-02 15:04 UTC"), age(src.AgeSeconds))
		if src.Version != "" {
			line += ", version " + src.Version
		}
		sec.lines = append(sec.lines, line)
	}
	if b.overlayVersion != "" {
		t, err := time.Parse(time.RFC3339, b.overlayVersion)
		when := b.overlayVersion
		if err == nil {
			when = t.Format("2006-01-02 15:04 UTC")
		}
		sec.lines = append(sec.lines, "- overlay updated "+when)
	}
	if b.history.Snapshots > 0 {
		sec.lines = append(sec.lines, fmt.Sprintf("- History: %d snapshot(s) since %s (%.1f days); nothing earlier can be known",
			b.history.Snapshots, b.history.FirstSnapshot.Format("2006-01-02"), b.history.Days))
	}
	return sec
}

func (b *builder) resources() *section {
	sec := &section{title: "Resources", share: 0.07, more: "wotctx query resources"}
	r := b.res
	if r == nil {
		sec.lines = append(sec.lines, "- Not synced.")
		return sec
	}
	sec.lines = append(sec.lines,
		fmt.Sprintf("- Credits %s · Gold %s · Bonds %s · Free XP %s",
			num(r.Credits), num(r.Gold), num(r.Bonds), num(r.FreeXP)),
		fmt.Sprintf("- Premium Account %s · WoT Plus %s (from the %s)",
			yesNo(r.Premium.PremiumAccount), yesNo(r.Premium.WoTPlus), r.Premium.Source),
		fmt.Sprintf("- Personal Reserves: %s in stock across %d kinds, %d active",
			num(r.Reserves.Count), r.Reserves.Kinds, len(r.Reserves.Active)))
	if r.LastBattle != nil {
		sec.lines = append(sec.lines, "- Last battle "+r.LastBattle.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return sec
}

func (b *builder) garage() *section {
	sec := &section{title: "Garage", share: 0.08, more: "wotctx query garage"}
	g := b.garageData
	if g == nil {
		sec.lines = append(sec.lines, "- Not synced.")
		return sec
	}
	sec.title = fmt.Sprintf("Garage (%d vehicles)", g.Count)

	tiers := make([]string, 0, len(g.ByTier))
	for tier := 11; tier >= 1; tier-- {
		if n := g.ByTier[fmt.Sprint(tier)]; n > 0 {
			tiers = append(tiers, fmt.Sprintf("%s %d", roman(tier), n))
		}
	}
	classes := make([]string, 0, len(g.ByClass))
	for _, c := range classOrder {
		if n := g.ByClass[c]; n > 0 {
			classes = append(classes, fmt.Sprintf("%s %d", classShort(c), n))
		}
	}
	sec.lines = append(sec.lines, "- By tier: "+strings.Join(tiers, " · "), "- By class: "+strings.Join(classes, " · "))

	// The top tiers are what most questions are about, so name them.
	var top []string
	for _, t := range g.Tanks {
		if t.Tier >= 10 {
			top = append(top, fmt.Sprintf("%s (%s %s)", t.Name, roman(t.Tier), classShort(t.Class)))
		}
	}
	if len(top) > 0 {
		sec.lines = append(sec.lines, "- Tier X and above: "+strings.Join(top, ", "))
	}
	return sec
}

func (b *builder) performance() *section {
	sec := &section{title: "Performance by class, tier " + roman(CurrentTier) + "+", share: 0.16,
		more: fmt.Sprintf("wotctx query performance --by class --min-tier %d", CurrentTier)}
	life := b.perf["lifetime"]
	if !life.Available {
		sec.lines = append(sec.lines, "- "+life.Reason)
		return sec
	}
	sec.lines = append(sec.lines,
		"Random battles in tanks of tier "+roman(CurrentTier)+" and above, the baseline for current play. Lifetime, then WN8 over the last 30 and 60 days where history allows.",
		"",
		"| Class | Battles | Conf. | WR % | DPG | Assist | WN8 | 30 d WN8 | 60 d WN8 |",
		"|---|--:|---|--:|--:|--:|--:|--:|--:|")

	windowed := func(window, key string) string {
		p := b.perf[window]
		if !p.Available {
			return "—"
		}
		if key == "" && p.Total != nil {
			return wn8Cell(p.Total)
		}
		for _, row := range p.Rows {
			if row.Key == key {
				return wn8Cell(&row.Stats)
			}
		}
		return "no battles"
	}
	row := func(label, key string, l query.StatLine) string {
		return fmt.Sprintf("| %s | %s | %s | %.2f | %s | %s | %s | %s | %s |",
			label, num(l.Battles), l.Confidence, l.WinRate, num(int(l.DPG+0.5)), num(int(l.Assist+0.5)),
			wn8(l.WN8), windowed("30d", key), windowed("60d", key))
	}
	for _, r := range life.Rows {
		sec.lines = append(sec.lines, row(classShort(r.Key), r.Key, r.Stats))
	}
	if life.Total != nil {
		sec.lines = append(sec.lines, row("**All**", "", *life.Total))
	}
	if all := b.allTiers; all.Available && all.Total != nil {
		sec.lines = append(sec.lines, fmt.Sprintf(
			"\nAll tiers, for context: %s battles, WN8 %s; lower tiers include old games that do not describe current play.",
			num(all.Total.Battles), wn8(all.Total.WN8)))
	}
	for _, w := range []string{"30d", "60d"} {
		if p := b.perf[w]; p.Available && p.Span != nil && p.Span.Days > float64(windowDays(w))+0.5 {
			sec.lines = append(sec.lines, fmt.Sprintf("\nThe %s column actually covers %.1f days (the nearest earlier snapshot).", w, p.Span.Days))
		}
	}
	return sec
}

// tanks lists the strongest and weakest owned tanks by lifetime WN8.
func (b *builder) tanks() *section {
	sec := &section{title: "Strongest and weakest tanks", share: 0.24, more: "wotctx query garage"}
	if b.garageData == nil {
		sec.lines = append(sec.lines, "- Not synced.")
		return sec
	}

	var rated []query.GarageTank
	for _, t := range b.garageData.Tanks {
		if t.Stats.WN8 != nil && t.Stats.Battles >= b.opts.MinBattles {
			rated = append(rated, t)
		}
	}
	if len(rated) == 0 {
		sec.lines = append(sec.lines, fmt.Sprintf("- No owned tank has a WN8 over %d or more battles.", b.opts.MinBattles))
		return sec
	}
	sort.SliceStable(rated, func(i, j int) bool {
		if *rated[i].Stats.WN8 != *rated[j].Stats.WN8 {
			return *rated[i].Stats.WN8 > *rated[j].Stats.WN8
		}
		return rated[i].TankID < rated[j].TankID
	})

	n := min(5, len(rated)/2)
	if n == 0 {
		n = len(rated)
	}
	sec.lines = append(sec.lines,
		fmt.Sprintf("Owned tanks with %d+ random battles (%d of %d), ordered by lifetime WN8. A sort, not a verdict: read each line through its battle count.",
			b.opts.MinBattles, len(rated), b.garageData.Count),
		"")
	table := func(heading string, list []query.GarageTank) {
		sec.lines = append(sec.lines,
			"**"+heading+"**",
			"",
			"| Tank | Tier | Battles | WR % | DPG | WN8 | 30 d | 60 d |",
			"|---|---|--:|--:|--:|--:|--:|--:|")
		for _, t := range list {
			sec.lines = append(sec.lines, fmt.Sprintf("| %s | %s %s | %s | %.2f | %s | %s | %s | %s |",
				t.Name, roman(t.Tier), classShort(t.Class), num(t.Stats.Battles), t.Stats.WinRate,
				num(int(t.Stats.DPG+0.5)), wn8(t.Stats.WN8), b.recent("30d", t.TankID), b.recent("60d", t.TankID)))
		}
		sec.lines = append(sec.lines, "")
	}
	table("Strongest", rated[:n])
	weakest := make([]query.GarageTank, 0, n)
	for i := len(rated) - 1; i >= len(rated)-n && i >= n; i-- {
		weakest = append(weakest, rated[i])
	}
	if len(weakest) > 0 {
		table("Weakest", weakest)
	}
	return sec
}

// recent renders a tank's windowed WN8 with its battles, or why there is none.
func (b *builder) recent(window string, tankID int) string {
	lines := b.lines[window]
	if !lines.Available {
		return "—"
	}
	l, ok := lines.Lines[tankID]
	if !ok {
		return "not played"
	}
	return wn8Cell(&l)
}

func (b *builder) goals() *section {
	sec := &section{title: "Goals and options", share: 0.18, more: "wotctx query candidates"}
	c := b.candidates
	if c == nil {
		sec.lines = append(sec.lines, "- Not synced.")
		return sec
	}

	var researched, targets, steps []query.Candidate
	next := 0
	for _, cand := range c.Candidates {
		switch {
		case cand.Goal == "target":
			targets = append(targets, cand)
		case cand.Goal == "step":
			steps = append(steps, cand)
		case cand.Origin == query.OriginResearched:
			researched = append(researched, cand)
		default:
			next++
		}
	}

	if len(targets) == 0 {
		sec.lines = append(sec.lines, "- No XP goal is set in the overlay.")
	}
	for _, t := range targets {
		line := fmt.Sprintf("- **Goal: %s** (%s %s, %s credits%s)", t.Name, roman(t.Tier), classShort(t.Class),
			num(t.PriceCredit), shortBy(t))
		switch {
		case t.PathXP > 0:
			line += fmt.Sprintf(": %s XP still needed from owned vehicles, through %s", num(t.PathXP), t.Via[0].Name)
		case t.XPRemaining != nil:
			line += fmt.Sprintf(": %s XP left of %s", num(*t.XPRemaining), num(t.XPCost))
		case t.XPCost > 0:
			line += fmt.Sprintf(": %s XP from %s, none recorded as banked", num(t.XPCost), viaNames(t))
		}
		if t.FreeXPCovers {
			line += fmt.Sprintf("; free XP (%s) covers what remains", num(c.FreeXP))
		}
		sec.lines = append(sec.lines, line)
	}
	for _, st := range steps {
		sec.lines = append(sec.lines, fmt.Sprintf("- Step towards a goal: %s, %s XP from %s", st.Name, num(st.XPCost), viaNames(st)))
	}

	if len(researched) > 0 {
		parts := make([]string, 0, len(researched))
		for _, r := range researched {
			parts = append(parts, fmt.Sprintf("%s (%s %s, %s cr%s)", r.Name, roman(r.Tier), classShort(r.Class), num(r.PriceCredit), shortBy(r)))
		}
		sec.lines = append(sec.lines, "- Researched, not bought: "+strings.Join(parts, "; "))
	}
	line := fmt.Sprintf("- %d further next-research option(s) one step from owned vehicles", next)
	if len(c.AvoidClasses) > 0 {
		line += fmt.Sprintf("; %d excluded as %s", c.ExcludedByPreference, strings.Join(c.AvoidClasses, ", "))
	}
	judged := fmt.Sprintf("- Judged against %s credits", num(c.Credits))
	if c.CreditBuffer > 0 {
		judged += fmt.Sprintf(", keeping %s as a buffer after any purchase", num(c.CreditBuffer))
	}
	judged += fmt.Sprintf("; %s free XP", num(c.FreeXP))
	if c.FreeXPMaxTier > 0 {
		judged += fmt.Sprintf(", spent on research only up to tier %s (modules at any tier)", roman(c.FreeXPMaxTier))
	}
	sec.lines = append(sec.lines, line, judged)
	if len(c.ImprovementFocus) > 0 {
		focus := make([]string, len(c.ImprovementFocus))
		for i, f := range c.ImprovementFocus {
			focus[i] = classShort(f)
		}
		sec.lines = append(sec.lines, "- Improvement focus: "+strings.Join(focus, ", "))
	}
	return sec
}

// gaps lists what is known to be missing: every caveat the queries raised,
// plus the gaps no query can raise because the data does not exist at all.
func (b *builder) gaps() *section {
	sec := &section{title: "Known gaps", share: 0.14, more: "wotctx doctor"}
	for _, c := range b.caveats {
		sec.lines = append(sec.lines, "- "+c)
	}
	// Vehicle XP, marks, loadouts and crew exist in no API. The client mod
	// supplies them; without it, they are simply unknown.
	if src, ok := b.sources["mod:garage"]; ok {
		sec.lines = append(sec.lines, fmt.Sprintf(
			"- Vehicle XP, marks, loadouts and crew come from the client mod's dump of %s: current until the next battle; `wotctx query tank <name>` shows them",
			src.ObservedAt.UTC().Format("2006-01-02 15:04 UTC")))
	} else {
		sec.lines = append(sec.lines,
			"- Not in any API: per-vehicle XP, Marks of Excellence, loadouts and crew (the wotctx client mod supplies them when installed)")
	}
	return sec
}

// assemble renders the sections within the budget.
func (b *builder) assemble(sections []*section) string {
	title := "# World of Tanks account brief"
	if b.opts.Title != "" {
		title += " — " + b.opts.Title
	}
	header := []string{
		title,
		"",
		fmt.Sprintf("Generated %s by wotctx. Random battles unless stated. Figures only; judgement is the reader's.",
			b.now.Format("2006-01-02 15:04 UTC")),
	}

	var out strings.Builder
	out.WriteString(strings.Join(header, "\n"))
	out.WriteString("\n")
	used := utf8.RuneCountInString(out.String())

	// Shares are of what the header leaves, so the sections together can never
	// exceed the cap whatever it is set to.
	avail := b.opts.MaxChars - used
	for _, sec := range sections {
		budget := int(float64(avail) * sec.share)
		out.WriteString(render(sec, budget))
	}
	return out.String()
}

// render writes one section, cutting whole lines to fit its budget.
func render(sec *section, budget int) string {
	for len(sec.lines) > 0 && sec.lines[len(sec.lines)-1] == "" {
		sec.lines = sec.lines[:len(sec.lines)-1]
	}
	head := "\n## " + sec.title + "\n\n"
	body := strings.Join(sec.lines, "\n") + "\n"
	if utf8.RuneCountInString(head+body) <= budget {
		return head + body
	}

	var kept []string
	size := utf8.RuneCountInString(head)
	for i, line := range sec.lines {
		note := fmt.Sprintf("\n_…%d more line(s) cut to fit the brief; run `%s` for all._\n", len(sec.lines)-i, sec.more)
		next := utf8.RuneCountInString(line) + 1
		if size+next+utf8.RuneCountInString(note) > budget {
			return head + strings.Join(kept, "\n") + "\n" + note
		}
		kept = append(kept, line)
		size += next
	}
	return head + strings.Join(kept, "\n") + "\n"
}

var classOrder = []string{"heavyTank", "mediumTank", "lightTank", "AT-SPG", "SPG"}

func classShort(c string) string {
	switch c {
	case "heavyTank":
		return "HT"
	case "mediumTank":
		return "MT"
	case "lightTank":
		return "LT"
	case "AT-SPG":
		return "TD"
	case "SPG":
		return "SPG"
	}
	return c
}

func roman(tier int) string {
	numerals := []string{"", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI"}
	if tier > 0 && tier < len(numerals) {
		return numerals[tier]
	}
	return fmt.Sprint(tier)
}

// num formats an integer with thousands separators.
func num(n int) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func wn8(v *float64) string {
	if v == nil {
		return "—"
	}
	return num(int(*v))
}

// wn8Cell is a windowed WN8 with its battle count, because a WN8 over three
// battles means nothing on its own.
func wn8Cell(l *query.StatLine) string {
	if l.Battles == 0 {
		return "no battles"
	}
	return fmt.Sprintf("%s (%d)", wn8(l.WN8), l.Battles)
}

func yesNo(b *bool) string {
	switch {
	case b == nil:
		return "unknown"
	case *b:
		return "yes"
	default:
		return "no"
	}
}

func age(seconds int64) string {
	d := time.Duration(seconds) * time.Second
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func shortBy(c query.Candidate) string {
	if c.CreditsShort > 0 {
		return ", " + num(c.CreditsShort) + " short"
	}
	if c.Affordable {
		return ", affordable"
	}
	return ""
}

func viaNames(c query.Candidate) string {
	names := make([]string, 0, len(c.Via))
	for _, v := range c.Via {
		names = append(names, v.Name)
	}
	if len(names) == 0 {
		return "an unknown parent"
	}
	return strings.Join(names, " or ")
}

func windowDays(w string) int {
	var d int
	fmt.Sscanf(w, "%dd", &d)
	return d
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
