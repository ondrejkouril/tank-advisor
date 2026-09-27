package wg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// PersonalMission is one mission from encyclopedia/personalmissions.
//
// Measured 2026-09-18, the endpoint describes only the first campaign
// ("Long-Awaited Backup": StuG IV, T28 HTC, T 55A, Object 260). Statuses for
// later campaigns come back in account/info but cannot be named from here.
type PersonalMission struct {
	MissionID   int
	CampaignID  int
	Campaign    string
	OperationID int
	Operation   string
	// SetID groups an operation's missions by vehicle class; within a set they
	// are done in mission-id order.
	SetID     int
	Name      string
	Class     string // API vehicle type, from the mission's tags
	MinTier   int
	MaxTier   int
	Primary   string // conditions for the main reward
	Secondary string // conditions for the reward "with honors"
}

// PersonalMissionsResult pairs parsed missions with the raw response.
type PersonalMissionsResult struct {
	Missions []PersonalMission
	Result   Result
}

// PersonalMissions fetches the personal-mission encyclopedia.
func (c *Client) PersonalMissions(ctx context.Context) (PersonalMissionsResult, error) {
	result, err := c.Get(ctx, "encyclopedia/personalmissions", url.Values{"language": {"en"}}, "")
	out := PersonalMissionsResult{Result: result}
	if err != nil {
		return out, err
	}
	missions, err := parsePersonalMissions(result.Data)
	if err != nil {
		return out, err
	}
	out.Missions = missions
	return out, nil
}

// missionClasses are the tags that name a mission's vehicle class.
var missionClasses = map[string]bool{
	"lightTank": true, "mediumTank": true, "heavyTank": true, "AT-SPG": true, "SPG": true,
}

func parsePersonalMissions(data json.RawMessage) ([]PersonalMission, error) {
	var campaigns map[string]*struct {
		CampaignID int    `json:"campaign_id"`
		Name       string `json:"name"`
		Operations map[string]*struct {
			OperationID int    `json:"operation_id"`
			Name        string `json:"name"`
			Missions    map[string]*struct {
				MissionID int      `json:"mission_id"`
				SetID     int      `json:"set_id"`
				Name      string   `json:"name"`
				Tags      []string `json:"tags"`
				MinLevel  int      `json:"min_level"`
				MaxLevel  int      `json:"max_level"`
				Rewards   struct {
					Primary *struct {
						Conditions string `json:"conditions"`
					} `json:"primary"`
					Secondary *struct {
						Conditions string `json:"conditions"`
					} `json:"secondary"`
				} `json:"rewards"`
			} `json:"missions"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(data, &campaigns); err != nil {
		return nil, fmt.Errorf("encyclopedia/personalmissions: parsing data: %w", err)
	}

	var out []PersonalMission
	for cid, camp := range campaigns {
		if camp == nil {
			continue
		}
		campaignID := camp.CampaignID
		if campaignID == 0 {
			campaignID, _ = strconv.Atoi(cid)
		}
		for oid, op := range camp.Operations {
			if op == nil {
				continue
			}
			operationID := op.OperationID
			if operationID == 0 {
				operationID, _ = strconv.Atoi(oid)
			}
			for mid, m := range op.Missions {
				if m == nil {
					continue
				}
				missionID := m.MissionID
				if missionID == 0 {
					missionID, _ = strconv.Atoi(mid)
				}
				pm := PersonalMission{
					MissionID: missionID, CampaignID: campaignID, Campaign: camp.Name,
					OperationID: operationID, Operation: op.Name, SetID: m.SetID,
					Name: m.Name, MinTier: m.MinLevel, MaxTier: m.MaxLevel,
				}
				for _, tag := range m.Tags {
					if missionClasses[tag] {
						pm.Class = tag
					}
				}
				if p := m.Rewards.Primary; p != nil {
					pm.Primary = cleanConditions(p.Conditions)
				}
				if s := m.Rewards.Secondary; s != nil {
					pm.Secondary = cleanConditions(s.Conditions)
				}
				out = append(out, pm)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MissionID < out[j].MissionID })
	return out, nil
}

// cleanConditions turns the API's bulleted text into one line per condition.
func cleanConditions(s string) string {
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, l := range lines {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "•"))
		if l != "" {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "; ")
}
