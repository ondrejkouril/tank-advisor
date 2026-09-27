package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/cli"
	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/game"
	"github.com/ondrejkouril/tank-advisor/internal/mod"
	"gopkg.in/yaml.v3"
)

// noticeVersion numbers the setup notice. Raising it asks every player to
// agree again, at the next start.
const noticeVersion = 1

// notice is what the player agrees to at the first step (docs/spec-desktop.md
// sections 5.1 and 12.3). A change to its meaning raises noticeVersion.
var notice = []string{
	"Tank Advisor lets Claude answer questions about your own World of Tanks account with real figures instead of guesses.",
	"It stores your account statistics, garage, resources and, if you install the client mod, what the game client shows in the garage. All of it stays on this computer, under your Windows account. Nothing is sent to the makers of Tank Advisor: there is no server.",
	"It talks to Wargaming, through its public API and its own login page, which means Tank Advisor never sees your password. It reads the public pages of XVM and tomato.gg for reference values; those requests carry no account data. It asks GitHub whether an update exists.",
	"When you ask Claude about your account, Claude receives the figures it needs to answer, under your own agreement with Anthropic.",
	"You can delete everything at any time with Delete my data.",
	"Tank Advisor is not affiliated with or endorsed by Wargaming.",
}

// examples are the questions offered at the end of setup: each one exercises
// a different part of the data.
var examples = []string{
	"What tank should I get next?",
	"How have I been playing my medium tanks lately?",
	"How close am I to the next mark on my best tank?",
}

// Wizard is the setup's state: every step, whether it is done, and what each
// step's page shows (docs/spec-desktop.md section 5.1).
type Wizard struct {
	Completed bool         `json:"completed"`
	Steps     []WizardStep `json:"steps"`
	// First is the first step not yet done, where a resumed setup opens.
	First string `json:"first"`

	Notice []string `json:"notice"`

	Realm       string   `json:"realm"`
	RealmLocked bool     `json:"realmLocked"`
	Realms      []Choice `json:"realms"`
	Account     string   `json:"account"`

	Games       []GameChoice `json:"games"`
	GameDir     string       `json:"gameDir"`
	GameVersion string       `json:"gameVersion"`
	GameProblem string       `json:"gameProblem,omitempty"`

	Mod       ModStep    `json:"mod"`
	Claude    ClaudeStep `json:"claude"`
	Questions []Question `json:"questions"`
	Examples  []string   `json:"examples"`
}

// WizardStep is one step in the list.
type WizardStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// GameChoice is one detected game folder.
type GameChoice struct {
	Dir    string `json:"dir"`
	Label  string `json:"label"`
	Chosen bool   `json:"chosen"`
}

// ModStep is the mod step's page.
type ModStep struct {
	Package   string `json:"package"`
	Installed bool   `json:"installed"`
	// Dump describes the newest dump, or "" when there is none.
	Dump        string `json:"dump"`
	DumpArrived bool   `json:"dumpArrived"`
	// OtherAccount is set when the newest dump came from another account:
	// the game is logged in as someone else.
	OtherAccount bool `json:"otherAccount"`
}

// ClaudeStep is the Claude step's page.
type ClaudeStep struct {
	Desktop          bool   `json:"desktop"`
	Extension        string `json:"extension"`
	ExtensionCurrent bool   `json:"extensionCurrent"`
	Bundle           string `json:"bundle"`
	CodeCLI          bool   `json:"codeCLI"`
	Plugin           bool   `json:"plugin"`
}

// Wizard reads the setup's state. Like Status, it has no side effects.
func (s *Service) Wizard(ctx context.Context) Wizard {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	w := Wizard{Notice: notice, Realms: realms, Examples: examples}
	if env.ConfigErr != nil {
		w.Steps = []WizardStep{{ID: "welcome", Title: "Welcome"}}
		w.First = "welcome"
		return w
	}
	cfg := env.Config
	w.Completed = cfg.Setup.Completed != ""

	// Server: the configured realm, or the one of a game found here.
	w.Realm = cfg.Account.Realm
	w.RealmLocked = cfg.Configured()
	if !cfg.Configured() && !hasKey(env.Paths.ConfigFile, "account", "realm") {
		if found := s.findGames(""); len(found) > 0 {
			w.Realm = found[0].Realm
		}
	}
	if cfg.Configured() {
		w.Account = cfg.Account.Nickname + " on " + realmName(cfg.Account.Realm)
	}
	login := cli.StoredLogin(env)

	// Game.
	chosen, gameErr := s.gameDir(env)
	for _, g := range s.findGames(w.Realm) {
		w.Games = append(w.Games, GameChoice{Dir: g.Dir, Label: gameLabel(g), Chosen: sameDir(g.Dir, chosen)})
	}
	if gameErr == nil {
		w.GameDir = chosen
		if v, err := game.Version(chosen); err == nil {
			w.GameVersion = v
		} else {
			gameErr = err
		}
	}
	if gameErr != nil {
		w.GameProblem = "World of Tanks for " + realmName(w.Realm) + " was not found. Choose its folder: the one that holds WorldOfTanks.exe's win64 folder and version.xml."
	}

	// Mod.
	w.Mod.Package = filepath.Base(s.modPackage())
	if w.Mod.Package == "." {
		w.Mod.Package = ""
	}
	st := s.modState(ctx)
	w.Mod.Installed = w.Mod.Package != "" && contains(st.installed, w.Mod.Package)
	if raw, err := os.ReadFile(cli.ModDumpPath(env)); err == nil {
		if d, err := mod.Parse(raw); err == nil {
			w.Mod.Dump = fmt.Sprintf("written %s by mod %s on game %s", d.CapturedAt.Local().Format("2 Jan 2006, 15:04"), d.ModVersion, d.GameVersion)
			switch {
			case cfg.Configured() && d.AccountID != cfg.Account.AccountID:
				w.Mod.OtherAccount = true
			case d.GameVersion == w.GameVersion:
				w.Mod.DumpArrived = true
			}
		}
	}

	// Claude.
	c := s.opts.Claude.Detect()
	w.Claude = ClaudeStep{Desktop: c.Desktop, Extension: c.Extension, Plugin: c.Plugin, CodeCLI: s.opts.ClaudeCode.Available()}
	if b := s.bundle(); b != "" {
		w.Claude.Bundle = bundleVersion(b)
	}
	w.Claude.ExtensionCurrent = c.Extension != "" && (w.Claude.Bundle == "" || c.Extension == w.Claude.Bundle)

	// Advice.
	w.Questions = questions(env.Config.OverlayFile(env.Paths))
	adviceSet := false
	for _, q := range w.Questions {
		adviceSet = adviceSet || q.Set
	}

	synced := hasAccountData(ctx, env)

	w.Steps = []WizardStep{
		{ID: "welcome", Title: "Welcome", Done: cfg.Consent.Notice >= noticeVersion},
		{ID: "server", Title: "Server", Done: cfg.Configured() || hasKey(env.Paths.ConfigFile, "account", "realm")},
		{ID: "login", Title: "Log in", Done: cfg.Configured() && login.Valid},
		{ID: "game", Title: "Game", Done: w.GameVersion != ""},
		{ID: "mod", Title: "Client mod", Done: w.Mod.DumpArrived},
		{ID: "claude", Title: "Claude", Done: w.Claude.ExtensionCurrent || w.Claude.Plugin},
		{ID: "advice", Title: "Your advice", Done: adviceSet},
		{ID: "sync", Title: "First sync", Done: synced},
	}
	for _, step := range w.Steps {
		if !step.Done {
			w.First = step.ID
			break
		}
	}
	if w.First == "" {
		w.First = "sync"
	}
	return w
}

// NeedsSetup reports whether the app should open on the wizard: it was never
// finished, or the notice changed since the player agreed.
func (s *Service) NeedsSetup() bool {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	return env.ConfigErr == nil && (env.Config.Setup.Completed == "" || env.Config.Consent.Notice < noticeVersion)
}

// gameDir is the game folder the app works with: game_dir from the config,
// else the best one found for the account's realm. It is wotctx's rule
// (internal/cli), over the app's own detection, so the two cannot disagree
// in a test that replaces one of them.
func (s *Service) gameDir(env *cli.Env) (string, error) {
	if env.Config.GameDir != "" {
		return env.Config.GameDir, nil
	}
	found := s.findGames(env.Config.Account.Realm)
	if len(found) == 0 {
		return "", errors.New("no World of Tanks client for " + realmName(env.Config.Account.Realm) + " was found")
	}
	return found[0].Dir, nil
}

func (s *Service) findGames(realm string) []game.Install {
	if s.opts.FindGames != nil {
		return s.opts.FindGames(realm)
	}
	return game.DefaultLocator().Find(realm)
}

func gameLabel(g game.Install) string {
	source := map[string]string{
		"game-center": "found by Game Center",
		"running":     "running now",
		"steam":       "installed by Steam",
		"default":     "in the default folder",
	}[g.Source]
	if source == "" {
		return g.Dir
	}
	return g.Dir + " (" + source + ")"
}

func sameDir(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// hasKey reports whether a YAML file sets a nested key, to tell a value the
// player chose from a default.
func hasKey(path string, keys ...string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return hasKeyIn(raw, keys...)
}

// hasKeyIn is hasKey for a file's contents.
func hasKeyIn(raw []byte, keys ...string) bool {
	var m map[string]any
	if yaml.Unmarshal(raw, &m) != nil {
		return false
	}
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		if cur, ok = mm[k]; !ok {
			return false
		}
	}
	return true
}

func today() string { return time.Now().Format("2006-01-02") }

func (s *Service) agree() Result {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	if err := config.Set(env.Paths.ConfigFile,
		config.Update{Key: "consent.notice", Value: noticeVersion},
		config.Update{Key: "consent.agreed", Value: today()},
	); err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true}
}

func (s *Service) setRealm(realm string) Result {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	valid := false
	for _, r := range realms {
		valid = valid || r.Value == realm
	}
	switch {
	case !valid:
		return Result{Message: "Choose Europe, North America or Asia."}
	case env.Config.Configured() && realm != env.Config.Account.Realm:
		return Result{Message: "This installation already holds " + env.Config.Account.Nickname + " on " +
			realmName(env.Config.Account.Realm) + ". To use another server, delete your data first."}
	}
	if err := config.Set(env.Paths.ConfigFile, config.Update{Key: "account.realm", Value: realm}); err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true}
}

// useGame records the game folder the player confirmed. The best detected
// folder is not written down, so detection keeps following the game if Game
// Center moves it; any other folder is, as game_dir.
func (s *Service) useGame(dir string) Result {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	dir, err := s.checkGameDir(env, dir)
	if err != nil {
		return Result{Message: err.Error()}
	}
	var value any = dir
	if found := s.findGames(env.Config.Account.Realm); len(found) > 0 && sameDir(found[0].Dir, dir) {
		value = nil
	}
	if err := config.Set(env.Paths.ConfigFile, config.Update{Key: "game_dir", Value: value}); err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true, Message: "Using " + dir + "."}
}

func (s *Service) chooseGame() Result {
	if s.opts.Host == nil {
		return Result{Message: "cannot show a folder picker"}
	}
	dir, err := s.opts.Host.ChooseFolder("Choose the World of Tanks folder", `C:\Games`)
	switch {
	case err != nil:
		return Result{Message: err.Error()}
	case dir == "":
		return Result{Message: "No folder chosen."}
	}
	return s.useGame(dir)
}

// checkGameDir accepts the game folder, or a folder inside it such as win64,
// holding the production client for the account's realm.
func (s *Service) checkGameDir(env *cli.Env, dir string) (string, error) {
	realm := env.Config.Account.Realm
	for _, candidate := range []string{dir, filepath.Dir(dir)} {
		id, err := game.ReadID(candidate)
		if err != nil {
			continue
		}
		switch r := game.RealmOfID(id); {
		case r == "":
			return "", fmt.Errorf("%s holds %s, a test client. Choose the one you play on", candidate, id)
		case r != realm:
			return "", fmt.Errorf("%s holds the %s client, but this installation is for %s", candidate, realmName(r), realmName(realm))
		}
		if _, err := game.Version(candidate); err != nil {
			return "", fmt.Errorf("%s: %w", candidate, err)
		}
		return candidate, nil
	}
	return "", errors.New(dir + " is not a World of Tanks folder: it has no game_info.xml")
}

func (s *Service) finishSetup() Result {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	if err := config.Set(env.Paths.ConfigFile, config.Update{Key: "setup.completed", Value: today()}); err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true, Message: "Setup is done. Ask Claude one of the questions above."}
}
