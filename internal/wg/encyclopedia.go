package wg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
)

// vehicleFields is the explicit field list sent with every encyclopedia request.
//
// Without it the API returns default_profile for every vehicle - full gun,
// armour, engine and ammunition characteristics - which is two orders of
// magnitude more data than wotctx needs and is not what any question here turns
// on. Asking for fields by name keeps a full sync small enough to store
// verbatim.
var vehicleFields = []string{
	"tank_id", "name", "short_name", "tier", "type", "nation", "tag",
	"is_premium", "is_gift", "is_wheeled", "price_credit", "price_gold",
	"next_tanks", "prices_xp",
}

// pageLimit is the API maximum.
const pageLimit = 100

// Vehicle is one entry from encyclopedia/vehicles.
type Vehicle struct {
	TankID      int    `json:"tank_id"`
	Name        string `json:"name"`
	ShortName   string `json:"short_name"`
	Tier        int    `json:"tier"`
	Type        string `json:"type"`
	Nation      string `json:"nation"`
	Tag         string `json:"tag"`
	IsPremium   bool   `json:"is_premium"`
	IsGift      bool   `json:"is_gift"`
	IsWheeled   bool   `json:"is_wheeled"`
	PriceCredit int    `json:"price_credit"`
	PriceGold   int    `json:"price_gold"`

	// NextTanks maps a researchable vehicle id to its XP cost. Premium and gift
	// vehicles have none, and so does the top of a line.
	NextTanks map[string]int `json:"next_tanks"`
	// PricesXP maps a parent vehicle id to the XP cost of researching this one
	// from it. It is the same edge as NextTanks seen from the other end; both
	// are kept because either side can be missing.
	PricesXP map[string]int `json:"prices_xp"`
}

// Edge is one tech-tree link.
type Edge struct {
	From   int
	To     int
	XPCost int
	// Source records which map the edge came from, so an answer can say how it
	// knows a vehicle is researchable.
	Source string
}

// Edge sources.
const (
	EdgeSourceNextTanks = "next_tanks"
	EdgeSourcePricesXP  = "prices_xp"
	EdgeSourceOverlay   = "overlay"
)

// Edges derives this vehicle's tech-tree links in both directions.
func (v Vehicle) Edges() []Edge {
	var edges []Edge

	for id, cost := range v.NextTanks {
		to, err := strconv.Atoi(id)
		if err != nil {
			continue
		}
		edges = append(edges, Edge{From: v.TankID, To: to, XPCost: cost, Source: EdgeSourceNextTanks})
	}
	for id, cost := range v.PricesXP {
		from, err := strconv.Atoi(id)
		if err != nil {
			continue
		}
		edges = append(edges, Edge{From: from, To: v.TankID, XPCost: cost, Source: EdgeSourcePricesXP})
	}

	// Deterministic order, so a re-sync writes identical rows and diffs stay
	// readable.
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Source < edges[j].Source
	})
	return edges
}

// VehiclePage is one page of encyclopedia results plus the raw response, which
// the caller records as a snapshot.
type VehiclePage struct {
	Vehicles []Vehicle
	Result   Result

	Page      int
	PageTotal int
	Total     int
}

// encyclopediaMeta is the paging block every encyclopedia response carries.
type encyclopediaMeta struct {
	Count     int `json:"count"`
	PageTotal int `json:"page_total"`
	Total     int `json:"total"`
	Limit     int `json:"limit"`
	Page      int `json:"page"`
}

// VehiclePageRequest asks for one page of the vehicle encyclopedia.
func (c *Client) VehiclePage(ctx context.Context, pageNo int, ifNoneMatch string) (VehiclePage, error) {
	params := url.Values{}
	params.Set("fields", joinFields(vehicleFields))
	params.Set("limit", strconv.Itoa(pageLimit))
	if pageNo > 0 {
		params.Set("page_no", strconv.Itoa(pageNo))
	}

	result, err := c.Get(ctx, "encyclopedia/vehicles", params, ifNoneMatch)
	page := VehiclePage{Result: result, Page: pageNo}
	if err != nil {
		return page, err
	}
	if result.NotModified {
		return page, nil
	}

	vehicles, err := ParseVehicles(result.Data)
	if err != nil {
		return page, err
	}
	page.Vehicles = vehicles

	if len(result.Meta) > 0 {
		var meta encyclopediaMeta
		if err := json.Unmarshal(result.Meta, &meta); err != nil {
			return page, fmt.Errorf("encyclopedia/vehicles: parsing meta: %w", err)
		}
		page.PageTotal = meta.PageTotal
		page.Total = meta.Total
		if meta.Page > 0 {
			page.Page = meta.Page
		}
	}
	return page, nil
}

// ParseVehicles decodes the `data` member, which is an object keyed by vehicle
// id rather than an array.
func ParseVehicles(data json.RawMessage) ([]Vehicle, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var byID map[string]Vehicle
	if err := json.Unmarshal(data, &byID); err != nil {
		return nil, fmt.Errorf("encyclopedia/vehicles: parsing data: %w", err)
	}

	vehicles := make([]Vehicle, 0, len(byID))
	for key, v := range byID {
		// Trust the key over the body: the body's tank_id is omitted if it was
		// not in the requested field list.
		if v.TankID == 0 {
			if id, err := strconv.Atoi(key); err == nil {
				v.TankID = id
			}
		}
		if v.TankID == 0 {
			continue
		}
		// Asked for a vehicle the encyclopedia does not describe - a hidden or
		// event vehicle - the API returns the id as a key with a null body. The
		// id is real, but there is nothing to record, and storing a nameless
		// tier-0 row would put a vehicle in the reference list that no question
		// could sensibly use. Callers that own such a vehicle learn about it
		// from the garage instead, where it is reported as unidentified.
		//
		// This relies on name and tier being in the requested field list, which
		// vehicleFields guarantees.
		if v.Name == "" && v.Tier == 0 {
			continue
		}
		vehicles = append(vehicles, v)
	}

	sort.Slice(vehicles, func(i, j int) bool { return vehicles[i].TankID < vehicles[j].TankID })
	return vehicles, nil
}

func joinFields(fields []string) string {
	out := ""
	for i, f := range fields {
		if i > 0 {
			out += ","
		}
		out += f
	}
	return out
}
