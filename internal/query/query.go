// Package query answers narrow questions about the cached account as small
// JSON payloads (docs/spec.md section 7).
//
// Every result is wrapped in a provenance envelope that says where each
// number came from, how old it is, and what is known to be missing - so no
// consumer ever needs a second call to learn how far to trust the first. The
// package emits numbers and flags and never a verdict (section 6.6): ranking
// and recommending belong to the skill.
package query

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/overlay"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
	"github.com/ondrejkouril/tank-advisor/internal/wn8"
)

// Source keys as they appear in meta.sources.
const (
	srcAccount   = "wg:account/info"
	srcTankStats = "wg:tanks/stats"
	srcMastery   = "wg:account/tanks"
	srcVehicles  = "wg:encyclopedia/vehicles"
	srcWN8       = "xvm:wn8exp"
	srcOverlay   = "overlay"
)

// AssistCaveat labels every random-battle assist figure. The totals it comes
// from are confirmed to mean what they say (the all-battles totals divide back
// to the API's own averages and to the in-game service record), but the client
// never displays the random-only figure or the radio/track split, so those
// rest on the API alone (spec section 3.1).
const AssistCaveat = "assist figures are random battles only, from the API's assist totals; " +
	"the in-game service record shows an all-battles figure with no radio/track split, so they will not match it exactly"

// Service answers queries against one open cache.
type Service struct {
	DB     *store.DB
	Config config.Config
	// OverlayPath is the overlay file; empty means none is configured.
	OverlayPath string
	// Now supplies the clock; tests override it.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Envelope is what every query returns (spec section 7.1).
type Envelope struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// Meta is the provenance half of an envelope.
type Meta struct {
	GeneratedAt time.Time `json:"generated_at"`
	// Sources lists each cached source the data was derived from, with its
	// age. It is never empty for a result built from cached data.
	Sources []Source `json:"sources"`
	// OverlayVersion is the overlay's updated_at, when the overlay was used.
	OverlayVersion string `json:"overlay_version,omitempty"`
	// Caveats are known gaps and qualifications, never silently dropped.
	Caveats []string `json:"caveats"`
}

// Source is one entry of meta.sources.
type Source struct {
	Name       string    `json:"name"`
	ObservedAt time.Time `json:"observed_at"`
	AgeSeconds int64     `json:"age_seconds"`
	// Version is a source's own vintage stamp where it has one (XVM's).
	Version string `json:"version,omitempty"`
}

// run carries one query's state while it is being built.
type run struct {
	s       *Service
	ctx     context.Context
	now     time.Time
	ages    map[string]store.SourceAge
	meta    Meta
	used    map[string]bool
	overlay *overlay.Overlay

	// The client mod's dump, loaded on first use (client.go).
	clientLoaded bool
	clientData   *clientDump
	// goalXPFromClient records that a goal's banked XP came from the dump.
	goalXPFromClient bool
}

func (s *Service) begin(ctx context.Context) (*run, error) {
	now := s.now()
	ages, err := s.DB.DataAges(ctx, now)
	if err != nil {
		return nil, err
	}
	r := &run{
		s: s, ctx: ctx, now: now,
		ages: make(map[string]store.SourceAge, len(ages)),
		meta: Meta{GeneratedAt: now, Sources: []Source{}, Caveats: []string{}},
		used: map[string]bool{},
	}
	for _, a := range ages {
		r.ages[a.Source] = a
	}
	return r, nil
}

// use records that the result draws on a source. A source never synced is a
// caveat rather than an entry with a meaningless age.
func (r *run) use(keys ...string) {
	for _, key := range keys {
		if r.used[key] {
			continue
		}
		r.used[key] = true
		age, ok := r.ages[key]
		if !ok {
			r.caveat("%s has never been synced (run: wotctx sync)", key)
			continue
		}
		r.meta.Sources = append(r.meta.Sources, Source{
			Name: key, ObservedAt: age.ObservedAt, AgeSeconds: age.AgeSeconds,
		})
	}
}

func (r *run) caveat(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, c := range r.meta.Caveats {
		if c == msg {
			return
		}
	}
	r.meta.Caveats = append(r.meta.Caveats, msg)
}

// loadOverlay reads and validates the overlay once per query. An invalid or
// missing overlay is a caveat and a nil overlay, never an error: the query can
// still answer, just with less.
func (r *run) loadOverlay() *overlay.Overlay {
	if r.overlay != nil || r.used[srcOverlay] {
		return r.overlay
	}
	r.used[srcOverlay] = true

	path := r.s.OverlayPath
	if path == "" {
		r.caveat("no overlay configured: premium status is unknown and there are no goals")
		return nil
	}
	result, err := overlay.Validate(r.ctx, path, r.s.DB)
	switch {
	case errors.Is(err, os.ErrNotExist):
		r.caveat("no overlay at %s: premium status is unknown and there are no goals", path)
		return nil
	case err != nil:
		r.caveat("overlay unreadable (%s), so it was not used", oneLine(err.Error()))
		return nil
	case !result.Valid():
		r.caveat("overlay is invalid (%d error(s)), so it was not used (run: wotctx overlay validate)",
			result.Count(overlay.SeverityError))
		return nil
	}
	for _, f := range result.Findings {
		r.caveat("overlay %s", f.String())
	}

	r.overlay = result.Overlay
	r.meta.OverlayVersion = r.overlay.UpdatedAt.Format(time.RFC3339)
	return r.overlay
}

// syncCaveats carries forward the last sync's caveats for the sources used,
// so a failure during sync reaches every answer it affects (spec 7.3).
func (r *run) syncCaveats() {
	run, err := r.s.DB.LatestSyncRun(r.ctx)
	if err != nil {
		return
	}
	for _, note := range run.NoteLines() {
		key, _, _ := strings.Cut(note, ": ")
		if r.used[key] || strings.HasPrefix(note, "overlay") {
			r.caveat("last sync: %s", note)
		}
	}
}

func (r *run) finish(data any) Envelope {
	r.syncCaveats()
	sort.SliceStable(r.meta.Sources, func(i, j int) bool { return r.meta.Sources[i].Name < r.meta.Sources[j].Name })
	return Envelope{Data: data, Meta: r.meta}
}

// expected loads WN8 expected values, noting their version on the source.
func (r *run) expected() map[int]store.WN8Expected {
	r.use(srcWN8)
	set, err := r.s.DB.LoadWN8Expected(r.ctx)
	if err != nil {
		r.caveat("no WN8 expected values synced, so no WN8 is reported (run: wotctx sync --only wn8)")
		return nil
	}
	for i := range r.meta.Sources {
		if r.meta.Sources[i].Name == srcWN8 {
			r.meta.Sources[i].Version = set.Version
		}
	}
	return set.ByTank
}

// StatLine is the standard set of per-battle figures for one tank or rollup,
// all random battles.
type StatLine struct {
	Battles int `json:"battles"`
	// Confidence grades the battle count (spec section 6.5). Every figure in
	// the line should be read through it.
	Confidence string `json:"confidence"`

	WinRate      float64 `json:"win_rate"`      // percent
	DPG          float64 `json:"dpg"`           // damage per battle
	Frags        float64 `json:"frags"`         // per battle
	Spots        float64 `json:"spots"`         // per battle
	SurvivalRate float64 `json:"survival_rate"` // percent
	AvgXP        float64 `json:"avg_xp"`

	// Assist per battle, random battles only; see AssistCaveat.
	Assist      float64 `json:"assist"`
	AssistRadio float64 `json:"assist_radio"`
	AssistTrack float64 `json:"assist_track"`
	AssistStun  float64 `json:"assist_stun"`

	// WN8 is the battle-aggregate rating; nil when nothing could be rated.
	WN8 *float64 `json:"wn8"`
	// WN8ExcludedBattles counts battles on tanks that could not be rated.
	WN8ExcludedBattles int `json:"wn8_excluded_battles,omitempty"`
}

// line computes a StatLine over rows that are all one mode.
func (s *Service) line(rows []store.TankStats, expected map[int]store.WN8Expected) StatLine {
	var sum store.TankStats
	for _, r := range rows {
		sum.Battles += r.Battles
		sum.Wins += r.Wins
		sum.Survived += r.Survived
		sum.DamageDealt += r.DamageDealt
		sum.Frags += r.Frags
		sum.Spotted += r.Spotted
		sum.XP += r.XP
		sum.RadioAssistedDamage += r.RadioAssistedDamage
		sum.TrackAssistedDamage += r.TrackAssistedDamage
		sum.StunAssistedDamage += r.StunAssistedDamage
	}

	l := StatLine{Battles: sum.Battles, Confidence: s.Config.Confidence.Classify(sum.Battles)}
	if sum.Battles == 0 {
		return l
	}
	n := float64(sum.Battles)
	per := func(v int) float64 { return round(float64(v)/n, 2) }
	l.WinRate = round(100*float64(sum.Wins)/n, 2)
	l.DPG = round(float64(sum.DamageDealt)/n, 1)
	l.Frags = per(sum.Frags)
	l.Spots = per(sum.Spotted)
	l.SurvivalRate = round(100*float64(sum.Survived)/n, 2)
	l.AvgXP = round(float64(sum.XP)/n, 1)
	l.AssistRadio = round(float64(sum.RadioAssistedDamage)/n, 1)
	l.AssistTrack = round(float64(sum.TrackAssistedDamage)/n, 1)
	l.AssistStun = round(float64(sum.StunAssistedDamage)/n, 1)
	l.Assist = round(float64(sum.RadioAssistedDamage+sum.TrackAssistedDamage+sum.StunAssistedDamage)/n, 1)

	if expected != nil {
		rating := wn8.Compute(rows, expected)
		if rating.Battles > 0 {
			v := round(rating.WN8, 0)
			l.WN8 = &v
		}
		l.WN8ExcludedBattles = rating.ExcludedBattles
	}
	return l
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func oneLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " ")
}

// Window is a parsed --window value.
type Window struct {
	Lifetime bool
	Days     int
}

var windowRe = regexp.MustCompile(`^(\d+)d$`)

// ParseWindow accepts "lifetime" or a number of days such as "30d".
func ParseWindow(s string) (Window, error) {
	if s == "lifetime" {
		return Window{Lifetime: true}, nil
	}
	m := windowRe.FindStringSubmatch(s)
	if m == nil {
		return Window{}, fmt.Errorf("window %q: want lifetime or a number of days such as 30d", s)
	}
	days, _ := strconv.Atoi(m[1])
	if days <= 0 {
		return Window{}, fmt.Errorf("window %q: must be at least 1d", s)
	}
	return Window{Days: days}, nil
}

func (w Window) String() string {
	if w.Lifetime {
		return "lifetime"
	}
	return fmt.Sprintf("%dd", w.Days)
}

// Span describes the interval a windowed result actually covers, which is
// reported instead of the one requested (spec section 6.1).
type Span struct {
	Requested string    `json:"requested"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	Days      float64   `json:"days"`
}

// History describes how much snapshot history exists, for the "not yet"
// answer a window without a baseline has to give.
type History struct {
	FirstSnapshot time.Time `json:"first_snapshot"`
	Days          float64   `json:"days"`
	Snapshots     int       `json:"snapshots"`
}

// windowDeltas returns the deltas for a window, or a reason it cannot be
// answered yet. It never falls back to a shorter window: that would silently
// answer a different question (spec section 6.1).
func (r *run) windowDeltas(w Window) ([]store.TankStatsDelta, *Span, *History, error) {
	refs, err := r.s.DB.TankStatsSnapshots(r.ctx, wg.ModeRandom)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(refs) == 0 {
		return nil, nil, nil, store.ErrNotFound
	}
	history := &History{
		FirstSnapshot: refs[0].At,
		Days:          round(r.now.Sub(refs[0].At).Hours()/24, 1),
		Snapshots:     len(refs),
	}

	cutoff := r.now.Add(-time.Duration(w.Days) * 24 * time.Hour)
	baseline := -1
	for i, ref := range refs {
		if !ref.At.After(cutoff) {
			baseline = i
		}
	}
	latest := refs[len(refs)-1]
	if baseline < 0 || refs[baseline].ID == latest.ID {
		return nil, nil, history, store.ErrNoBaseline
	}

	deltas, err := r.s.DB.TankStatsBetween(r.ctx, wg.ModeRandom, refs[baseline], latest)
	if err != nil {
		return nil, nil, history, err
	}
	span := &Span{
		Requested: w.String(),
		From:      refs[baseline].At,
		To:        latest.At,
		Days:      round(latest.At.Sub(refs[baseline].At).Hours()/24, 1),
	}
	r.spanCaveat(span, w.Days)
	return deltas, span, history, nil
}

// spanCaveat says when a window covers more than was asked. Snapshots are
// only as frequent as syncs, so the baseline is the newest one at or before
// the cutoff, which can be well before it; the figures then describe the
// longer span, and a reader skimming for "last 30 days" must be told.
func (r *run) spanCaveat(span *Span, requested int) {
	if span != nil && span.Days > float64(requested)+0.5 {
		r.caveat("the nearest snapshot before the %dd cutoff is from %s, so this %dd window actually covers %.1f days",
			requested, span.From.Format("2006-01-02"), requested, span.Days)
	}
}

func deltaRows(deltas []store.TankStatsDelta) []store.TankStats {
	rows := make([]store.TankStats, 0, len(deltas))
	for _, d := range deltas {
		rows = append(rows, d.Delta)
	}
	return rows
}

// ErrUnknownTank reports a tank reference that names no vehicle.
type ErrUnknownTank struct {
	Input       string
	Suggestions []string
}

func (e *ErrUnknownTank) Error() string {
	msg := fmt.Sprintf("unknown tank %q", e.Input)
	if len(e.Suggestions) > 0 {
		quoted := make([]string, len(e.Suggestions))
		for i, n := range e.Suggestions {
			quoted[i] = strconv.Quote(n)
		}
		msg += "; did you mean " + strings.Join(quoted, ", ") + "?"
	}
	return msg
}

// resolveTank accepts a tank_id or a name. All-digit input is tried as an id
// first; some vehicles have numeric names ("121"), so an id that does not
// exist falls back to a name lookup.
func (s *Service) resolveTank(ctx context.Context, input string) (store.Vehicle, error) {
	input = strings.TrimSpace(input)
	if id, err := strconv.Atoi(input); err == nil {
		v, err := s.DB.VehicleByID(ctx, id)
		if err == nil {
			return v, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return store.Vehicle{}, err
		}
	}
	v, err := s.DB.VehicleByName(ctx, input)
	if errors.Is(err, store.ErrNotFound) {
		suggestions, _ := s.DB.SuggestVehicleNames(ctx, input, 3)
		return store.Vehicle{}, &ErrUnknownTank{Input: input, Suggestions: suggestions}
	}
	return v, err
}
