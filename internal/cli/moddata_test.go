package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModDump puts the real, cut-down dump where the mod writes it, with its
// capture time and game version replaced.
func writeModDump(t *testing.T, env *Env, capturedAt, gameVersion string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "mod", "garage.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), `"2026-09-25T20:38:53Z"`, `"`+capturedAt+`"`, 1)
	text = strings.Replace(text, `"game_version": "2.4.0.1"`, `"game_version": "`+gameVersion+`"`, 1)
	path := env.modDumpPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func modDataCheck(t *testing.T, env *Env) check {
	t.Helper()
	for _, c := range collectChecks(context.Background(), env) {
		if c.Name == "mod-data" {
			return c
		}
	}
	t.Fatal("no mod-data check")
	return check{}
}

// installedGame is fakeGame with this mod's package in mods/2.4.0.1/.
func installedGame(t *testing.T) string {
	dir := fakeGame(t)
	os.WriteFile(filepath.Join(dir, "mods", "2.4.0.1", "ondrejkouril.wotctx_0.2.1.wotmod"), nil, 0o644)
	return dir
}

// The seed account's clock is 2026-09-18 12:00 UTC and its last battle 09:00.
func TestModDataStates(t *testing.T) {
	cases := []struct {
		name       string
		game       func(*testing.T) string
		dumpAt     string // "" for no dump
		dumpGame   string
		sync       bool
		wantStatus checkStatus
		wantDetail string
		wantFix    string
	}{
		{"not installed", fakeGame, "", "", false, statusUnknown, "not installed", "mod-install"},
		{"installed, game not started", installedGame, "", "", false, statusWarn, "no dump yet", "start the game"},
		{"dump not synced", installedGame, "2026-09-18T11:00:00Z", "2.4.0.1", false, statusWarn, "not synced yet", "wotctx sync"},
		{"current", installedGame, "2026-09-18T11:00:00Z", "2.4.0.1", true, statusOK, "(1h ago) by mod", ""},
		{"played since", installedGame, "2026-09-18T08:00:00Z", "2.4.0.1", true, statusWarn, "battles were played after it", "start the game"},
		{"older game", installedGame, "2026-09-18T11:00:00Z", "2.3.0.2", true, statusWarn, "predates the installed game 2.4.0.1", ""},
		{"removed by an update", fakeGame, "2026-09-18T11:00:00Z", "2.4.0.1", true, statusWarn, "not in the game's mods/2.4.0.1", "mod-install"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, _ := seededEnv(t)
			env.Config.GameDir = c.game(t)
			if c.dumpAt != "" {
				writeModDump(t, env, c.dumpAt, c.dumpGame)
			}
			if c.sync {
				if err := Run(context.Background(), env, []string{"sync", "--only", "mod"}); err != nil {
					t.Fatalf("sync --only mod: %v", err)
				}
			}
			got := modDataCheck(t, env)
			if got.Status != c.wantStatus || !strings.Contains(got.Detail, c.wantDetail) || !strings.Contains(got.Fix, c.wantFix) {
				t.Errorf("mod-data = %+v\nwant status %s, detail containing %q, fix containing %q",
					got, c.wantStatus, c.wantDetail, c.wantFix)
			}
		})
	}
}

// The dump ages whenever the game is closed, and no sync can refresh it, so it
// must not make data-age ask for one.
func TestDataAgeIgnoresTheModDump(t *testing.T) {
	env, _ := seededEnv(t)
	env.Config.GameDir = installedGame(t)
	writeModDump(t, env, "2026-09-10T11:00:00Z", "2.4.0.1") // eight days old
	if err := Run(context.Background(), env, []string{"sync", "--only", "mod"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range collectChecks(context.Background(), env) {
		if c.Name == "data-age" && strings.Contains(c.Detail, "mod:") {
			t.Errorf("data-age counts the mod dump: %s", c.Detail)
		}
	}
}
