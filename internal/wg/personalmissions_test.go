package wg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestParsePersonalMissions uses an excerpt of the live response (2026-09-18):
// campaign 1, operation 4 (Object 260), the first three light-tank missions
// and the first medium-tank one.
func TestParsePersonalMissions(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "wg", "encyclopedia-personalmissions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	missions, err := parsePersonalMissions(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	if len(missions) != 4 {
		t.Fatalf("missions = %d, want 4", len(missions))
	}
	m := missions[0]
	if m.MissionID != 226 || m.Name != "LT-1: For Victory!" || m.Class != "lightTank" ||
		m.CampaignID != 1 || m.Campaign != "Long-Awaited Backup" || m.OperationID != 4 ||
		m.Operation != "Object 260" || m.MinTier != 6 || m.MaxTier != 10 || m.SetID != 1 {
		t.Errorf("mission 226 = %+v", m)
	}
	want := "Destroy all enemy vehicles / capture or defend the base; Finish the battle as the top player on your team by experience earned"
	if m.Primary != want {
		t.Errorf("primary = %q, want %q", m.Primary, want)
	}
	if m.Secondary != "Survive the battle" {
		t.Errorf("secondary = %q", m.Secondary)
	}
	if missions[3].MissionID != 241 || missions[3].Class != "heavyTank" || missions[3].SetID != 2 {
		t.Errorf("mission 241 = %+v, want HT-1, the first of set 2", missions[3])
	}
}

func TestAccountInfoParsesPersonalMissions(t *testing.T) {
	data := json.RawMessage(`{"512345678":{"account_id":512345678,"private":{"personal_missions":{"1":"ALL_REWARDS_GOTTEN","226":"MAIN_REWARD_GOTTEN","x":"BAD","5":""}}}}`)
	account, err := parseAccountInfo(data, 512345678)
	if err != nil {
		t.Fatal(err)
	}
	if len(account.PersonalMissions) != 2 || account.PersonalMissions[226] != "MAIN_REWARD_GOTTEN" {
		t.Errorf("personal missions = %v, want two valid entries", account.PersonalMissions)
	}
}
