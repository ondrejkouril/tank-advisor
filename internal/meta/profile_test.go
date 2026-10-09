package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// wz120 serves Wargaming's answers for the WZ-120, recorded 2026-10-09, to a
// real client: stock and top profile, every valid combination, the modules.
func wz120(t *testing.T) *wg.Client {
	t.Helper()
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "wg", name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "encyclopedia/vehicleprofiles"):
			w.Write(read("wz120-vehicleprofiles.json"))
		case strings.Contains(r.URL.Path, "encyclopedia/vehicleprofile"):
			if r.URL.Query().Get("profile_id") == "567-2866-2867-513589-514356" {
				w.Write(read("wz120-vehicleprofile-top.json"))
			} else {
				w.Write(read("wz120-vehicleprofile-stock.json"))
			}
		case strings.Contains(r.URL.Path, "encyclopedia/vehicles"):
			w.Write(read("wz120-modules-tree.json"))
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := wg.New("eu", "app", wg.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConfigurationsWZ120(t *testing.T) {
	cfg, err := Configurations(context.Background(), wz120(t), 1841)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Stock.Gun.Name != "100 mm 59-100T (120)" || cfg.Top == nil || cfg.Top.Gun.Name != "122 mm 60-122T (120)" {
		t.Fatalf("stock %q, top %+v", cfg.Stock.Gun.Name, cfg.Top)
	}
	// Model 3 chassis, WZ-120-1 turret, 122 mm gun, 12150LT engine.
	if cfg.XPToTop != 22400+23500+59000+46000 || len(cfg.Research) != 4 {
		t.Errorf("research = %+v, %d XP", cfg.Research, cfg.XPToTop)
	}
	var order []string
	for _, m := range cfg.Research {
		order = append(order, m.Type)
	}
	if got := strings.Join(order, " "); got != "vehicleChassis vehicleTurret vehicleGun vehicleEngine" {
		t.Errorf("order = %s", got)
	}
	if DPM(*cfg.Top) != 2398 || PowerToWeight(*cfg.Top) != 17.5 {
		t.Errorf("top dpm %d, hp/t %v", DPM(*cfg.Top), PowerToWeight(*cfg.Top))
	}
}

// fixedConfig is a vehicle with nothing to research.
type fixedConfig struct{}

func (fixedConfig) ModulesTree(context.Context, int) (map[int]wg.Module, error) {
	return map[int]wg.Module{1: {ModuleID: 1, Type: "vehicleGun", IsDefault: true}, 2: {ModuleID: 2, Type: "vehicleEngine", IsDefault: true}}, nil
}
func (fixedConfig) VehicleProfiles(context.Context, int) ([]wg.ProfileRef, error) {
	return []wg.ProfileRef{{ProfileID: "1-2", IsDefault: true}}, nil
}
func (fixedConfig) VehicleProfile(_ context.Context, _ int, id string) (wg.Profile, error) {
	if id != "" {
		return wg.Profile{}, errUnexpected
	}
	return wg.Profile{ProfileID: "1-2", IsDefault: true}, nil
}

var errUnexpected = os.ErrInvalid

func TestConfigurationsNothingToResearch(t *testing.T) {
	cfg, err := Configurations(context.Background(), fixedConfig{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Top != nil || cfg.XPToTop != 0 || len(cfg.Research) != 0 {
		t.Errorf("cfg = %+v", cfg)
	}
}
