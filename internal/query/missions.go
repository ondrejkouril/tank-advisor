package query

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

const srcMissions = "wg:encyclopedia/personalmissions"

// Mission statuses as reported, mapped to plain words.
const (
	MissionDoneWithHonors = "done_with_honors" // ALL_REWARDS_GOTTEN: main and secondary conditions
	MissionDone           = "done"             // MAIN_REWARD_GOTTEN: main conditions only
	MissionNotDone        = "not_done"         // no status: not completed; started or not is unknown
)

// missionClassOrder is the order classes are listed in.
var missionClassOrder = map[string]int{"lightTank": 0, "mediumTank": 1, "heavyTank": 2, "AT-SPG": 3, "SPG": 4}

// Missions is `query missions`.
type Missions struct {
	// StatusAt is when the statuses were observed (the account snapshot).
	StatusAt   time.Time           `json:"status_at"`
	Operations []OperationProgress `json:"operations"`
	// UndescribedStatuses counts statuses for missions the API's encyclopedia
	// does not describe (later campaigns): they cannot be named.
	UndescribedStatuses int              `json:"undescribed_statuses"`
	Detail              *OperationDetail `json:"detail,omitempty"`
}

// OperationProgress summarises one operation.
type OperationProgress struct {
	CampaignID  int    `json:"campaign_id"`
	Campaign    string `json:"campaign"`
	OperationID int    `json:"operation_id"`
	// Operation is named after its reward vehicle.
	Operation      string      `json:"operation"`
	Missions       int         `json:"missions"`
	Done           int         `json:"done"`
	DoneWithHonors int         `json:"done_with_honors"`
	NotDone        int         `json:"not_done"`
	OpenByClass    []ClassOpen `json:"open_by_class"`
}

// ClassOpen counts one class's open missions in an operation, with the
// lowest-numbered one.
type ClassOpen struct {
	Class string      `json:"class"`
	Open  int         `json:"open"`
	First MissionView `json:"first"`
}

// OperationDetail lists every mission of one operation.
type OperationDetail struct {
	OperationID int    `json:"operation_id"`
	Operation   string `json:"operation"`
	// OpenOnly reports that Missions was limited to missions not done.
	OpenOnly bool          `json:"open_only"`
	Missions []MissionView `json:"missions"`
}

// MissionView is one mission with its status.
type MissionView struct {
	MissionID int    `json:"mission_id"`
	Name      string `json:"name"`
	Class     string `json:"class"`
	MinTier   int    `json:"min_tier"`
	MaxTier   int    `json:"max_tier"`
	Status    string `json:"status"`
	Primary   string `json:"primary"`
	Secondary string `json:"secondary,omitempty"`
}

// Missions reports personal-mission progress. operation, when non-empty,
// selects one operation by id or by (part of) its name for a full listing;
// openOnly limits that listing to missions not yet done, so a caller never has
// to filter the full list itself (evaluation run 1 did, with grep, and
// misattributed a mission).
func (s *Service) Missions(ctx context.Context, operation string, openOnly bool) (Envelope, error) {
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcAccount, srcMissions)
	r.caveat("the Dravec, Fossa VM 68 and Black Rock mission chains are not in the API; ask the player for that progress")

	out := Missions{Operations: []OperationProgress{}}
	statuses, at, err := s.DB.LatestMissionStatuses(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		r.caveat("no personal-mission statuses synced yet (they arrive with account/info; run: wotctx sync)")
		statuses = map[int]string{}
	case err != nil:
		return Envelope{}, err
	default:
		out.StatusAt = at
	}
	missions, err := s.DB.PersonalMissions(ctx)
	if err != nil {
		return Envelope{}, err
	}
	if len(missions) == 0 {
		r.caveat("the personal-mission encyclopedia has not been synced (run: wotctx sync)")
	}
	r.caveat("not_done means not completed; the API does not say whether a mission was started")

	described := map[int]bool{}
	type opKey struct{ campaign, operation int }
	byOp := map[opKey][]store.PersonalMission{}
	for _, m := range missions {
		described[m.MissionID] = true
		k := opKey{m.CampaignID, m.OperationID}
		byOp[k] = append(byOp[k], m)
	}
	for id := range statuses {
		if !described[id] {
			out.UndescribedStatuses++
		}
	}
	if out.UndescribedStatuses > 0 {
		r.caveat("%d mission status(es) belong to campaigns the API does not describe; they cannot be named",
			out.UndescribedStatuses)
	}

	keys := make([]opKey, 0, len(byOp))
	for k := range byOp {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].campaign != keys[j].campaign {
			return keys[i].campaign < keys[j].campaign
		}
		return keys[i].operation < keys[j].operation
	})

	wanted := strings.ToLower(strings.TrimSpace(operation))
	for _, k := range keys {
		ms := byOp[k]
		p := OperationProgress{
			CampaignID: k.campaign, Campaign: ms[0].Campaign,
			OperationID: k.operation, Operation: ms[0].Operation,
			Missions: len(ms), OpenByClass: []ClassOpen{},
		}
		open := map[string]*ClassOpen{}
		for _, m := range ms {
			view := missionView(m, statuses[m.MissionID])
			switch view.Status {
			case MissionDoneWithHonors:
				p.DoneWithHonors++
				p.Done++
			case MissionDone:
				p.Done++
			default:
				p.NotDone++
				// Completion is not strictly in id order - the live account has
				// HT-10 and HT-11 done with HT-9 open - so this reports the
				// first open mission and a count, not "the next" one.
				c, seen := open[m.Class]
				if !seen {
					c = &ClassOpen{Class: m.Class, First: view}
					open[m.Class] = c
				}
				c.Open++
			}
		}
		for _, c := range open {
			p.OpenByClass = append(p.OpenByClass, *c)
		}
		sort.Slice(p.OpenByClass, func(i, j int) bool {
			return missionClassOrder[p.OpenByClass[i].Class] < missionClassOrder[p.OpenByClass[j].Class]
		})
		out.Operations = append(out.Operations, p)

		if wanted != "" && out.Detail == nil &&
			(wanted == strconv.Itoa(k.operation) || strings.Contains(strings.ToLower(p.Operation), wanted)) {
			d := &OperationDetail{OperationID: k.operation, Operation: p.Operation, OpenOnly: openOnly, Missions: []MissionView{}}
			for _, m := range ms {
				view := missionView(m, statuses[m.MissionID])
				if openOnly && view.Status != MissionNotDone {
					continue
				}
				d.Missions = append(d.Missions, view)
			}
			out.Detail = d
		}
	}
	if wanted != "" && out.Detail == nil {
		r.caveat("no operation matches %q", operation)
	}
	return r.finish(out), nil
}

func missionView(m store.PersonalMission, raw string) MissionView {
	return MissionView{
		MissionID: m.MissionID, Name: m.Name, Class: m.Class,
		MinTier: m.MinTier, MaxTier: m.MaxTier, Status: missionStatus(raw),
		Primary: m.Primary, Secondary: m.Secondary,
	}
}

func missionStatus(raw string) string {
	switch raw {
	case "ALL_REWARDS_GOTTEN":
		return MissionDoneWithHonors
	case "MAIN_REWARD_GOTTEN":
		return MissionDone
	case "":
		return MissionNotDone
	default:
		return strings.ToLower(raw)
	}
}
