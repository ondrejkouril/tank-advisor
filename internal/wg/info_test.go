package wg

import (
	"testing"
	"time"
)

func TestParseGameInfo(t *testing.T) {
	// The live response of 2026-10-09, with the fields asked for.
	info, err := parseGameInfo([]byte(`{"game_version":"2.4","tanks_updated_at":1790827403}`))
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "2.4" || !info.TanksUpdatedAt.Equal(time.Date(2026, 10, 1, 4, 3, 23, 0, time.UTC)) {
		t.Errorf("info = %+v", info)
	}
	if _, err := parseGameInfo([]byte(`{"tanks_updated_at":1}`)); err == nil {
		t.Error("a response without a game version was accepted")
	}
}
