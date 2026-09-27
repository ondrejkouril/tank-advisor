package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default() is invalid: %v", err)
	}
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load on a missing file = %v, want nil", err)
	}
	if cfg.Account.AccountID != Default().Account.AccountID {
		t.Errorf("account_id = %d, want the default %d", cfg.Account.AccountID, Default().Account.AccountID)
	}
}

// TestLoadMergesOverDefaults is the reason Load starts from Default(): a config
// file should be able to change one value without restating the rest.
func TestLoadMergesOverDefaults(t *testing.T) {
	path := write(t, `
account:
  realm: com
sync:
  auto_sync_after: 45m
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Account.Realm != "com" {
		t.Errorf("realm = %q, want %q", cfg.Account.Realm, "com")
	}
	if cfg.Sync.AutoSyncAfter.Duration != 45*time.Minute {
		t.Errorf("auto_sync_after = %s, want 45m", cfg.Sync.AutoSyncAfter)
	}
	// Untouched values keep their defaults.
	if cfg.Account.Nickname != Default().Account.Nickname {
		t.Errorf("nickname = %q, want the default %q", cfg.Account.Nickname, Default().Account.Nickname)
	}
	if cfg.Confidence != Default().Confidence {
		t.Errorf("confidence = %+v, want the defaults %+v", cfg.Confidence, Default().Confidence)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := write(t, "accont:\n  realm: eu\n") // typo

	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unknown key, want an error")
	} else if !strings.Contains(err.Error(), "accont") {
		t.Errorf("error %q does not name the offending key", err)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		"bad realm":              "account:\n  realm: xx\n",
		"negative account id":    "account:\n  account_id: -5\n",
		"short retention":        "sync:\n  retention: 2d\n",
		"negative ttl":           "sync:\n  ttl:\n    wg:account/info: -1h\n",
		"unordered thresholds":   "confidence:\n  very_low_below: 500\n  low_below: 100\n  moderate_below: 300\n",
		"unparseable duration":   "sync:\n  auto_sync_after: soon\n",
		"duration as a bare int": "sync:\n  auto_sync_after: 6\n",
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body)); err == nil {
				t.Errorf("Load accepted %s, want an error", name)
			}
		})
	}
}

// TestLoadFailureReturnsDefaultsNotPartialState guards against a half-applied
// config being handed to callers after a validation failure.
func TestLoadFailureReturnsDefaultsNotPartialState(t *testing.T) {
	path := write(t, "account:\n  realm: xx\n  nickname: someone-else\n")

	cfg, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted an invalid realm")
	}
	if cfg.Account.Nickname != Default().Account.Nickname {
		t.Errorf("nickname = %q, want the default after a failed load", cfg.Account.Nickname)
	}
}

func TestClassifyUsesConfiguredThresholds(t *testing.T) {
	c := Default().Confidence

	cases := []struct {
		battles int
		want    string
	}{
		{0, ConfidenceVeryLow},
		{29, ConfidenceVeryLow},
		{30, ConfidenceLow},
		{99, ConfidenceLow},
		{100, ConfidenceModerate},
		{299, ConfidenceModerate},
		{300, ConfidenceOK},
		{5000, ConfidenceOK},
	}
	for _, tc := range cases {
		if got := c.Classify(tc.battles); got != tc.want {
			t.Errorf("Classify(%d) = %q, want %q", tc.battles, got, tc.want)
		}
	}
}

func TestTTLForFallsBackForUnknownSource(t *testing.T) {
	cfg := Default()

	if got := cfg.TTLFor("wg:encyclopedia/vehicles"); got != 7*24*time.Hour {
		t.Errorf("TTLFor(encyclopedia) = %s, want 168h", got)
	}
	if got := cfg.TTLFor("tomato:something-new"); got != time.Hour {
		t.Errorf("TTLFor(unknown) = %s, want the 1h fallback", got)
	}
}

func TestOverlayFileDefaultsIntoConfigDir(t *testing.T) {
	paths := Paths{ConfigDir: filepath.Join("C:", "cfg", "wotctx")}

	cfg := Default()
	want := filepath.Join(paths.ConfigDir, "wot-overlay.yaml")
	if got := cfg.OverlayFile(paths); got != want {
		t.Errorf("OverlayFile = %q, want %q", got, want)
	}

	cfg.OverlayPath = filepath.Join("D:", "repo", "wot-overlay.yaml")
	if got := cfg.OverlayFile(paths); got != cfg.OverlayPath {
		t.Errorf("OverlayFile = %q, want the configured %q", got, cfg.OverlayPath)
	}
}

func TestResolveSeparatesConfigFromData(t *testing.T) {
	paths, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for name, got := range map[string]string{
		"ConfigDir":  paths.ConfigDir,
		"ConfigFile": paths.ConfigFile,
		"DataDir":    paths.DataDir,
		"DBFile":     paths.DBFile,
	} {
		if got == "" {
			t.Errorf("%s is empty", name)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("%s = %q, want an absolute path", name, got)
		}
	}
	if filepath.Dir(paths.ConfigFile) != paths.ConfigDir {
		t.Errorf("ConfigFile %q is not inside ConfigDir %q", paths.ConfigFile, paths.ConfigDir)
	}
	if filepath.Dir(paths.DBFile) != paths.DataDir {
		t.Errorf("DBFile %q is not inside DataDir %q", paths.DBFile, paths.DataDir)
	}
}

func TestEnsureDataDirIsIdempotent(t *testing.T) {
	paths := Paths{DataDir: filepath.Join(t.TempDir(), "nested", "wotctx")}

	for i := range 2 {
		if err := paths.EnsureDataDir(); err != nil {
			t.Fatalf("EnsureDataDir call %d: %v", i+1, err)
		}
	}
	if info, err := os.Stat(paths.DataDir); err != nil || !info.IsDir() {
		t.Fatalf("data dir was not created: %v", err)
	}
}

func TestEnsureDataDirRejectsEmptyPath(t *testing.T) {
	if err := (Paths{}).EnsureDataDir(); err == nil {
		t.Error("EnsureDataDir on unresolved paths = nil, want an error")
	}
}

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

func TestResolveHonoursTheDirectoryOverrides(t *testing.T) {
	cfg, data := t.TempDir(), t.TempDir()
	t.Setenv("WOTCTX_CONFIG_DIR", cfg)
	t.Setenv("WOTCTX_DATA_DIR", data)
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigFile != filepath.Join(cfg, "config.yaml") || p.DBFile != filepath.Join(data, "wotctx.db") {
		t.Errorf("Resolve = %+v", p)
	}
}

func TestLoadReadsTheAppsRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("consent:\n  notice: 1\n  agreed: 2026-09-27\nsetup:\n  completed: 2026-09-27\nmod:\n  managed: true\n  placed: ['C:/Games/WoT/mods/2.4.0.1']\n"), 0o644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Consent.Notice != 1 || cfg.Setup.Completed != "2026-09-27" || !cfg.Mod.Managed || len(cfg.Mod.Placed) != 1 {
		t.Errorf("cfg = %+v %+v %+v", cfg.Consent, cfg.Setup, cfg.Mod)
	}

	os.WriteFile(path, []byte("consent:\n  agreed: yesterday\n"), 0o644)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "consent.agreed") {
		t.Errorf("a bad date loaded: %v", err)
	}
}
