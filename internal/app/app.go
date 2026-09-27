// Package app is the Tank Advisor app's logic, without its window: the status
// rows of docs/spec-desktop.md section 5.2 and the actions behind their
// buttons. It is `wotctx doctor` with buttons. The checks come from
// internal/cli and every action runs a wotctx command in process, so nothing
// here re-implements the core. cmd/tankadvisor puts it on screen.
package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ondrejkouril/tank-advisor/internal/cli"
	"github.com/ondrejkouril/tank-advisor/internal/game"
)

// Host is what the app needs from its window toolkit and the desktop.
type Host interface {
	OpenURL(url string) error
	// ShowInFolder opens Explorer at path: the folder itself, or the folder
	// holding a file with the file selected.
	ShowInFolder(path string) error
	Autostart() (bool, error)
	SetAutostart(on bool) error
	// ChooseFolder asks the player for a folder; "" means cancelled.
	ChooseFolder(title, start string) (string, error)
	// InstallModElevated copies the mod package into a game folder the
	// player cannot write to, with administrator permission that Windows
	// asks for.
	InstallModElevated(pkg, gameDir string) error
}

// Options configures a Service.
type Options struct {
	Version string
	// PayloadDir holds what ships beside the app: the client mod package
	// and the Claude Desktop bundle.
	PayloadDir string
	Host       Host
	// NewEnv builds a fresh wotctx environment writing to the given streams.
	// The default is cli.Bootstrap; tests pass an isolated one.
	NewEnv func(stdout, stderr io.Writer) *cli.Env
	// These are replaced in tests.
	Claude     ClaudeLocator
	ClaudeCode ClaudeCode
	Updates    UpdateChecker
	// FindGames lists the game clients for a realm ("" for any), best
	// first. The default is internal/game's detection.
	FindGames func(realm string) []game.Install
}

// Service is bound to the window: the frontend calls Status and Do.
type Service struct {
	opts Options

	// busy holds one action at a time. A second click while a sync or a
	// login runs is answered at once rather than queued behind it.
	busy    sync.Mutex
	mu      sync.Mutex
	running string
	// update is the last update check's answer; zero until one has run.
	update UpdateResult
	// modNeedsPermission is set when the mod upkeep found a game update but
	// cannot write to the game folder without administrator permission.
	modNeedsPermission bool
	// duties are the background duties, once the app has started them.
	duties *Duties
}

// New returns a Service with defaults filled in.
func New(opts Options) *Service {
	if opts.NewEnv == nil {
		version := opts.Version
		opts.NewEnv = func(stdout, stderr io.Writer) *cli.Env { return cli.Bootstrap(stdout, stderr, version) }
	}
	if opts.Claude.LocalAppData == "" && opts.Claude.AppData == "" && opts.Claude.Home == "" {
		opts.Claude = DefaultClaudeLocator()
	}
	if opts.Updates == nil {
		opts.Updates = GitHubUpdates{}
	}
	if opts.ClaudeCode == nil {
		opts.ClaudeCode = ClaudeCLI{}
	}
	return &Service{opts: opts}
}

// Result is what an action reports back to the window.
type Result struct {
	OK bool `json:"ok"`
	// Message is one line for the player.
	Message string `json:"message"`
	// Output is what the command printed, for the details view.
	Output string `json:"output,omitempty"`
}

// Running names the action in progress, or "" when idle.
func (s *Service) Running() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Do runs one action by id. arg carries the choice some actions take (the
// server for a first login).
func (s *Service) Do(ctx context.Context, id, arg string) Result {
	a, ok := actions[id]
	if !ok {
		return Result{Message: "unknown action " + id}
	}
	if !s.busy.TryLock() {
		return Result{Message: "Busy: " + s.Running() + ". Try again when it finishes."}
	}
	defer s.busy.Unlock()
	s.mu.Lock()
	s.running = a.label
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = ""
		s.mu.Unlock()
	}()
	return a.run(ctx, s, arg)
}

// action is one button's behaviour.
type action struct {
	label string // what is running, for "Busy: ..."
	run   func(ctx context.Context, s *Service, arg string) Result
}

var actions = map[string]action{
	"login": {"logging in", func(ctx context.Context, s *Service, arg string) Result { return s.login(ctx, arg) }},
	"relogin": {"logging in", func(ctx context.Context, s *Service, _ string) Result {
		return s.wotctx(ctx, "Logged in.", "auth", "wg", "--relogin")
	}},
	"renew": {"renewing the login", func(ctx context.Context, s *Service, _ string) Result {
		return s.wotctx(ctx, "The login is renewed.", "auth", "wg", "--prolong")
	}},
	"logout": {"logging out", func(ctx context.Context, s *Service, _ string) Result {
		return s.wotctx(ctx, "Logged out. The token is revoked and removed from this computer.", "auth", "wg", "--logout")
	}},
	"sync": {"syncing", func(ctx context.Context, s *Service, _ string) Result { return s.sync(ctx) }},
	"delete-data": {"deleting your data", func(ctx context.Context, s *Service, _ string) Result {
		return s.wotctx(ctx, "Your data is deleted. Your advice settings are kept.", "data", "delete", "--yes")
	}},
	"mod-install":    {"installing the mod", func(ctx context.Context, s *Service, _ string) Result { return s.installMod(ctx) }},
	"mod-folder":     {"opening the mods folder", func(ctx context.Context, s *Service, _ string) Result { return s.openModsFolder() }},
	"claude-ext":     {"installing the Claude extension", func(ctx context.Context, s *Service, _ string) Result { return s.installExtension() }},
	"claude-get":     {"opening the Claude download page", func(_ context.Context, s *Service, _ string) Result { return s.open(claudeDownloadURL) }},
	"export":         {"exporting for claude.ai", func(ctx context.Context, s *Service, _ string) Result { return s.export(ctx) }},
	"update-check":   {"checking for updates", func(ctx context.Context, s *Service, _ string) Result { return s.checkUpdates(ctx) }},
	"update-install": {"installing the update", func(ctx context.Context, s *Service, _ string) Result { return s.installUpdate(ctx) }},
	"release-page":   {"opening the release page", func(_ context.Context, s *Service, _ string) Result { return s.openRelease() }},
	"autostart-on": {"switching on start with Windows", func(_ context.Context, s *Service, _ string) Result {
		return s.setAutostart(true)
	}},
	"autostart-off": {"switching off start with Windows", func(_ context.Context, s *Service, _ string) Result {
		return s.setAutostart(false)
	}},

	// The setup wizard's steps (docs/spec-desktop.md section 5.1).
	"agree":        {"recording your agreement", func(_ context.Context, s *Service, _ string) Result { return s.agree() }},
	"set-realm":    {"choosing the server", func(_ context.Context, s *Service, arg string) Result { return s.setRealm(arg) }},
	"game-use":     {"choosing the game folder", func(_ context.Context, s *Service, arg string) Result { return s.useGame(arg) }},
	"game-choose":  {"choosing the game folder", func(_ context.Context, s *Service, _ string) Result { return s.chooseGame() }},
	"claude-code":  {"installing the Claude Code plugin", func(ctx context.Context, s *Service, _ string) Result { return s.installPlugin(ctx) }},
	"advice-save":  {"saving your advice settings", func(_ context.Context, s *Service, arg string) Result { return s.saveAdvice(arg) }},
	"setup-finish": {"finishing setup", func(_ context.Context, s *Service, _ string) Result { return s.finishSetup() }},

	// The Goals and Advice pages (docs/spec-desktop.md sections 5.3 and 8.4).
	"advice-page-save": {"saving your advice", func(ctx context.Context, s *Service, arg string) Result { return s.saveAdvicePage(ctx, arg) }},
	"goals-save":       {"saving your goals", func(ctx context.Context, s *Service, arg string) Result { return s.saveGoalsPage(ctx, arg) }},

	// The background duties (docs/spec-desktop.md section 5.6).
	"duty-pause":  {"pausing a background duty", func(_ context.Context, s *Service, arg string) Result { return s.setDutyPaused(arg, true) }},
	"duty-resume": {"resuming a background duty", func(_ context.Context, s *Service, arg string) Result { return s.setDutyPaused(arg, false) }},
}

// wotctx runs one wotctx command in process and reports it.
func (s *Service) wotctx(ctx context.Context, done string, args ...string) Result {
	var out bytes.Buffer
	env := s.opts.NewEnv(&out, &out)
	err := cli.Run(ctx, env, args)
	output := strings.TrimSpace(env.Redactor().Redact(out.String()))
	switch {
	case errors.Is(err, context.Canceled):
		return Result{Message: "Cancelled.", Output: output}
	case err != nil:
		return Result{Message: env.Redactor().RedactError(err), Output: output}
	}
	return Result{OK: true, Message: done, Output: output}
}

func (s *Service) login(ctx context.Context, realm string) Result {
	args := []string{"auth", "wg"}
	if realm != "" {
		args = append(args, "--realm", realm)
	}
	return s.wotctx(ctx, "Logged in.", args...)
}

func (s *Service) openModsFolder() Result {
	st := s.modState(context.Background())
	if st.modsDir == "" {
		return Result{Message: "World of Tanks was not found."}
	}
	if err := os.MkdirAll(st.modsDir, 0o755); err != nil {
		return Result{Message: err.Error()}
	}
	return s.show(st.modsDir)
}

func (s *Service) installExtension() Result {
	bundle := s.bundle()
	if bundle == "" {
		return Result{Message: "This build carries no Claude Desktop extension."}
	}
	if err := s.opts.Claude.Install(bundle); err != nil {
		// Claude Desktop does not claim .mcpb files, so the documented
		// fallback is dragging the file onto its Extensions page.
		s.show(bundle)
		return Result{Message: "Claude Desktop did not open it. In Claude Desktop, open Settings → Extensions and drag this file onto the page: " + bundle}
	}
	return Result{OK: true, Message: "Claude Desktop is asking to install the extension. Click Install there."}
}

// exportDir is where the claude.ai export goes: a folder the player finds
// again when uploading.
func exportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Documents", "Tank Advisor", "claude.ai")
}

func (s *Service) export(ctx context.Context) Result {
	dir := exportDir()
	if dir == "" {
		return Result{Message: "No home folder to export into."}
	}
	r := s.wotctx(ctx, "Exported. Upload wot-advisor.zip once as a skill, and wot-brief.md to your claude.ai Project.",
		"export", "claude-ai", "--out", dir)
	if r.OK {
		s.show(dir)
	}
	return r
}

func (s *Service) open(url string) Result {
	if s.opts.Host == nil {
		return Result{Message: "cannot open " + url}
	}
	if err := s.opts.Host.OpenURL(url); err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true}
}

func (s *Service) show(path string) Result {
	if s.opts.Host == nil {
		return Result{Message: "cannot open " + path}
	}
	if err := s.opts.Host.ShowInFolder(path); err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true}
}

func (s *Service) setAutostart(on bool) Result {
	if s.opts.Host == nil {
		return Result{Message: "start with Windows is not available"}
	}
	if err := s.opts.Host.SetAutostart(on); err != nil {
		return Result{Message: err.Error()}
	}
	if on {
		return Result{OK: true, Message: "Tank Advisor starts with Windows."}
	}
	return Result{OK: true, Message: "Tank Advisor no longer starts with Windows."}
}

// payload finds the highest-versioned file matching pattern in the payload
// folder. An installed app has one of each; a development folder may hold
// several builds.
func (s *Service) payload(pattern string, versionOf func(string) string) string {
	if s.opts.PayloadDir == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(s.opts.PayloadDir, pattern))
	var (
		best    string
		bestVer version
	)
	for _, m := range matches {
		v, ok := parseVersion(versionOf(m))
		if !ok {
			continue
		}
		if best == "" || compareVersions(v, bestVer) > 0 {
			best, bestVer = m, v
		}
	}
	return best
}

func (s *Service) modPackage() string {
	return s.payload("ondrejkouril.wotctx_*.wotmod", modPackageVersion)
}

func (s *Service) bundle() string { return s.payload("wotctx-*.mcpb", bundleVersion) }

// bundleVersion reads the version from the bundle's file name,
// wotctx-<version>.mcpb.
func bundleVersion(path string) string {
	name := filepath.Base(path)
	return strings.TrimSuffix(strings.TrimPrefix(name, "wotctx-"), ".mcpb")
}

// modPackageVersion reads the version from ondrejkouril.wotctx_<version>.wotmod.
func modPackageVersion(name string) string {
	return strings.TrimSuffix(strings.TrimPrefix(filepath.Base(name), "ondrejkouril.wotctx_"), ".wotmod")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
