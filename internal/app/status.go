package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/cli"
	"github.com/ondrejkouril/tank-advisor/internal/game"
)

// Row states, worst last. They are doctor's statuses.
const (
	stateOK      = string(cli.StatusOK)
	stateUnknown = string(cli.StatusUnknown)
	stateWarn    = string(cli.StatusWarn)
	stateFail    = string(cli.StatusFail)
)

var severity = map[string]int{stateOK: 0, stateUnknown: 1, stateWarn: 2, stateFail: 3}

func worst(states ...string) string {
	w := stateOK
	for _, s := range states {
		if severity[s] > severity[w] {
			w = s
		}
	}
	return w
}

// Status is everything the status window shows.
type Status struct {
	Version string `json:"version"`
	// Account is "nickname on Europe", or "" before the first login.
	Account string `json:"account"`
	// Running names the action in progress, or "".
	Running string `json:"running"`
	// NeedsSetup is true until the setup wizard has been finished, or when
	// the notice changed since the player agreed.
	NeedsSetup bool  `json:"needsSetup"`
	Rows       []Row `json:"rows"`
	About      About `json:"about"`
}

// Row is one line of the status window: a state, what it means, and the
// buttons that fix it.
type Row struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	State   string   `json:"state"`
	Summary string   `json:"summary"`
	Lines   []string `json:"lines"`
	// Hint says what to click, never what to type (docs/spec-desktop.md
	// section 5.1).
	Hint    string   `json:"hint,omitempty"`
	Actions []Action `json:"actions"`
}

// Action is one button.
type Action struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Primary bool   `json:"primary,omitempty"`
	Danger  bool   `json:"danger,omitempty"`
	// Confirm, when set, is asked before the action runs.
	Confirm string `json:"confirm,omitempty"`
	// Choices, when set, is a list the player picks from; the pick is the
	// action's argument.
	Choices []Choice `json:"choices,omitempty"`
	// Waits is true for an action that waits on the player elsewhere (the
	// browser login), so the window offers to cancel it.
	Waits bool `json:"waits,omitempty"`
	// Arg is passed to the action, for a button that is one of several of
	// the same kind.
	Arg string `json:"arg,omitempty"`
}

// Choice is one option of an Action.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// realms are the servers a player picks from, in the API's own names.
var realms = []Choice{{"eu", "Europe"}, {"com", "North America"}, {"asia", "Asia"}}

func realmName(realm string) string {
	for _, r := range realms {
		if r.Value == realm {
			return r.Label
		}
	}
	return realm
}

// Status gathers every row. It has no side effects: like doctor, it never
// creates the cache or touches the network.
func (s *Service) Status(ctx context.Context) Status {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	checks := map[string]cli.Check{}
	for _, c := range cli.Checks(ctx, env) {
		checks[c.Name] = c
	}

	st := Status{Version: s.opts.Version, Running: s.Running(), About: aboutFor(env.Config.Account.Realm), NeedsSetup: s.NeedsSetup()}
	if env.ConfigErr == nil && env.Config.Configured() {
		st.Account = env.Config.Account.Nickname + " on " + realmName(env.Config.Account.Realm)
	}
	st.Rows = []Row{
		s.loginRow(env, checks),
		s.dataRow(ctx, env, checks),
		s.modRow(ctx, env, checks),
		s.claudeRow(),
		s.dutiesRow(env),
		s.appRow(),
	}
	return st
}

func (s *Service) loginRow(env *cli.Env, checks map[string]cli.Check) Row {
	row := Row{ID: "login", Title: "Wargaming login"}
	logout := Action{ID: "logout", Label: "Log out"}
	relogin := Action{ID: "relogin", Label: "Log in again", Waits: true}

	switch {
	case env.ConfigErr != nil:
		row.State, row.Summary = stateFail, "The settings file cannot be read."
		row.Lines = []string{checks["config"].Detail}
		row.Hint = "Fix or delete the file named above, then reopen this window."
		return row
	case checks["secrets"].Status == cli.StatusFail:
		row.State, row.Summary = stateFail, "This build of Tank Advisor cannot reach Wargaming: it carries no application id."
		row.Hint = "Install a release build of Tank Advisor."
		return row
	case !env.Config.Configured():
		row.State, row.Summary = stateFail, "No account yet."
		row.Hint = "Click Set up. It walks you through choosing your server and logging in on Wargaming's own page; Tank Advisor never sees your password."
		row.Actions = []Action{{ID: "setup", Label: "Set up", Primary: true}}
		return row
	}

	row.Lines = []string{"Account: " + env.Config.Account.Nickname + " on " + realmName(env.Config.Account.Realm)}
	login := cli.StoredLogin(env)
	auth := checks["wg-auth"]
	switch {
	case !login.Stored:
		row.State, row.Summary = stateWarn, "Not logged in. Only public data can be fetched."
		row.Hint = "Click Log in. Your browser opens Wargaming's own login page."
		row.Actions = []Action{{ID: "login", Label: "Log in", Primary: true, Waits: true}}
		return row
	case login.ExpiresAt.IsZero():
		row.State, row.Summary = stateWarn, "Logged in, but the login's expiry is unknown."
		row.Hint = "Click Log in again."
		relogin.Primary = true
		row.Actions = []Action{relogin, logout}
		return row
	case !login.Valid:
		row.State = stateFail
		row.Summary = "The login expired on " + login.ExpiresAt.Local().Format("2 Jan 2006") + ". Syncing has stopped."
		row.Hint = "Click Log in again."
		relogin.Primary = true
		row.Actions = []Action{relogin, logout}
		return row
	}

	left := time.Until(login.ExpiresAt)
	row.Summary = fmt.Sprintf("Logged in until %s (%s left).", login.ExpiresAt.Local().Format("2 Jan 2006, 15:04"), roundDuration(left))
	row.State = stateOK
	renew := Action{ID: "renew", Label: "Renew"}
	if auth.Status == cli.StatusWarn {
		row.State = stateWarn
		row.Hint = "Click Renew to extend it by two weeks."
		renew.Primary = true
	}
	row.Actions = []Action{renew, relogin, logout}
	return row
}

func (s *Service) dataRow(ctx context.Context, env *cli.Env, checks map[string]cli.Check) Row {
	row := Row{ID: "data", Title: "Data"}
	sync := Action{ID: "sync", Label: "Sync now"}
	del := Action{ID: "delete-data", Label: "Delete my data", Danger: true, Confirm: deleteConfirm(env)}

	if env.ConfigErr != nil {
		row.State, row.Summary = stateUnknown, "Unknown until the settings file can be read."
		return row
	}
	if !env.HasStore() {
		row.State, row.Summary = stateWarn, "Nothing synced yet."
		if env.Config.Configured() {
			row.Hint = "Click Sync now."
			sync.Primary = true
			row.Actions = []Action{sync}
		} else {
			row.Hint = "Log in first."
		}
		return row
	}

	fetched := []string{"data-age", "vehicles", "wn8"}
	var states []string
	for _, name := range append(fetched, "overlay") {
		states = append(states, string(checks[name].Status))
	}
	row.State = worst(states...)
	h := readHistory(ctx, env)
	row.Summary = h.lastSync
	if row.Summary == "" {
		row.Summary = "Synced data is present."
	}
	row.Lines = []string{
		"Sources: " + checks["data-age"].Detail,
		"Vehicles: " + checks["vehicles"].Detail,
		"WN8 table: " + checks["wn8"].Detail,
		"Your settings: " + checks["overlay"].Detail,
	}
	if h.gap != "" {
		row.Lines = append(row.Lines, h.gap)
	}

	for _, name := range fetched {
		if checks[name].Status == cli.StatusWarn || checks[name].Status == cli.StatusFail {
			row.Hint = "Click Sync now."
			sync.Primary = true
			break
		}
	}
	if c := checks["overlay"]; c.Status == cli.StatusFail {
		row.Hint = strings.TrimSpace(row.Hint + " Your settings file has an error; fix it and save: " + env.Config.OverlayFile(env.Paths))
	}
	row.Actions = []Action{sync, del}
	return row
}

// history is the data row's reading of the cache: when the account was last
// synced, and the widest hole in its history, the limit on how finely recent
// form can be split.
type history struct {
	lastSync string
	gap      string
}

func readHistory(ctx context.Context, env *cli.Env) history {
	var h history
	db, err := env.OpenStore(ctx)
	if err != nil {
		return h
	}
	defer db.Close()
	if ages, err := db.DataAges(ctx, time.Now()); err == nil {
		// Newest first. The mod's dump ages with the game, not with syncs.
		for _, a := range ages {
			if !strings.HasPrefix(a.Source, "mod:") {
				h.lastSync = "Last synced " + ago(time.Duration(a.AgeSeconds)*time.Second) + "."
				break
			}
		}
	}
	if gap, ok, err := db.LongestGap(ctx, "wg", "account/info"); err == nil && ok {
		h.gap = fmt.Sprintf("Longest gap between syncs: %s (%s to %s)", roundDuration(gap.Length()),
			gap.From.Local().Format("2 Jan"), gap.To.Local().Format("2 Jan 2006"))
	}
	return h
}

func ago(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	return roundDuration(d) + " ago"
}

func deleteConfirm(env *cli.Env) string {
	return "This deletes the synced data and its history, the client mod's dump, and your Wargaming login from this computer. " +
		"History cannot be fetched again: recent form starts over from the next sync. " +
		"Your advice settings (" + env.Config.OverlayFile(env.Paths) + ") are kept.\n\n" +
		"If Claude Desktop is open, close it first."
}

// modState is what the game folder says about the mod.
type modState struct {
	gameDir, gameVersion, modsDir string
	gameErr                       error
	installed                     []string
}

func (s *Service) modState(context.Context) modState {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	var st modState
	if env.ConfigErr != nil {
		st.gameErr = env.ConfigErr
		return st
	}
	st.gameDir, st.gameErr = s.gameDir(env)
	if st.gameErr == nil {
		st.gameVersion, st.gameErr = game.Version(st.gameDir)
	}
	if st.gameErr == nil {
		st.modsDir, st.gameErr = game.ModsDir(st.gameDir)
	}
	if st.gameErr == nil {
		st.installed, _ = game.InstalledPackages(st.modsDir)
	}
	return st
}

func (s *Service) modRow(ctx context.Context, env *cli.Env, checks map[string]cli.Check) Row {
	row := Row{ID: "mod", Title: "Client mod"}
	st := s.modState(ctx)
	pkg := s.modPackage()

	if st.gameErr != nil {
		row.State, row.Summary = stateWarn, "World of Tanks was not found on this computer."
		row.Lines = []string{env.Redactor().RedactError(st.gameErr)}
		row.Hint = "Tank Advisor looks where Game Center and Steam install the game, and at a running game. If yours is elsewhere, click Choose game folder."
		row.Actions = []Action{{ID: "game-choose", Label: "Choose game folder", Primary: true}}
		return row
	}

	row.Lines = []string{fmt.Sprintf("Game %s in %s", st.gameVersion, st.gameDir)}
	install := Action{ID: "mod-install", Label: "Reinstall"}
	folder := Action{ID: "mod-folder", Label: "Open mods folder"}

	var state string
	switch {
	case len(st.installed) == 0:
		state = stateWarn
		row.Summary = "Not installed for game " + st.gameVersion + ". Vehicle XP, marks, loadouts and crew are unavailable."
		install.Label, install.Primary = "Install", true
		row.Hint = "Click Install, then start the game and wait in the garage for a few seconds."
	default:
		state = stateOK
		row.Summary = "Installed for game " + st.gameVersion + ": " + strings.Join(st.installed, ", ")
		if pkg != "" && !contains(st.installed, filepath.Base(pkg)) {
			state = stateWarn
			row.Summary += fmt.Sprintf(". This app carries version %s.", modPackageVersion(pkg))
			install.Label, install.Primary = "Update", true
			row.Hint = "Click Update while the game is closed."
		}
	}
	s.mu.Lock()
	needsPermission := s.modNeedsPermission
	s.mu.Unlock()
	if needsPermission && install.Primary {
		row.Hint = "The game was updated, and its folder needs administrator permission for the mod. Click " + install.Label + ": Windows asks for permission, for this one copy."
	}
	if env.Config.Mod.Managed {
		row.Lines = append(row.Lines, "Tank Advisor installs the mod again after each game update.")
	}
	dump := checks["mod-data"]
	row.Lines = append(row.Lines, "Last dump: "+dump.Detail)
	row.State = worst(state, string(dump.Status))
	// doctor's fix says which of two things the dump needs: a sync to take
	// a dump the mod has already written, or a visit to the garage to write
	// a fresh one.
	needsSync := dump.Fix == "wotctx sync"
	if row.Hint == "" && (dump.Status == cli.StatusWarn || dump.Status == cli.StatusFail) {
		if needsSync {
			row.Hint = "The mod has written data that is not synced yet. Click Sync now."
		} else {
			row.Hint = "Start the game and wait in the garage for a few seconds, then click Sync now."
		}
	}
	if needsSync {
		row.Actions = append(row.Actions, Action{ID: "sync", Label: "Sync now", Primary: !install.Primary})
	}

	if pkg != "" {
		row.Actions = append(row.Actions, install)
	} else {
		row.Lines = append(row.Lines, "This build carries no mod package to install.")
	}
	row.Actions = append(row.Actions, folder)
	return row
}

func (s *Service) claudeRow() Row {
	row := Row{ID: "claude", Title: "Claude"}
	c := s.opts.Claude.Detect()
	bundle := s.bundle()
	want := bundleVersion(bundle)
	export := Action{ID: "export", Label: "Export for claude.ai"}

	extOK := false
	switch {
	case !c.Desktop:
		row.Lines = append(row.Lines, "Claude Desktop: not found")
	case !c.ExtensionKnown:
		row.Lines = append(row.Lines, "Claude Desktop: installed; whether the extension is installed is unknown")
	case c.Extension == "":
		row.Lines = append(row.Lines, "Claude Desktop: installed, without the Tank Advisor extension")
	case bundle != "" && c.Extension != want:
		row.Lines = append(row.Lines, fmt.Sprintf("Claude Desktop: extension %s installed; this app carries %s", c.Extension, want))
	default:
		extOK = true
		row.Lines = append(row.Lines, "Claude Desktop: extension "+c.Extension+" installed")
	}
	switch {
	case !c.PluginKnown:
	case c.Plugin:
		row.Lines = append(row.Lines, "Claude Code: the wot plugin is installed")
	default:
		row.Lines = append(row.Lines, "Claude Code: the wot plugin is not installed")
	}
	if at := lastExport(); !at.IsZero() {
		row.Lines = append(row.Lines, "claude.ai export: last written "+at.Local().Format("2 Jan 2006, 15:04")+" in "+exportDir())
	}

	switch {
	case extOK || (c.Plugin && !c.Desktop):
		row.State, row.Summary = stateOK, "Claude can read your data."
	case c.Desktop && c.Extension != "" && c.ExtensionKnown:
		row.State, row.Summary = stateWarn, "The Claude Desktop extension is out of date."
		row.Hint = "Click Update extension, then click Install in Claude Desktop."
	case c.Desktop:
		row.State, row.Summary = stateWarn, "Claude Desktop cannot read your data yet."
		row.Hint = "Click Install extension, then click Install in Claude Desktop."
	case c.Plugin:
		row.State, row.Summary = stateOK, "Claude Code can read your data."
	default:
		row.State, row.Summary = stateWarn, "Claude Desktop was not found."
		row.Hint = "Install Claude Desktop, then come back here and install the extension."
		row.Actions = []Action{{ID: "claude-get", Label: "Get Claude Desktop", Primary: true}, export}
		return row
	}

	if c.Desktop && bundle != "" && !extOK {
		label := "Install extension"
		if c.Extension != "" {
			label = "Update extension"
		}
		row.Actions = append(row.Actions, Action{ID: "claude-ext", Label: label, Primary: true})
	} else if c.Desktop && bundle != "" {
		row.Actions = append(row.Actions, Action{ID: "claude-ext", Label: "Reinstall extension"})
	}
	row.Actions = append(row.Actions, export)
	return row
}

func lastExport() time.Time {
	dir := exportDir()
	if dir == "" {
		return time.Time{}
	}
	info, err := os.Stat(filepath.Join(dir, "wot-brief.md"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func (s *Service) appRow() Row {
	row := Row{ID: "app", Title: "Tank Advisor", State: stateOK}
	row.Summary = "Version " + s.opts.Version + "."

	s.mu.Lock()
	u := s.update
	s.mu.Unlock()
	check := Action{ID: "update-check", Label: "Check for updates"}
	switch {
	case u.CheckedAt.IsZero():
	case u.Err != "":
		row.Lines = append(row.Lines, "Update check failed: "+u.Err)
	case u.Newer:
		row.State = stateWarn
		row.Lines = append(row.Lines, "Version "+u.Latest+" is available.")
		if _, ok := s.opts.Updates.(UpdateInstaller); ok {
			row.Hint = "Click Update. Tank Advisor downloads it, checks its signature, and restarts."
			row.Actions = append(row.Actions, Action{ID: "update-install", Label: "Update", Primary: true},
				Action{ID: "release-page", Label: "What's new"})
		} else {
			row.Hint = "Click Open release page to download it."
			row.Actions = append(row.Actions, Action{ID: "release-page", Label: "Open release page", Primary: true})
		}
	default:
		row.Lines = append(row.Lines, u.Note)
	}
	row.Actions = append(row.Actions, check)

	if s.opts.Host != nil {
		on, err := s.opts.Host.Autostart()
		switch {
		case err != nil:
			row.Lines = append(row.Lines, "Start with Windows: unknown ("+err.Error()+")")
		case on:
			row.Lines = append(row.Lines, "Starts with Windows.")
			row.Actions = append(row.Actions, Action{ID: "autostart-off", Label: "Don't start with Windows"})
		default:
			row.Lines = append(row.Lines, "Does not start with Windows, so nothing syncs until you open it.")
			row.Actions = append(row.Actions, Action{ID: "autostart-on", Label: "Start with Windows"})
		}
	}
	return row
}

// roundDuration renders a duration the way the window reads it.
func roundDuration(d time.Duration) string {
	switch {
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute", "minutes")
	case d < 48*time.Hour:
		return plural(int(d.Hours()), "hour", "hours")
	default:
		return plural(int(d.Hours()/24), "day", "days")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
