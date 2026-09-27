package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/cli"
	"github.com/ondrejkouril/tank-advisor/internal/config"
)

// The background duties of docs/spec-desktop.md section 5.6. The window's
// process decides when to call them (cmd/tankadvisor); this file decides what
// each does, so it can be tested with a fake clock.

// Notice is a tray notification.
type Notice struct {
	// ID identifies the notice, so a repeat replaces rather than piles up.
	ID    string
	Title string
	Body  string
	// Action is an action for Do that the notice offers as a button, with
	// its label; "" offers none. Clicking the notice itself opens the window.
	Action      string
	ActionLabel string
}

// DutyHooks are what the duties need from the app around them.
type DutyHooks struct {
	Notify  func(Notice)
	Changed func(Result) // tell the window something happened
	Now     func() time.Time
	Sleep   func(ctx context.Context, d time.Duration) error
	// Sync runs a sync; the default is the Sync now action.
	Sync func(ctx context.Context) Result
}

// Duties runs the background duties and remembers what they last did.
type Duties struct {
	svc   *Service
	hooks DutyHooks

	mu       sync.Mutex
	last     map[string]dutyRun
	notified map[string]time.Time // notice ID -> when it was last shown
}

type dutyRun struct {
	at   time.Time
	what string
}

// Timings of the duties.
const (
	// settleQuiet is how long the dump must go unchanged after the game
	// exits before the sync takes it. The mod writes it 3 s after the lobby
	// settles, so a few seconds of quiet means it is done.
	settleQuiet = 5 * time.Second
	settleMax   = 2 * time.Minute
	// dailySyncAfter is how old the account's data may get on a day without
	// play before a sync runs anyway.
	dailySyncAfter = 24 * time.Hour
	// renewWithin is how close to expiry the login is renewed; the CLI uses
	// the same three days.
	renewWithin = 3 * 24 * time.Hour
	// renoticeAfter keeps a lapsed-login notice from repeating more than
	// once a day.
	renoticeAfter    = 24 * time.Hour
	updateCheckEvery = 24 * time.Hour
)

// NewDuties returns the duties with defaults filled in.
func (s *Service) NewDuties(h DutyHooks) *Duties {
	if h.Now == nil {
		h.Now = time.Now
	}
	if h.Sleep == nil {
		h.Sleep = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	if h.Notify == nil {
		h.Notify = func(Notice) {}
	}
	if h.Changed == nil {
		h.Changed = func(Result) {}
	}
	d := &Duties{svc: s, hooks: h, last: map[string]dutyRun{}, notified: map[string]time.Time{}}
	if d.hooks.Sync == nil {
		d.hooks.Sync = d.syncWhenFree
	}
	s.mu.Lock()
	s.duties = d
	s.mu.Unlock()
	return d
}

func (d *Duties) config() (config.Config, bool) {
	env := d.svc.opts.NewEnv(io.Discard, io.Discard)
	return env.Config, env.ConfigErr == nil
}

func (d *Duties) paused(name string) bool {
	cfg, ok := d.config()
	return !ok || cfg.Duties.IsPaused(name)
}

func (d *Duties) record(name, what string) {
	d.mu.Lock()
	d.last[name] = dutyRun{at: d.hooks.Now(), what: what}
	d.mu.Unlock()
}

// syncWhenFree runs Sync now, waiting while a click in the window holds the
// app busy.
func (d *Duties) syncWhenFree(ctx context.Context) Result {
	for i := 0; ; i++ {
		r := d.svc.Do(ctx, "sync", "")
		if r.OK || !strings.HasPrefix(r.Message, "Busy:") || i == 30 {
			return r
		}
		if err := d.hooks.Sleep(ctx, 10*time.Second); err != nil {
			return Result{Message: err.Error()}
		}
	}
}

// AfterGame is called when World of Tanks has exited. Sync boundaries then
// fall between play sessions, so query sessions sees each session whole.
func (d *Duties) AfterGame(ctx context.Context) {
	if d.paused("sync") {
		return
	}
	cfg, _ := d.config()
	if !cfg.Configured() {
		return
	}
	d.waitForDump(ctx)
	r := d.hooks.Sync(ctx)
	d.record("sync", "after the game closed: "+r.Message)
	d.hooks.Changed(r)
}

// waitForDump waits until the mod's dump has gone unchanged for a few
// seconds. With no dump there is nothing to wait for.
func (d *Duties) waitForDump(ctx context.Context) {
	env := d.svc.opts.NewEnv(io.Discard, io.Discard)
	path := cli.ModDumpPath(env)
	start := d.hooks.Now()
	for d.hooks.Now().Sub(start) < settleMax {
		info, err := os.Stat(path)
		if err != nil || d.hooks.Now().Sub(info.ModTime()) >= settleQuiet {
			return
		}
		if d.hooks.Sleep(ctx, 2*time.Second) != nil {
			return
		}
	}
}

// Periodic runs the duties that are due by the clock: the daily sync, the
// login's renewal, the update check. It is cheap when nothing is due: it
// reads the config, the keychain and one row of the cache.
func (d *Duties) Periodic(ctx context.Context) {
	cfg, ok := d.config()
	if !ok {
		return
	}
	if cfg.Configured() && !cfg.Duties.IsPaused("login") {
		d.keepLogin(ctx)
	}
	if cfg.Configured() && !cfg.Duties.IsPaused("sync") && d.syncDue(ctx) {
		r := d.hooks.Sync(ctx)
		d.record("sync", "daily: "+r.Message)
		d.hooks.Changed(r)
	}
	if !cfg.Duties.IsPaused("updates") {
		d.checkUpdates(ctx)
	}
}

// syncDue reports whether the account's data is a day old: the daily sync
// that keeps history going on days without play.
func (d *Duties) syncDue(ctx context.Context) bool {
	env := d.svc.opts.NewEnv(io.Discard, io.Discard)
	if !env.HasStore() {
		return true
	}
	db, err := env.OpenStore(ctx)
	if err != nil {
		return false
	}
	defer db.Close()
	snap, err := db.LatestUsableSnapshot(ctx, "wg", "account/info")
	if err != nil {
		return true
	}
	return d.hooks.Now().Sub(snap.RequestedAt) >= dailySyncAfter
}

// keepLogin renews the login ahead of expiry, and says so when it has lapsed:
// syncing stops without it, and nothing else would tell the player.
func (d *Duties) keepLogin(ctx context.Context) {
	env := d.svc.opts.NewEnv(io.Discard, io.Discard)
	login := cli.StoredLogin(env)
	switch {
	case !login.Stored:
		// Logged out on purpose, or never logged in: not a lapse.
	case login.Valid && login.ExpiresAt.Sub(d.hooks.Now()) < renewWithin:
		r := d.svc.Do(ctx, "renew", "")
		d.record("login", "renewed: "+r.Message)
		if !r.OK {
			d.notify(Notice{ID: "login-renew", Title: "Wargaming login could not be renewed",
				Body: r.Message, Action: "relogin", ActionLabel: "Log in again"})
		}
		d.hooks.Changed(r)
	case !login.Valid:
		d.notify(Notice{ID: "login-lapsed", Title: "Your Wargaming login has lapsed",
			Body: "Tank Advisor cannot sync until you log in again.", Action: "relogin", ActionLabel: "Log in again"})
	}
}

func (d *Duties) checkUpdates(ctx context.Context) {
	d.mu.Lock()
	last := d.last["updates"]
	d.mu.Unlock()
	if !last.at.IsZero() && d.hooks.Now().Sub(last.at) < updateCheckEvery {
		return
	}
	r := d.svc.checkUpdates(ctx)
	d.record("updates", r.Message)
	d.svc.mu.Lock()
	u := d.svc.update
	d.svc.mu.Unlock()
	if u.Newer {
		d.notify(Notice{ID: "update-" + u.Latest, Title: "Tank Advisor " + u.Latest + " is available",
			Body: "Open Tank Advisor to get it.", Action: "release-page", ActionLabel: "Open release page"})
	}
}

// Mod is the mod upkeep, run every few minutes.
func (d *Duties) Mod(ctx context.Context) {
	if d.paused("mod") {
		return
	}
	if msg := d.svc.KeepModInstalled(ctx); msg != "" {
		d.record("mod", msg)
		d.hooks.Changed(Result{OK: true, Message: msg})
	}
}

// notify shows a notice, at most once a day per ID.
func (d *Duties) notify(n Notice) {
	d.mu.Lock()
	last, seen := d.notified[n.ID]
	if seen && d.hooks.Now().Sub(last) < renoticeAfter {
		d.mu.Unlock()
		return
	}
	d.notified[n.ID] = d.hooks.Now()
	d.mu.Unlock()
	d.hooks.Notify(n)
}

// dutyLines describes each duty for the status window.
func (d *Duties) dutyLines(cfg config.Config) []string {
	names := map[string]string{
		"sync":    "Sync after you play, and once a day",
		"login":   "Renew the Wargaming login before it runs out",
		"mod":     "Put the mod back after a game update",
		"updates": "Check for a new Tank Advisor once a day",
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var lines []string
	for _, n := range config.DutyNames {
		state := "on"
		if cfg.Duties.IsPaused(n) {
			state = "paused"
		}
		line := fmt.Sprintf("%s: %s", names[n], state)
		if run, ok := d.last[n]; ok {
			line += fmt.Sprintf(" (last %s: %s)", run.at.Local().Format("2 Jan 15:04"), run.what)
		}
		lines = append(lines, line)
	}
	return lines
}

// dutyVerbs name each duty on its pause button.
var dutyVerbs = map[string]string{"sync": "syncing", "login": "login renewal", "mod": "mod upkeep", "updates": "update checks"}

func (s *Service) dutiesRow(env *cli.Env) Row {
	row := Row{ID: "duties", Title: "Background", State: stateOK}
	s.mu.Lock()
	d := s.duties
	s.mu.Unlock()
	if d == nil {
		d = &Duties{last: map[string]dutyRun{}}
	}
	row.Lines = d.dutyLines(env.Config)
	var paused []string
	for _, n := range config.DutyNames {
		if env.Config.Duties.IsPaused(n) {
			paused = append(paused, dutyVerbs[n])
			row.Actions = append(row.Actions, Action{ID: "duty-resume", Arg: n, Label: "Resume " + dutyVerbs[n]})
		} else {
			row.Actions = append(row.Actions, Action{ID: "duty-pause", Arg: n, Label: "Pause " + dutyVerbs[n]})
		}
	}
	if len(paused) > 0 {
		row.State = stateWarn
		row.Summary = "Paused: " + strings.Join(paused, ", ") + "."
		if env.Config.Duties.IsPaused("sync") {
			row.Hint = "While syncing is paused, history has gaps on the days you do not sync by hand."
		}
	} else {
		row.Summary = "Everything runs by itself."
	}
	return row
}

func (s *Service) setDutyPaused(name string, pause bool) Result {
	known := false
	for _, n := range config.DutyNames {
		known = known || n == name
	}
	if !known {
		return Result{Message: "unknown duty " + name}
	}
	env := s.opts.NewEnv(io.Discard, io.Discard)
	var paused []string
	for _, p := range env.Config.Duties.Paused {
		if p != name {
			paused = append(paused, p)
		}
	}
	if pause {
		paused = append(paused, name)
	}
	var value any = paused
	if len(paused) == 0 {
		value = nil
	}
	if err := config.Set(env.Paths.ConfigFile, config.Update{Key: "duties.paused", Value: value}); err != nil {
		return Result{Message: err.Error()}
	}
	if pause {
		return Result{OK: true, Message: "Paused " + dutyVerbs[name] + "."}
	}
	return Result{OK: true, Message: "Resumed " + dutyVerbs[name] + "."}
}
