package wg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
)

// Mode names for per-tank statistics. "all" is every battle type combined and
// is always present; "random" is requested explicitly.
const (
	ModeAll    = "all"
	ModeRandom = "random"
)

// TankStats is one tank's cumulative statistics in one mode.
//
// Every figure here is cumulative since the account was created. The API offers
// no windowed view, which is why snapshots are retained: recent form is a
// difference between two of these.
type TankStats struct {
	TankID int
	Mode   string
	ModeStats

	// MarkOfMastery comes from account/tanks rather than tanks/stats, and is
	// filled in by the caller. 0 none, 1 third class, 2 second, 3 first, 4 Ace.
	MarkOfMastery int
}

// TankStatsResult pairs parsed per-tank statistics with the raw response.
type TankStatsResult struct {
	Stats  []TankStats
	Result Result
}

// TankStats fetches tanks/stats for every vehicle on the account.
//
// No tank_id filter is sent, so this is a single request covering the whole
// account, including vehicles that have since been sold - those battles were
// still played, and a question about a line the player has moved on from
// depends on them.
func (c *Client) TankStats(ctx context.Context, accountID int, accessToken string) (TankStatsResult, error) {
	params := url.Values{}
	params.Set("account_id", strconv.Itoa(accountID))
	params.Set("extra", ModeRandom)
	if accessToken != "" {
		params.Set("access_token", accessToken)
	}

	result, err := c.Get(ctx, "tanks/stats", params, "")
	out := TankStatsResult{Result: result}
	if err != nil {
		return out, err
	}

	stats, err := parseTankStats(result.Data, accountID)
	if err != nil {
		return out, err
	}
	out.Stats = stats
	return out, nil
}

func parseTankStats(data json.RawMessage, accountID int) ([]TankStats, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var byAccount map[string][]*struct {
		TankID int       `json:"tank_id"`
		All    *rawStats `json:"all"`
		Random *rawStats `json:"random"`
	}
	if err := json.Unmarshal(data, &byAccount); err != nil {
		return nil, fmt.Errorf("tanks/stats: parsing data: %w", err)
	}

	entries, ok := byAccount[strconv.Itoa(accountID)]
	if !ok {
		return nil, fmt.Errorf("tanks/stats: no data for account %d", accountID)
	}

	var stats []TankStats
	for _, e := range entries {
		if e == nil || e.TankID == 0 {
			continue
		}
		// A mode with no battles is omitted rather than stored as zeroes, so
		// that "no data" and "played and scored nothing" stay distinguishable.
		if e.All != nil && e.All.Battles > 0 {
			stats = append(stats, TankStats{TankID: e.TankID, Mode: ModeAll, ModeStats: e.All.toModeStats()})
		}
		if e.Random != nil && e.Random.Battles > 0 {
			stats = append(stats, TankStats{TankID: e.TankID, Mode: ModeRandom, ModeStats: e.Random.toModeStats()})
		}
	}

	sort.Slice(stats, func(i, j int) bool {
		if stats[i].TankID != stats[j].TankID {
			return stats[i].TankID < stats[j].TankID
		}
		return stats[i].Mode < stats[j].Mode
	})
	return stats, nil
}

// TankMastery is one tank's mastery badge and headline record.
type TankMastery struct {
	TankID        int
	MarkOfMastery int
	Battles       int
	Wins          int
}

// TankMasteryResult pairs mastery badges with the raw response.
type TankMasteryResult struct {
	Mastery []TankMastery
	Result  Result
}

// AccountTanks fetches account/tanks, the only source of mastery badges.
func (c *Client) AccountTanks(ctx context.Context, accountID int, accessToken string) (TankMasteryResult, error) {
	params := url.Values{}
	params.Set("account_id", strconv.Itoa(accountID))
	if accessToken != "" {
		params.Set("access_token", accessToken)
	}

	result, err := c.Get(ctx, "account/tanks", params, "")
	out := TankMasteryResult{Result: result}
	if err != nil {
		return out, err
	}

	var byAccount map[string][]*struct {
		TankID        int `json:"tank_id"`
		MarkOfMastery int `json:"mark_of_mastery"`
		Statistics    *struct {
			Battles int `json:"battles"`
			Wins    int `json:"wins"`
		} `json:"statistics"`
	}
	if err := json.Unmarshal(result.Data, &byAccount); err != nil {
		return out, fmt.Errorf("account/tanks: parsing data: %w", err)
	}

	entries, ok := byAccount[strconv.Itoa(accountID)]
	if !ok {
		return out, fmt.Errorf("account/tanks: no data for account %d", accountID)
	}

	for _, e := range entries {
		if e == nil || e.TankID == 0 {
			continue
		}
		m := TankMastery{TankID: e.TankID, MarkOfMastery: e.MarkOfMastery}
		if e.Statistics != nil {
			m.Battles = e.Statistics.Battles
			m.Wins = e.Statistics.Wins
		}
		out.Mastery = append(out.Mastery, m)
	}

	sort.Slice(out.Mastery, func(i, j int) bool { return out.Mastery[i].TankID < out.Mastery[j].TankID })
	return out, nil
}

// MasteryLabel renders a badge the way the game does.
func MasteryLabel(mark int) string {
	switch mark {
	case 1:
		return "3rd Class"
	case 2:
		return "2nd Class"
	case 3:
		return "1st Class"
	case 4:
		return "Ace Tanker"
	default:
		return "none"
	}
}

// ParseTankStatsBody parses a complete tanks/stats response body, as stored in
// a snapshot. It exists so that fields a newer parser understands can be
// recovered from responses already on disk, without another request.
func ParseTankStatsBody(raw []byte, accountID int) ([]TankStats, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("tanks/stats: parsing body: %w", err)
	}
	if env.Status != "ok" {
		return nil, fmt.Errorf("tanks/stats: body has status %q", env.Status)
	}
	return parseTankStats(env.Data, accountID)
}
