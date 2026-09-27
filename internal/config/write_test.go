package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetKeepsCommentsAndOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	orig := "# wotctx config.\n\n# The overlay is versioned in the repo.\noverlay_path: C:/overlay.yaml # mine\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Set(path,
		Update{Key: "account.realm", Value: "eu"},
		Update{Key: "account.account_id", Value: 123},
		Update{Key: "account.nickname", Value: "someone"},
	)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	raw, _ := os.ReadFile(path)
	got := string(raw)
	for _, want := range []string{"# wotctx config.", "# The overlay is versioned in the repo.", "overlay_path: C:/overlay.yaml # mine", "account_id: 123", "nickname: someone"} {
		if !strings.Contains(got, want) {
			t.Errorf("result lacks %q:\n%s", want, got)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Account.AccountID != 123 || cfg.OverlayPath != "C:/overlay.yaml" {
		t.Errorf("loaded %+v", cfg)
	}

	// Removing keys leaves the rest alone.
	if err := Set(path, Update{Key: "account.account_id"}, Update{Key: "account.nickname"}); err != nil {
		t.Fatalf("Set remove: %v", err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Configured() || cfg.Account.Realm != "eu" || cfg.OverlayPath == "" {
		t.Errorf("after removal: %+v", cfg)
	}
}

func TestSetCreatesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	if err := Set(path, Update{Key: "game_dir", Value: `D:\WoT`}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.GameDir != `D:\WoT` {
		t.Fatalf("Load = %+v, %v", cfg, err)
	}
}

func TestSetRefusesAResultThatDoesNotLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("overlay_path: x\n"), 0o644)
	if err := Set(path, Update{Key: "account.realm", Value: "moon"}); err == nil {
		t.Fatal("Set accepted an invalid realm")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "overlay_path: x\n" {
		t.Errorf("file changed despite the refusal: %q", raw)
	}
}

func TestParseDurationTakesDays(t *testing.T) {
	d, err := ParseDuration("90d")
	if err != nil || d != 90*24*time.Hour {
		t.Errorf("90d = %v, %v", d, err)
	}
	if _, err := ParseDuration("xd"); err == nil {
		t.Error("xd parsed")
	}
}

func TestDefaultHasNoAccount(t *testing.T) {
	if Default().Configured() {
		t.Error("the defaults name an account; a fresh install must not look set up")
	}
	if !Default().MoEFetchEnabled() {
		t.Error("the MoE fetch is off by default")
	}
}
