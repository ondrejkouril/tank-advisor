package mod

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixture is a real dump from the 2.4.0.1 client (2026-09-25 20:38 UTC), cut down to
// five vehicles: the three checked against the client by hand, one event
// rental and one Steel Hunter vehicle.
func fixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "mod", "garage.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return raw
}

func TestParseTheClientsDump(t *testing.T) {
	d, err := Parse(fixture(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if want := time.Date(2026, 9, 25, 20, 38, 53, 0, time.UTC); !d.CapturedAt.Equal(want) {
		t.Errorf("CapturedAt = %s, want %s", d.CapturedAt, want)
	}
	if d.GameVersion != "2.4.0.1" || d.AccountID != 512345678 {
		t.Errorf("header = %q / %d", d.GameVersion, d.AccountID)
	}
	if *d.Resources.FreeXP != 253317 || *d.Premium.WotPlus != true {
		t.Errorf("resources/premium = %+v / %+v", d.Resources, d.Premium)
	}
	if len(d.Vehicles) != 5 {
		t.Fatalf("%d vehicles, want 5", len(d.Vehicles))
	}

	// The figures the owner checked against the client.
	byID := map[int]Vehicle{}
	for _, v := range d.Vehicles {
		byID[v.TankID] = v
	}
	if selma := byID[6257]; *selma.XP != 90237 {
		t.Errorf("Šelma XP = %d, want 90237", *selma.XP)
	}
	if o := byID[16897]; o.Marks.Marks != 1 || o.Marks.Percent != 75.15 {
		t.Errorf("Object 140 marks = %+v, want 1 at 75.15 %%", o.Marks)
	}
	tvp := byID[2417]
	if len(tvp.Shells) != 3 || tvp.Shells[0].Kind != "ARMOR_PIERCING_CR" || *tvp.Shells[0].Count != 28 {
		t.Errorf("TVP shells = %+v", tvp.Shells)
	}
	// Unmanned in the first dump (20:34); the owner put a crew in at 20:37,
	// and the mod rewrote the file within seconds.
	var roles []string
	for _, seat := range tvp.Crew {
		if seat.Role == nil || len(seat.Skills) != 4 {
			t.Fatalf("TVP seat %d = %+v, want a crewman with four skills", seat.Slot, seat)
		}
		roles = append(roles, *seat.Role)
	}
	if got := strings.Join(roles, " "); got != "commander gunner driver loader" {
		t.Errorf("TVP crew = %s", got)
	}
	if byID[34849].Owned() {
		t.Error("the TS-54 rental counts as owned")
	}
	if !byID[6257].Owned() {
		t.Error("the Šelma does not count as owned")
	}
}

func TestParseRefusesWhatItCannotRead(t *testing.T) {
	cases := map[string]string{
		"schema":     `{"schema": 2, "captured_at": "2026-09-25T20:34:01Z", "account_id": 1}`,
		"no time":    `{"schema": 1, "account_id": 1}`,
		"no account": `{"schema": 1, "captured_at": "2026-09-25T20:34:01Z"}`,
		"not json":   `{"schema": 1,`,
	}
	for name, raw := range cases {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("%s: Parse = nil error", name)
		}
	}
	_, err := Parse([]byte(cases["schema"]))
	if !strings.Contains(err.Error(), "schema 2") {
		t.Errorf("schema error %q does not name the version", err)
	}
}

// A field the mod could not read is null, and must stay distinguishable from
// zero.
func TestNullStaysMissing(t *testing.T) {
	d, err := Parse([]byte(`{"schema": 1, "captured_at": "2026-09-25T20:34:01Z", "account_id": 1,
		"resources": {"credits": null, "gold": 0},
		"vehicles": [{"tank_id": 1, "xp": null, "crew": null, "equipment": [null, {"id": 5, "name": "x"}]}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if d.Resources.Credits != nil || d.Resources.Gold == nil {
		t.Errorf("credits/gold = %v/%v", d.Resources.Credits, d.Resources.Gold)
	}
	v := d.Vehicles[0]
	if v.XP != nil || v.Crew != nil {
		t.Error("a null XP or crew was read as a value")
	}
	if v.Equipment[0] != nil || v.Equipment[1].ID != 5 {
		t.Errorf("equipment = %+v", v.Equipment)
	}
}
