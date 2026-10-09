package wg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// GameInfo is what encyclopedia/info says about the game itself, measured
// 2026-10-09 as game_version "2.4" and tanks_updated_at 2026-10-01.
type GameInfo struct {
	// Version is the server's game version, as Wargaming writes it ("2.4").
	Version string
	// TanksUpdatedAt is when the vehicle encyclopedia last changed.
	TanksUpdatedAt time.Time
}

// GameInfoResult pairs the parsed info with the raw response.
type GameInfoResult struct {
	Info   GameInfo
	Result Result
}

// GameInfo fetches encyclopedia/info, asking only for the two fields used:
// the rest is lists of languages, nations and achievement sections.
func (c *Client) GameInfo(ctx context.Context) (GameInfoResult, error) {
	result, err := c.Get(ctx, "encyclopedia/info", url.Values{"fields": {"game_version,tanks_updated_at"}}, "")
	out := GameInfoResult{Result: result}
	if err != nil {
		return out, err
	}
	info, err := parseGameInfo(result.Data)
	if err != nil {
		return out, err
	}
	out.Info = info
	return out, nil
}

func parseGameInfo(data json.RawMessage) (GameInfo, error) {
	var raw struct {
		GameVersion    string `json:"game_version"`
		TanksUpdatedAt int64  `json:"tanks_updated_at"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return GameInfo{}, fmt.Errorf("encyclopedia/info: %w", err)
	}
	if raw.GameVersion == "" {
		return GameInfo{}, fmt.Errorf("encyclopedia/info: no game_version in the response")
	}
	info := GameInfo{Version: raw.GameVersion}
	if raw.TanksUpdatedAt > 0 {
		info.TanksUpdatedAt = time.Unix(raw.TanksUpdatedAt, 0).UTC()
	}
	return info, nil
}
