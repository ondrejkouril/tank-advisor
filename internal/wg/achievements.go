package wg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
)

// Achievement kinds. The API returns three blocks with different names but the
// same shape, and they are always read together.
const (
	// KindAchievement is a medal or badge held, such as Top Gun.
	KindAchievement = "achievement"
	// KindMaxSeries is a personal best streak, such as consecutive victories.
	KindMaxSeries = "max_series"
	// KindFrags counts kill-based awards.
	KindFrags = "frags"
)

// AchievementCount is one achievement and how many times it was earned.
type AchievementCount struct {
	Kind  string
	Code  string
	Count int
}

// AchievementDef is an achievement's definition from the encyclopedia.
//
// Without these the counts are unreadable: the API reports codes like
// "medalKay" and "warrior", and the condition text is what makes "which medals
// am I close to" answerable at all.
type AchievementDef struct {
	Code         string
	Name         string
	Description  string
	Condition    string
	HeroInfo     string
	Section      string
	SectionOrder int
	Order        int
	Image        string
}

// AccountAchievementsResult pairs account-wide counts with the raw response.
type AccountAchievementsResult struct {
	Counts []AchievementCount
	Result Result
}

// AccountAchievements fetches account/achievements.
func (c *Client) AccountAchievements(ctx context.Context, accountID int) (AccountAchievementsResult, error) {
	params := url.Values{}
	params.Set("account_id", strconv.Itoa(accountID))

	result, err := c.Get(ctx, "account/achievements", params, "")
	out := AccountAchievementsResult{Result: result}
	if err != nil {
		return out, err
	}

	var byAccount map[string]*achievementBlocks
	if err := json.Unmarshal(result.Data, &byAccount); err != nil {
		return out, fmt.Errorf("account/achievements: parsing data: %w", err)
	}

	entry, ok := byAccount[strconv.Itoa(accountID)]
	if !ok || entry == nil {
		return out, fmt.Errorf("account/achievements: no data for account %d", accountID)
	}

	out.Counts = entry.flatten()
	return out, nil
}

// TankAchievement is one tank's achievement counts.
type TankAchievement struct {
	TankID int
	Counts []AchievementCount
}

// TankAchievementsResult pairs per-tank counts with the raw response.
type TankAchievementsResult struct {
	Tanks  []TankAchievement
	Result Result
}

// TankAchievements fetches tanks/achievements for the whole account.
func (c *Client) TankAchievements(ctx context.Context, accountID int, accessToken string) (TankAchievementsResult, error) {
	params := url.Values{}
	params.Set("account_id", strconv.Itoa(accountID))
	if accessToken != "" {
		params.Set("access_token", accessToken)
	}

	result, err := c.Get(ctx, "tanks/achievements", params, "")
	out := TankAchievementsResult{Result: result}
	if err != nil {
		return out, err
	}

	var byAccount map[string][]*struct {
		TankID int `json:"tank_id"`
		achievementBlocks
	}
	if err := json.Unmarshal(result.Data, &byAccount); err != nil {
		return out, fmt.Errorf("tanks/achievements: parsing data: %w", err)
	}

	entries, ok := byAccount[strconv.Itoa(accountID)]
	if !ok {
		return out, fmt.Errorf("tanks/achievements: no data for account %d", accountID)
	}

	for _, e := range entries {
		if e == nil || e.TankID == 0 {
			continue
		}
		counts := e.flatten()
		if len(counts) == 0 {
			continue
		}
		out.Tanks = append(out.Tanks, TankAchievement{TankID: e.TankID, Counts: counts})
	}

	sort.Slice(out.Tanks, func(i, j int) bool { return out.Tanks[i].TankID < out.Tanks[j].TankID })
	return out, nil
}

// achievementBlocks is the three-block shape both achievement endpoints share.
type achievementBlocks struct {
	Achievements map[string]int `json:"achievements"`
	MaxSeries    map[string]int `json:"max_series"`
	Frags        map[string]int `json:"frags"`
}

// flatten turns the three maps into one sorted slice, so that storage and
// comparison do not have to know about the split.
func (b achievementBlocks) flatten() []AchievementCount {
	var counts []AchievementCount

	for kind, block := range map[string]map[string]int{
		KindAchievement: b.Achievements,
		KindMaxSeries:   b.MaxSeries,
		KindFrags:       b.Frags,
	} {
		for code, count := range block {
			if count == 0 {
				continue // never earned; storing a zero would imply it was measured
			}
			counts = append(counts, AchievementCount{Kind: kind, Code: code, Count: count})
		}
	}

	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Kind != counts[j].Kind {
			return counts[i].Kind < counts[j].Kind
		}
		return counts[i].Code < counts[j].Code
	})
	return counts
}

// AchievementDefsResult pairs definitions with the raw response.
type AchievementDefsResult struct {
	Defs   []AchievementDef
	Result Result
}

// EncyclopediaAchievements fetches the achievement definitions.
func (c *Client) EncyclopediaAchievements(ctx context.Context, ifNoneMatch string) (AchievementDefsResult, error) {
	result, err := c.Get(ctx, "encyclopedia/achievements", nil, ifNoneMatch)
	out := AchievementDefsResult{Result: result}
	if err != nil || result.NotModified {
		return out, err
	}

	var byCode map[string]*struct {
		Name         string `json:"name"`
		NameI18n     string `json:"name_i18n"`
		Description  string `json:"description"`
		Condition    string `json:"condition"`
		HeroInfo     string `json:"hero_info"`
		Section      string `json:"section"`
		SectionOrder int    `json:"section_order"`
		Order        int    `json:"order"`
		Image        string `json:"image"`
	}
	if err := json.Unmarshal(result.Data, &byCode); err != nil {
		return out, fmt.Errorf("encyclopedia/achievements: parsing data: %w", err)
	}

	for code, d := range byCode {
		if d == nil {
			continue
		}
		// name_i18n is the localised label; name is an internal identifier that
		// is often just the code again.
		label := d.NameI18n
		if label == "" {
			label = d.Name
		}
		out.Defs = append(out.Defs, AchievementDef{
			Code:         code,
			Name:         label,
			Description:  d.Description,
			Condition:    d.Condition,
			HeroInfo:     d.HeroInfo,
			Section:      d.Section,
			SectionOrder: d.SectionOrder,
			Order:        d.Order,
			Image:        d.Image,
		})
	}

	sort.Slice(out.Defs, func(i, j int) bool { return out.Defs[i].Code < out.Defs[j].Code })
	return out, nil
}
