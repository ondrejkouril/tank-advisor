package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/game"
	"github.com/ondrejkouril/tank-advisor/internal/overlay"
	"github.com/ondrejkouril/tank-advisor/internal/store"
)

func (f *fixture) wizard(t *testing.T) Wizard {
	t.Helper()
	return f.svc.Wizard(context.Background())
}

func (f *fixture) do(t *testing.T, id, arg string) Result {
	t.Helper()
	return f.svc.Do(context.Background(), id, arg)
}

func (f *fixture) config(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(f.paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func step(w Wizard, id string) WizardStep {
	for _, s := range w.Steps {
		if s.ID == id {
			return s
		}
	}
	return WizardStep{}
}

// client makes dir a production (or test) client for realm, as detection
// reads one.
func client(t *testing.T, dir, id, version string) {
	t.Helper()
	write(t, filepath.Join(dir, "game_info.xml"), "<protocol><game><id>"+id+"</id></game></protocol>")
	write(t, filepath.Join(dir, "version.xml"), "<version.xml><version> v."+version+" #1 </version></version.xml>")
}

// modPackage writes a package mod install accepts into the payload folder.
func (f *fixture) modPackage(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(f.payload, "ondrejkouril.wotctx_"+version+".wotmod")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(out)
	for _, name := range []string{"meta.xml", "res/scripts/client/gui/mods/mod_wotctx.pyc"} {
		w, _ := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		w.Write([]byte("x"))
	}
	z.Close()
	out.Close()
	return path
}

func TestTheWizardStartsWithTheNoticeAndRecordsTheAgreement(t *testing.T) {
	f := newFixture(t, false)
	w := f.wizard(t)
	if w.First != "welcome" || w.Completed || len(w.Steps) != 8 {
		t.Fatalf("wizard = first %s, completed %v, %d steps", w.First, w.Completed, len(w.Steps))
	}
	text := strings.Join(w.Notice, " ")
	for _, want := range []string{"stays on this computer", "Claude receives the figures", "not affiliated", "never sees your password"} {
		if !strings.Contains(text, want) {
			t.Errorf("the notice lacks %q", want)
		}
	}

	if r := f.do(t, "agree", ""); !r.OK {
		t.Fatal(r.Message)
	}
	cfg := f.config(t)
	if cfg.Consent.Notice != noticeVersion || cfg.Consent.Agreed != time.Now().Format("2006-01-02") {
		t.Errorf("consent = %+v", cfg.Consent)
	}
	if w = f.wizard(t); w.First != "server" {
		t.Errorf("after agreeing, first = %s", w.First)
	}
}

func TestTheServerIsPreselectedFromTheGameFound(t *testing.T) {
	f := newFixture(t, false)
	f.games = []game.Install{{Dir: f.game, ID: "WOT.ASIA.PRODUCTION", Realm: "asia", Source: "game-center"}}
	if w := f.wizard(t); w.Realm != "asia" || w.RealmLocked {
		t.Errorf("realm = %s, locked %v; want asia from the game", w.Realm, w.RealmLocked)
	}

	// Once the player chooses, the choice stands.
	if r := f.do(t, "set-realm", "com"); !r.OK {
		t.Fatal(r.Message)
	}
	if w := f.wizard(t); w.Realm != "com" || !step(w, "server").Done {
		t.Errorf("realm = %s, server done %v", w.Realm, step(w, "server").Done)
	}
	if r := f.do(t, "set-realm", "ru"); r.OK {
		t.Error("an unknown server was accepted")
	}
}

func TestTheServerCannotChangeUnderAnAccount(t *testing.T) {
	f := newFixture(t, true) // someone on com
	if w := f.wizard(t); !w.RealmLocked {
		t.Error("the server is not locked once an account is set up")
	}
	if r := f.do(t, "set-realm", "eu"); r.OK || !strings.Contains(r.Message, "delete your data") {
		t.Errorf("result = %+v", r)
	}
}

func TestChoosingTheGameFolder(t *testing.T) {
	f := newFixture(t, true) // realm com; the fixture pins game_dir
	detected := mkdir(t, f.dir, "detected")
	client(t, detected, "WOT.NA.PRODUCTION", "2.4.0.1")
	f.games = []game.Install{{Dir: detected, ID: "WOT.NA.PRODUCTION", Realm: "com", Source: "game-center"}}

	// The detected folder is used without being written down.
	if r := f.do(t, "game-use", detected); !r.OK {
		t.Fatal(r.Message)
	}
	if got := f.config(t).GameDir; got != "" {
		t.Errorf("game_dir = %q, want it left to detection", got)
	}

	// Another folder is written down; its win64 subfolder is accepted too.
	other := mkdir(t, f.dir, "elsewhere")
	client(t, other, "WOT.NA.PRODUCTION", "2.4.0.1")
	f.host.folder = mkdir(t, other, "win64")
	if r := f.do(t, "game-choose", ""); !r.OK {
		t.Fatal(r.Message)
	}
	if got := f.config(t).GameDir; got != other {
		t.Errorf("game_dir = %q, want %q", got, other)
	}

	wrong := mkdir(t, f.dir, "eu")
	client(t, wrong, "WOT.EU.PRODUCTION", "2.4.0.1")
	if r := f.do(t, "game-use", wrong); r.OK || !strings.Contains(r.Message, "Europe") {
		t.Errorf("another realm's client: %+v", r)
	}
	ct := mkdir(t, f.dir, "ct")
	client(t, ct, "WOT.CT.PRODUCTION", "2.4.1.0")
	if r := f.do(t, "game-use", ct); r.OK || !strings.Contains(r.Message, "test client") {
		t.Errorf("the Common Test: %+v", r)
	}
	if r := f.do(t, "game-use", f.dir); r.OK {
		t.Error("a folder with no game in it was accepted")
	}
}

func TestInstallingTheModMakesItManaged(t *testing.T) {
	f := newFixture(t, true)
	f.modPackage(t, "0.3.0")
	if r := f.do(t, "mod-install", ""); !r.OK {
		t.Fatalf("%s\n%s", r.Message, r.Output)
	}
	mods := filepath.Join(f.game, "mods", "2.4.0.1")
	if _, err := os.Stat(filepath.Join(mods, "ondrejkouril.wotctx_0.3.0.wotmod")); err != nil {
		t.Fatal(err)
	}
	cfg := f.config(t)
	if !cfg.Mod.Managed || len(cfg.Mod.Placed) != 1 || !sameDir(cfg.Mod.Placed[0], mods) {
		t.Errorf("mod = %+v", cfg.Mod)
	}
}

func TestAProtectedGameFolderIsWrittenWithPermission(t *testing.T) {
	f := newFixture(t, true)
	f.modPackage(t, "0.3.0")
	canWrite = func(string) bool { return false }
	t.Cleanup(func() { canWrite = writable })

	if r := f.do(t, "mod-install", ""); !r.OK {
		t.Fatal(r.Message)
	}
	if len(f.host.elevated) != 1 || !sameDir(f.host.elevated[0], f.game) {
		t.Errorf("elevated = %v", f.host.elevated)
	}
	if !f.config(t).Mod.Managed {
		t.Error("not recorded as managed")
	}
}

// TestAGameUpdateGetsTheModWithoutAsking is plan step D6's simulated update:
// a new version.xml, and the mod lands in the new mods folder unasked.
func TestAGameUpdateGetsTheModWithoutAsking(t *testing.T) {
	f := newFixture(t, true)
	f.modPackage(t, "0.3.0")

	// Not managed: the player never had the app install it, so nothing happens.
	if msg := f.svc.KeepModInstalled(context.Background()); msg != "" {
		t.Fatalf("an unmanaged mod was installed: %s", msg)
	}
	if r := f.do(t, "mod-install", ""); !r.OK {
		t.Fatal(r.Message)
	}
	if msg := f.svc.KeepModInstalled(context.Background()); msg != "" {
		t.Errorf("nothing to do, yet: %s", msg)
	}

	write(t, filepath.Join(f.game, "version.xml"), "<version.xml><version> v.2.5.0.0 #1 </version></version.xml>")
	msg := f.svc.KeepModInstalled(context.Background())
	if !strings.Contains(msg, "2.5.0.0") {
		t.Fatalf("message = %q", msg)
	}
	if _, err := os.Stat(filepath.Join(f.game, "mods", "2.5.0.0", "ondrejkouril.wotctx_0.3.0.wotmod")); err != nil {
		t.Fatal(err)
	}
	if placed := f.config(t).Mod.Placed; len(placed) != 2 {
		t.Errorf("placed = %v, want both folders for the uninstaller", placed)
	}
}

func TestUpkeepWaitsForAClickWhenPermissionIsNeeded(t *testing.T) {
	f := newFixture(t, true)
	f.modPackage(t, "0.3.0")
	f.do(t, "mod-install", "")
	write(t, filepath.Join(f.game, "version.xml"), "<version.xml><version> v.2.5.0.0 #1 </version></version.xml>")
	canWrite = func(string) bool { return false }
	t.Cleanup(func() { canWrite = writable })

	if msg := f.svc.KeepModInstalled(context.Background()); msg != "" {
		t.Errorf("upkeep did something without permission: %s", msg)
	}
	if len(f.host.elevated) != 0 {
		t.Error("upkeep asked for permission by itself")
	}
	if row := f.row(t, "mod"); !strings.Contains(row.Hint, "administrator permission") {
		t.Errorf("hint = %q", row.Hint)
	}
}

func TestTheModStepWaitsForADumpFromThisAccount(t *testing.T) {
	f := newFixture(t, true) // account 42, game 2.4.0.1
	dump := filepath.Join(f.dir, "mod", "garage.json")
	put := func(account int, game string) {
		write(t, dump, `{"schema":1,"mod_version":"0.3.0","captured_at":"2026-09-27T10:00:00Z","game_version":"`+game+`","account_id":`+
			itoa(account)+`,"resources":{},"premium":{},"vehicles":[]}`)
	}
	if w := f.wizard(t); w.Mod.DumpArrived {
		t.Fatal("arrived with no dump")
	}
	put(7, "2.4.0.1")
	if w := f.wizard(t); w.Mod.DumpArrived || !w.Mod.OtherAccount {
		t.Errorf("another account's dump: %+v", w.Mod)
	}
	put(42, "2.3.0.0")
	if w := f.wizard(t); w.Mod.DumpArrived {
		t.Error("a dump from an older game counted")
	}
	put(42, "2.4.0.1")
	if w := f.wizard(t); !w.Mod.DumpArrived || !step(w, "mod").Done {
		t.Errorf("mod = %+v", w.Mod)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestAdviceAnswersGoIntoTheOverlay(t *testing.T) {
	f := newFixture(t, true)
	path := filepath.Join(f.dir, "wot-overlay.yaml")

	answers := `{"profile.session_minutes":"90","profile.battles_per_hour":6,"preferences.avoid_classes":["SPG"],"advice.answer.format":"plain"}`
	if r := f.do(t, "advice-save", answers); !r.OK {
		t.Fatal(r.Message)
	}
	o, findings, err := overlay.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range findings {
		if fd.Severity == overlay.SeverityError {
			t.Errorf("finding: %s", fd)
		}
	}
	if o.Profile.SessionMinutes != 90 || o.Profile.BattlesPerHour != 6 || o.Advice.Answer.Format != "plain" ||
		len(o.Preferences.AvoidClasses) != 1 || o.Profile.Experience != "" {
		t.Errorf("overlay = %+v %+v %+v", o.Profile, o.Advice.Answer, o.Preferences.AvoidClasses)
	}
	if !step(f.wizard(t), "advice").Done {
		t.Error("the advice step is not done")
	}

	// The player's own comments survive; unticking every class removes the key.
	raw, _ := os.ReadFile(path)
	os.WriteFile(path, append([]byte("# mine, keep me\n"), raw...), 0o644)
	if r := f.do(t, "advice-save", `{"preferences.avoid_classes":[]}`); !r.OK {
		t.Fatal(r.Message)
	}
	raw, _ = os.ReadFile(path)
	if !strings.Contains(string(raw), "# mine, keep me") || strings.Contains(string(raw), "avoid_classes") {
		t.Errorf("overlay:\n%s", raw)
	}
}

func TestBadAdviceIsRefusedAndABrokenOverlayIsLeftAlone(t *testing.T) {
	f := newFixture(t, true)
	path := filepath.Join(f.dir, "wot-overlay.yaml")
	for _, bad := range []string{
		`{"profile.session_minutes":"45"}`,
		`{"profile.battles_per_hour":99}`,
		`{"preferences.avoid_classes":["tanks"]}`,
		`not json`,
	} {
		if r := f.do(t, "advice-save", bad); r.OK {
			t.Errorf("%s was accepted", bad)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a refused answer wrote the overlay")
	}

	broken := "version: 1\nupdated_at: 2026-09-01\nmystery_key: 1\n"
	write(t, path, broken)
	if r := f.do(t, "advice-save", `{"advice.answer.length":"short"}`); r.OK || !strings.Contains(r.Message, "left as it is") {
		t.Errorf("result = %+v", r)
	}
	if raw, _ := os.ReadFile(path); string(raw) != broken {
		t.Errorf("the broken overlay was rewritten:\n%s", raw)
	}
}

func TestThePluginInstallsThroughClaudeCode(t *testing.T) {
	f := newFixture(t, true)
	if r := f.do(t, "claude-code", ""); r.OK {
		t.Error("installed without Claude Code")
	}
	f.code.available = true
	if w := f.wizard(t); !w.Claude.CodeCLI {
		t.Error("Claude Code not detected")
	}
	if r := f.do(t, "claude-code", ""); !r.OK || !f.code.installed {
		t.Errorf("result = %+v", r)
	}
}

func TestFinishingTheSetup(t *testing.T) {
	f := newFixture(t, true)
	f.do(t, "agree", "")
	if !f.svc.NeedsSetup() {
		t.Fatal("setup is done before it was finished")
	}
	if r := f.do(t, "setup-finish", ""); !r.OK {
		t.Fatal(r.Message)
	}
	if f.svc.NeedsSetup() || !f.wizard(t).Completed {
		t.Error("setup still needed after finishing")
	}
}

func TestTheFirstSyncCountsOnlyWithAccountData(t *testing.T) {
	f := newFixture(t, true)
	env := f.svc.opts.NewEnv(nil, nil)
	db, err := env.OpenStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// A sync whose Wargaming sources all failed still leaves XVM's table.
	db.PutSnapshot(context.Background(), store.Snapshot{Source: "xvm", Endpoint: "wn8exp", RequestedAt: time.Now(), HTTPStatus: 200, Raw: []byte("{}")})
	db.PutSnapshot(context.Background(), store.Snapshot{Source: "wg", Endpoint: "account/info", RequestedAt: time.Now(), HTTPStatus: 200, WGError: "INVALID_APPLICATION_ID"})
	if step(f.wizard(t), "sync").Done {
		t.Error("done without account data")
	}
	db.PutSnapshot(context.Background(), store.Snapshot{Source: "wg", Endpoint: "account/info", RequestedAt: time.Now(), HTTPStatus: 200, Raw: []byte("{}")})
	db.Close()
	if !step(f.wizard(t), "sync").Done {
		t.Error("not done with account data")
	}
}

func TestANewOverlayStartsWithItsVersion(t *testing.T) {
	f := newFixture(t, true)
	if r := f.do(t, "advice-save", `{"advice.answer.length":"short"}`); !r.OK {
		t.Fatal(r.Message)
	}
	raw, _ := os.ReadFile(filepath.Join(f.dir, "wot-overlay.yaml"))
	if !strings.HasPrefix(string(raw), "version: 1\nupdated_at: ") {
		t.Errorf("overlay:\n%s", raw)
	}
}
