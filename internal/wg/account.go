package wg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// accountExtras are the extra blocks requested from account/info.
//
// Only random-battle statistics are requested: that is the mode every
// comparison, WN8 figure and tier list is based on. Adding a mode here is
// cheap, but each one becomes another set of rows and rollups to interpret.
var accountExtras = []string{
	"private.garage",
	"private.boosters",
	// Personal-mission statuses ride on the same request at no extra cost.
	"private.personal_missions",
	"statistics.random",
}

// accountFields limits account/info to what wotctx reads (docs/spec-desktop.md
// section 12.3). Measured in docs/plan.md step D1, the unfiltered response is
// 24.6 KB, half of it statistics for modes nothing uses, with personal fields
// (ban_info, restrictions, is_bound_to_phone) no answer needs. With fields
// set, each extra block has to be named too, or it is left out.
var accountFields = []string{
	"account_id",
	"nickname",
	"last_battle_time",
	"global_rating",
	"private.credits",
	"private.gold",
	"private.bonds",
	"private.free_xp",
	"private.is_premium",
	"private.premium_expires_at",
	"private.battle_life_time",
	"private.garage",
	"private.boosters",
	"private.personal_missions",
	"statistics.random",
	"statistics.all",
}

// Account is the account/info response for one player.
//
// Everything under Private requires an access token, and none of it is
// obtainable any other way - the game client is the only other place these
// numbers exist.
type Account struct {
	AccountID      int
	Nickname       string
	ClanID         int
	CreatedAt      time.Time
	LastBattleTime time.Time
	LogoutAt       time.Time
	GlobalRating   int

	// HasPrivate reports whether the private block came back. A request with no
	// token or an expired one still succeeds, just without it, so this is how a
	// caller tells "no premium" from "did not ask".
	HasPrivate       bool
	Credits          int
	Gold             int
	Bonds            int
	FreeXP           int
	IsPremium        bool
	PremiumExpiresAt time.Time
	BattleLifeTime   time.Duration
	Garage           []int
	Boosters         []Booster
	// PersonalMissions maps a mission id to its status (e.g. MAIN_REWARD_GOTTEN,
	// ALL_REWARDS_GOTTEN). A mission with no entry has not been completed; the
	// API does not say whether it was started.
	PersonalMissions map[int]string

	// RandomStats is the account-wide random-battle aggregate.
	RandomStats ModeStats
}

// Booster is one Personal Reserve.
type Booster struct {
	Kind      string
	Count     int
	State     string // ACTIVE | INACTIVE | USED
	ExpiresAt time.Time
}

// ModeStats is the account-level aggregate for one game mode.
type ModeStats struct {
	Battles                int
	Wins                   int
	Losses                 int
	Draws                  int
	SurvivedBattles        int
	DamageDealt            int
	DamageReceived         int
	Frags                  int
	Spotted                int
	XP                     int
	BattleAvgXP            int
	HitsPercents           int
	MaxDamage              int
	MaxFrags               int
	MaxXP                  int
	AvgDamageAssisted      float64
	AvgDamageAssistedRadio float64
	AvgDamageAssistedTrack float64
	AvgDamageAssistedStun  float64
	AvgDamageBlocked       float64
	TankingFactor          float64
	CapturePoints          int
	DroppedCapturePoints   int
	RadioAssistedDamage    int
	TrackAssistedDamage    int
	StunAssistedDamage     int
}

// WinRate returns wins as a fraction of battles, or 0 with no battles.
func (s ModeStats) WinRate() float64 {
	if s.Battles == 0 {
		return 0
	}
	return float64(s.Wins) / float64(s.Battles)
}

// DPG returns average damage per battle, or 0 with no battles.
func (s ModeStats) DPG() float64 {
	if s.Battles == 0 {
		return 0
	}
	return float64(s.DamageDealt) / float64(s.Battles)
}

// SurvivalRate returns survived battles as a fraction, or 0 with no battles.
func (s ModeStats) SurvivalRate() float64 {
	if s.Battles == 0 {
		return 0
	}
	return float64(s.SurvivedBattles) / float64(s.Battles)
}

// rawStats mirrors the API's statistics block. It is separate from ModeStats so
// that the JSON shape and the shape the rest of wotctx uses can diverge without
// one dictating the other.
type rawStats struct {
	Battles                int     `json:"battles"`
	Wins                   int     `json:"wins"`
	Losses                 int     `json:"losses"`
	Draws                  int     `json:"draws"`
	SurvivedBattles        int     `json:"survived_battles"`
	DamageDealt            int     `json:"damage_dealt"`
	DamageReceived         int     `json:"damage_received"`
	Frags                  int     `json:"frags"`
	Spotted                int     `json:"spotted"`
	XP                     int     `json:"xp"`
	BattleAvgXP            int     `json:"battle_avg_xp"`
	HitsPercents           int     `json:"hits_percents"`
	MaxDamage              int     `json:"max_damage"`
	MaxFrags               int     `json:"max_frags"`
	MaxXP                  int     `json:"max_xp"`
	AvgDamageAssisted      float64 `json:"avg_damage_assisted"`
	AvgDamageAssistedRadio float64 `json:"avg_damage_assisted_radio"`
	AvgDamageAssistedTrack float64 `json:"avg_damage_assisted_track"`
	AvgDamageAssistedStun  float64 `json:"avg_damage_assisted_stun"`
	AvgDamageBlocked       float64 `json:"avg_damage_blocked"`
	TankingFactor          float64 `json:"tanking_factor"`

	// Totals rather than averages. dropped_capture_points is WN8's "def". The
	// assist totals are present in the random block, which carries none of the
	// avg_* fields above (measured 2026-09-18).
	CapturePoints        int `json:"capture_points"`
	DroppedCapturePoints int `json:"dropped_capture_points"`
	RadioAssistedDamage  int `json:"radio_assisted_damage"`
	TrackAssistedDamage  int `json:"track_assisted_damage"`
	StunAssistedDamage   int `json:"stun_assisted_damage"`
}

func (r rawStats) toModeStats() ModeStats {
	return ModeStats{
		Battles: r.Battles, Wins: r.Wins, Losses: r.Losses, Draws: r.Draws,
		SurvivedBattles: r.SurvivedBattles,
		DamageDealt:     r.DamageDealt, DamageReceived: r.DamageReceived,
		Frags: r.Frags, Spotted: r.Spotted,
		XP: r.XP, BattleAvgXP: r.BattleAvgXP, HitsPercents: r.HitsPercents,
		MaxDamage: r.MaxDamage, MaxFrags: r.MaxFrags, MaxXP: r.MaxXP,
		AvgDamageAssisted:      r.AvgDamageAssisted,
		AvgDamageAssistedRadio: r.AvgDamageAssistedRadio,
		AvgDamageAssistedTrack: r.AvgDamageAssistedTrack,
		AvgDamageAssistedStun:  r.AvgDamageAssistedStun,
		AvgDamageBlocked:       r.AvgDamageBlocked,
		TankingFactor:          r.TankingFactor,
		CapturePoints:          r.CapturePoints,
		DroppedCapturePoints:   r.DroppedCapturePoints,
		RadioAssistedDamage:    r.RadioAssistedDamage,
		TrackAssistedDamage:    r.TrackAssistedDamage,
		StunAssistedDamage:     r.StunAssistedDamage,
	}
}

// AccountInfoResult pairs the parsed account with the raw response.
type AccountInfoResult struct {
	Account Account
	Result  Result
}

// AccountInfo fetches account/info, including the private block when a token is
// supplied.
func (c *Client) AccountInfo(ctx context.Context, accountID int, accessToken string) (AccountInfoResult, error) {
	params := url.Values{}
	params.Set("account_id", strconv.Itoa(accountID))
	params.Set("extra", joinFields(accountExtras))
	params.Set("fields", joinFields(accountFields))
	if accessToken != "" {
		params.Set("access_token", accessToken)
	}

	result, err := c.Get(ctx, "account/info", params, "")
	out := AccountInfoResult{Result: result}
	if err != nil {
		return out, err
	}

	account, err := parseAccountInfo(result.Data, accountID)
	if err != nil {
		return out, err
	}
	out.Account = account
	return out, nil
}

func parseAccountInfo(data json.RawMessage, accountID int) (Account, error) {
	if len(data) == 0 {
		return Account{}, fmt.Errorf("account/info: empty data")
	}

	var byID map[string]*struct {
		AccountID      int    `json:"account_id"`
		Nickname       string `json:"nickname"`
		ClanID         int    `json:"clan_id"`
		CreatedAt      int64  `json:"created_at"`
		LastBattleTime int64  `json:"last_battle_time"`
		LogoutAt       int64  `json:"logout_at"`
		GlobalRating   int    `json:"global_rating"`

		Private *struct {
			Credits          int   `json:"credits"`
			Gold             int   `json:"gold"`
			Bonds            int   `json:"bonds"`
			FreeXP           int   `json:"free_xp"`
			IsPremium        bool  `json:"is_premium"`
			PremiumExpiresAt int64 `json:"premium_expires_at"`
			BattleLifeTime   int   `json:"battle_life_time"`
			Garage           []int `json:"garage"`
			Boosters         map[string]*struct {
				Count          int    `json:"count"`
				ExpirationTime int64  `json:"expiration_time"`
				State          string `json:"state"`
			} `json:"boosters"`
			PersonalMissions map[string]string `json:"personal_missions"`
		} `json:"private"`

		Statistics *struct {
			Random *rawStats `json:"random"`
			All    *rawStats `json:"all"`
		} `json:"statistics"`
	}

	if err := json.Unmarshal(data, &byID); err != nil {
		return Account{}, fmt.Errorf("account/info: parsing data: %w", err)
	}

	entry, ok := byID[strconv.Itoa(accountID)]
	if !ok || entry == nil {
		// A valid request for an account with a hidden profile returns a null
		// entry rather than an error, so say which case this is.
		return Account{}, fmt.Errorf("account/info: no data for account %d (hidden profile, or wrong realm)", accountID)
	}

	account := Account{
		AccountID:      entry.AccountID,
		Nickname:       entry.Nickname,
		ClanID:         entry.ClanID,
		CreatedAt:      unixOrZero(entry.CreatedAt),
		LastBattleTime: unixOrZero(entry.LastBattleTime),
		LogoutAt:       unixOrZero(entry.LogoutAt),
		GlobalRating:   entry.GlobalRating,
	}
	if account.AccountID == 0 {
		account.AccountID = accountID
	}

	if p := entry.Private; p != nil {
		account.HasPrivate = true
		account.Credits = p.Credits
		account.Gold = p.Gold
		account.Bonds = p.Bonds
		account.FreeXP = p.FreeXP
		account.IsPremium = p.IsPremium
		account.PremiumExpiresAt = unixOrZero(p.PremiumExpiresAt)
		account.BattleLifeTime = time.Duration(p.BattleLifeTime) * time.Second
		account.Garage = append([]int(nil), p.Garage...)
		sort.Ints(account.Garage)

		if len(p.PersonalMissions) > 0 {
			account.PersonalMissions = make(map[int]string, len(p.PersonalMissions))
			for id, status := range p.PersonalMissions {
				if n, err := strconv.Atoi(id); err == nil && n > 0 && status != "" {
					account.PersonalMissions[n] = status
				}
			}
		}

		for kind, b := range p.Boosters {
			if b == nil {
				continue
			}
			account.Boosters = append(account.Boosters, Booster{
				Kind:      kind,
				Count:     b.Count,
				State:     b.State,
				ExpiresAt: unixOrZero(b.ExpirationTime),
			})
		}
		sort.Slice(account.Boosters, func(i, j int) bool {
			if account.Boosters[i].Kind != account.Boosters[j].Kind {
				return account.Boosters[i].Kind < account.Boosters[j].Kind
			}
			return account.Boosters[i].State < account.Boosters[j].State
		})
	}

	if s := entry.Statistics; s != nil && s.Random != nil {
		account.RandomStats = s.Random.toModeStats()
	}
	return account, nil
}

func unixOrZero(seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}
