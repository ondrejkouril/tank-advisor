package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/cli"
	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/game"
	"github.com/ondrejkouril/tank-advisor/internal/secrets"
)

// fixture is one isolated installation: its own folders, keychain, game
// folder, payloads and Claude folders. Nothing reads this machine's.
type fixture struct {
	dir     string
	secrets *secrets.Memory
	paths   config.Paths
	game    string
	payload string
	claude  ClaudeLocator
	host    *fakeHost
	code    *fakeCode
	games   []game.Install
	svc     *Service
}

func newFixture(t *testing.T, configured bool) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{
		dir:     dir,
		secrets: secrets.NewMemory(),
		paths: config.Paths{
			ConfigDir: dir, ConfigFile: filepath.Join(dir, "config.yaml"),
			DataDir: dir, DBFile: filepath.Join(dir, "wotctx.db"),
		},
		game:    mkdir(t, dir, "game"),
		payload: mkdir(t, dir, "payload"),
		claude: ClaudeLocator{
			LocalAppData: mkdir(t, dir, "local"), AppData: mkdir(t, dir, "roaming"), Home: mkdir(t, dir, "home"),
		},
		host: &fakeHost{},
		code: &fakeCode{},
	}
	f.secrets.Set(secrets.WGApplicationID, "app")
	write(t, filepath.Join(f.game, "version.xml"), "<version.xml>\r\n\t<version> v.2.4.0.1 #952 </version>\r\n</version.xml>")

	cfg := "game_dir: " + filepath.ToSlash(f.game) + "\n"
	if configured {
		cfg += "account:\n  realm: com\n  account_id: 42\n  nickname: someone\n"
	}
	write(t, f.paths.ConfigFile, cfg)

	f.svc = New(Options{
		Version:    "v1.2.0",
		PayloadDir: f.payload,
		Host:       f.host,
		Claude:     f.claude,
		Updates:    fakeUpdates{},
		ClaudeCode: f.code,
		FindGames:  func(string) []game.Install { return f.games },
		NewEnv: func(stdout, stderr io.Writer) *cli.Env {
			env := &cli.Env{Stdout: stdout, Stderr: stderr, Version: "test", Paths: f.paths,
				Config: config.Default(), Secrets: f.secrets, Stdin: strings.NewReader("")}
			env.Config, env.ConfigErr = config.Load(f.paths.ConfigFile)
			return env
		},
	})
	return f
}

func (f *fixture) login(expires time.Time) {
	f.secrets.Set(secrets.WGAccessToken, "token")
	f.secrets.Set(secrets.WGTokenExpiry, expires.UTC().Format(time.RFC3339))
}

func (f *fixture) row(t *testing.T, id string) Row {
	t.Helper()
	for _, r := range f.svc.Status(context.Background()).Rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no %s row", id)
	return Row{}
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func actionIDs(r Row) []string {
	var ids []string
	for _, a := range r.Actions {
		ids = append(ids, a.ID)
	}
	return ids
}

func primary(r Row) string {
	for _, a := range r.Actions {
		if a.Primary {
			return a.ID
		}
	}
	return ""
}

type fakeHost struct {
	opened    []string
	shown     []string
	autostart bool
	folder    string   // what ChooseFolder answers
	elevated  []string // the game folders InstallModElevated was asked for
}

func (h *fakeHost) OpenURL(url string) error                    { h.opened = append(h.opened, url); return nil }
func (h *fakeHost) ShowInFolder(path string) error              { h.shown = append(h.shown, path); return nil }
func (h *fakeHost) Autostart() (bool, error)                    { return h.autostart, nil }
func (h *fakeHost) SetAutostart(on bool) error                  { h.autostart = on; return nil }
func (h *fakeHost) ChooseFolder(string, string) (string, error) { return h.folder, nil }
func (h *fakeHost) InstallModElevated(pkg, dir string) error {
	h.elevated = append(h.elevated, dir)
	return nil
}

type fakeCode struct {
	available, installed bool
}

func (c *fakeCode) Available() bool { return c.available }
func (c *fakeCode) InstallPlugin(context.Context) (string, error) {
	c.installed = true
	return "installed", nil
}

type fakeUpdates struct {
	rel   Release
	found bool
}

func (u fakeUpdates) Latest(context.Context) (Release, bool, error) { return u.rel, u.found, nil }

func TestAFreshInstallAsksForTheServerAndALogin(t *testing.T) {
	f := newFixture(t, false)
	st := f.svc.Status(context.Background())
	if st.Account != "" {
		t.Errorf("account = %q before any login", st.Account)
	}

	if !st.NeedsSetup {
		t.Error("a fresh install does not open on the setup")
	}
	login := f.row(t, "login")
	if login.State != stateFail || primary(login) != "setup" {
		t.Fatalf("login row = %+v", login)
	}

	data := f.row(t, "data")
	if data.Summary != "Nothing synced yet." || len(data.Actions) != 0 {
		t.Errorf("data row = %+v, want nothing to sync before a login", data)
	}
}

func TestLoginRowFollowsTheToken(t *testing.T) {
	cases := []struct {
		name    string
		expires time.Duration // from now; 0 means no token
		state   string
		primary string
		actions string
	}{
		{"no token", 0, stateWarn, "login", "login"},
		{"valid", 10 * 24 * time.Hour, stateOK, "", "renew relogin logout"},
		{"near expiry", 2 * 24 * time.Hour, stateWarn, "renew", "renew relogin logout"},
		{"expired", -time.Hour, stateFail, "relogin", "relogin logout"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, true)
			if c.expires != 0 {
				f.login(time.Now().Add(c.expires))
			}
			row := f.row(t, "login")
			if row.State != c.state || primary(row) != c.primary || strings.Join(actionIDs(row), " ") != c.actions {
				t.Errorf("row = state %s, primary %q, actions %v; want %s, %q, %s",
					row.State, primary(row), actionIDs(row), c.state, c.primary, c.actions)
			}
			if !strings.Contains(row.Lines[0], "someone on North America") {
				t.Errorf("lines = %v", row.Lines)
			}
		})
	}
}

func TestModRowOffersTheModThisAppCarries(t *testing.T) {
	f := newFixture(t, true)
	mods := mkdir(t, f.game, "mods", "2.4.0.1")

	// Nothing installed, and nothing to install.
	row := f.row(t, "mod")
	if row.State != stateWarn || strings.Join(actionIDs(row), " ") != "mod-folder" {
		t.Errorf("no package: %+v", row)
	}

	write(t, filepath.Join(f.payload, "ondrejkouril.wotctx_0.3.0.wotmod"), "pk")
	if row = f.row(t, "mod"); primary(row) != "mod-install" || row.Actions[0].Label != "Install" {
		t.Errorf("not installed: %+v", row)
	}

	write(t, filepath.Join(mods, "ondrejkouril.wotctx_0.2.1.wotmod"), "pk")
	if row = f.row(t, "mod"); row.State != stateWarn || row.Actions[0].Label != "Update" {
		t.Errorf("older installed: %+v", row)
	}

	write(t, filepath.Join(mods, "ondrejkouril.wotctx_0.3.0.wotmod"), "pk")
	os.Remove(filepath.Join(mods, "ondrejkouril.wotctx_0.2.1.wotmod"))
	if row = f.row(t, "mod"); row.Actions[0].Label != "Reinstall" || row.Actions[0].Primary {
		t.Errorf("current installed: %+v", row)
	}
}

func TestModRowSaysWhenTheGameIsMissing(t *testing.T) {
	f := newFixture(t, true)
	os.Remove(filepath.Join(f.game, "version.xml"))
	row := f.row(t, "mod")
	if row.State != stateWarn || !strings.Contains(row.Summary, "not found") || primary(row) != "game-choose" {
		t.Errorf("row = %+v", row)
	}
}

func TestClaudeRow(t *testing.T) {
	record := func(version string) string {
		return `{"extensions":{"local.mcpb.someone.wotctx":{"version":"` + version + `","manifest":{"name":"wotctx"}}}}`
	}
	cases := []struct {
		name     string
		desktop  bool
		record   string // "" for no file
		plugin   bool
		state    string
		primary  string
		contains string
	}{
		{"nothing", false, "", false, stateWarn, "claude-get", "not found"},
		{"desktop only", true, `{"extensions":{}}`, false, stateWarn, "claude-ext", "without the Tank Advisor extension"},
		{"old extension", true, record("0.4.0"), false, stateWarn, "claude-ext", "0.4.0 installed; this app carries 0.5.0"},
		{"current extension", true, record("0.5.0"), false, stateOK, "", "extension 0.5.0 installed"},
		{"unreadable record", true, "{not json", false, stateWarn, "claude-ext", "unknown"},
		{"plugin only", false, "", true, stateOK, "", "wot plugin is installed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, true)
			write(t, filepath.Join(f.payload, "wotctx-0.5.0.mcpb"), "zip")
			if c.desktop {
				write(t, filepath.Join(f.claude.LocalAppData, "AnthropicClaude", "claude.exe"), "")
			}
			if c.record != "" {
				write(t, filepath.Join(f.claude.AppData, "Claude", "extensions-installations.json"), c.record)
			}
			if c.plugin {
				write(t, filepath.Join(f.claude.Home, ".claude", "plugins", "installed_plugins.json"),
					`{"version":2,"plugins":{"wot@tank-advisor":[{}]}}`)
			}
			row := f.row(t, "claude")
			if row.State != c.state || primary(row) != c.primary {
				t.Errorf("state %s, primary %q; want %s, %q (%+v)", row.State, primary(row), c.state, c.primary, row)
			}
			if !strings.Contains(strings.Join(row.Lines, "\n"), c.contains) {
				t.Errorf("lines %v lack %q", row.Lines, c.contains)
			}
			if !contains(actionIDs(row), "export") {
				t.Error("export is always offered")
			}
		})
	}
}

func TestStatusHasNoSideEffects(t *testing.T) {
	f := newFixture(t, true)
	f.login(time.Now().Add(10 * 24 * time.Hour))
	f.svc.Status(context.Background())
	if _, err := os.Stat(f.paths.DBFile); !os.IsNotExist(err) {
		t.Errorf("Status created the cache: %v", err)
	}
}

func TestDeleteMyDataRemovesTheCacheAndTheLogin(t *testing.T) {
	f := newFixture(t, true)
	f.login(time.Now().Add(10 * 24 * time.Hour))
	write(t, f.paths.DBFile, "not really sqlite")

	r := f.svc.Do(context.Background(), "delete-data", "")
	if !r.OK {
		t.Fatalf("delete: %+v", r)
	}
	if _, err := os.Stat(f.paths.DBFile); !os.IsNotExist(err) {
		t.Error("the cache is still there")
	}
	if _, _, err := f.secrets.Get(secrets.WGAccessToken); err == nil {
		t.Error("the token is still stored")
	}
	if f.svc.Status(context.Background()).Account != "" {
		t.Error("the account is still recorded")
	}
}

func TestOneActionAtATime(t *testing.T) {
	f := newFixture(t, true)
	f.svc.busy.Lock()
	f.svc.running = "syncing"
	r := f.svc.Do(context.Background(), "sync", "")
	f.svc.busy.Unlock()
	if r.OK || !strings.Contains(r.Message, "Busy: syncing") {
		t.Errorf("result = %+v", r)
	}
	if r := f.svc.Do(context.Background(), "no-such-thing", ""); r.OK {
		t.Errorf("an unknown action succeeded: %+v", r)
	}
}

func TestAutostartToggles(t *testing.T) {
	f := newFixture(t, true)
	if !contains(actionIDs(f.row(t, "app")), "autostart-on") {
		t.Fatal("off: no switch to turn it on")
	}
	if r := f.svc.Do(context.Background(), "autostart-on", ""); !r.OK || !f.host.autostart {
		t.Fatalf("switching on: %+v", r)
	}
	if !contains(actionIDs(f.row(t, "app")), "autostart-off") {
		t.Error("on: no switch to turn it off")
	}
}

func TestUpdateCheck(t *testing.T) {
	f := newFixture(t, true)
	if r := f.svc.Do(context.Background(), "update-check", ""); !r.OK || r.Message != "No release is published yet." {
		t.Errorf("no release: %+v", r)
	}
	f.svc.opts.Updates = fakeUpdates{Release{Tag: "v1.3.0", URL: "https://example.test/r"}, true}
	f.svc.Do(context.Background(), "update-check", "")
	row := f.row(t, "app")
	if row.State != stateWarn || primary(row) != "release-page" {
		t.Fatalf("row = %+v", row)
	}
	f.svc.Do(context.Background(), "release-page", "")
	if len(f.host.opened) != 1 || f.host.opened[0] != "https://example.test/r" {
		t.Errorf("opened %v", f.host.opened)
	}
}

func TestCompareRelease(t *testing.T) {
	cases := []struct {
		current, tag string
		newer        bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.2.0", "v1.10.0", true},
		{"v1.0.0-rc.1", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-rc.2", false},
		{"v2.0.0", "v1.9.9", false},
		{"c3c518f-dirty", "v1.0.0", false}, // a development build
		{"v1.0.0", "nightly", false},
	}
	for _, c := range cases {
		if got, note := compareRelease(c.current, c.tag); got != c.newer {
			t.Errorf("compareRelease(%s, %s) = %v (%s), want %v", c.current, c.tag, got, note, c.newer)
		}
	}
}

func TestAboutCarriesTheNoticesWargamingRequires(t *testing.T) {
	f := newFixture(t, true)
	about := f.svc.Status(context.Background()).About
	text := strings.Join(about.Notices, "\n")
	for _, want := range []string{"© Wargaming.net. All rights reserved", "not affiliated with or endorsed by Wargaming", "© 2026", "comes from Wargaming.net"} {
		if !strings.Contains(text, want) {
			t.Errorf("notices lack %q", want)
		}
	}
	if about.Support.URL == "" || !strings.Contains(about.Support.Label, "Wargaming Support") {
		t.Errorf("support = %+v", about.Support)
	}
	if about.Links[0].URL != "https://worldoftanks.com/" {
		t.Errorf("game site for com = %s", about.Links[0].URL)
	}
}

func TestPayloadsArePickedByVersionNotFileTime(t *testing.T) {
	f := newFixture(t, true)
	write(t, filepath.Join(f.payload, "wotctx-0.10.0.mcpb"), "zip")
	write(t, filepath.Join(f.payload, "wotctx-0.9.0.mcpb"), "zip") // written last
	write(t, filepath.Join(f.payload, "wotctx-latest.mcpb"), "zip")
	if got := filepath.Base(f.svc.bundle()); got != "wotctx-0.10.0.mcpb" {
		t.Errorf("bundle = %s", got)
	}
}
