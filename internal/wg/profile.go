package wg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// Module is one entry of a vehicle's modules_tree.
type Module struct {
	ModuleID    int    `json:"module_id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // vehicleGun, vehicleTurret, vehicleEngine, vehicleChassis, vehicleRadio
	PriceXP     int    `json:"price_xp"`
	PriceCredit int    `json:"price_credit"`
	IsDefault   bool   `json:"is_default"`
	NextModules []int  `json:"next_modules"`
}

// ProfileRef is one valid module combination from encyclopedia/vehicleprofiles.
type ProfileRef struct {
	ProfileID   string `json:"profile_id"`
	IsDefault   bool   `json:"is_default"`
	PriceCredit int    `json:"price_credit"`
}

// Profile is a vehicle's characteristics in one configuration, from
// encyclopedia/vehicleprofile. Only the fields an answer turns on are read.
type Profile struct {
	ProfileID     string `json:"profile_id"`
	IsDefault     bool   `json:"is_default"`
	HP            int    `json:"hp"`
	Weight        int    `json:"weight"` // kg
	SpeedForward  int    `json:"speed_forward"`
	SpeedBackward int    `json:"speed_backward"`
	MaxAmmo       int    `json:"max_ammo"`
	Gun           struct {
		Name          string  `json:"name"`
		Caliber       int     `json:"caliber"`
		ReloadTime    float64 `json:"reload_time"`
		FireRate      float64 `json:"fire_rate"`
		AimTime       float64 `json:"aim_time"`
		Dispersion    float64 `json:"dispersion"`
		MoveDownArc   int     `json:"move_down_arc"`
		MoveUpArc     int     `json:"move_up_arc"`
		TraverseSpeed int     `json:"traverse_speed"`
	} `json:"gun"`
	Ammo   []Shell `json:"ammo"`
	Turret struct {
		Name          string `json:"name"`
		ViewRange     int    `json:"view_range"`
		TraverseSpeed int    `json:"traverse_speed"`
	} `json:"turret"`
	Engine struct {
		Name  string `json:"name"`
		Power int    `json:"power"`
	} `json:"engine"`
	Suspension struct {
		Name          string `json:"name"`
		TraverseSpeed int    `json:"traverse_speed"`
	} `json:"suspension"`
	Radio struct {
		Name        string `json:"name"`
		SignalRange int    `json:"signal_range"`
	} `json:"radio"`
	Armor struct {
		Hull   ArmorFaces `json:"hull"`
		Turret ArmorFaces `json:"turret"`
	} `json:"armor"`
}

// ArmorFaces is nominal armour in millimetres.
type ArmorFaces struct {
	Front int `json:"front"`
	Sides int `json:"sides"`
	Rear  int `json:"rear"`
}

// ModulesTree fetches one vehicle's modules: what each costs in XP and
// what it leads to. The synced encyclopedia leaves it out (vehicleFields).
func (c *Client) ModulesTree(ctx context.Context, tankID int) (map[int]Module, error) {
	result, err := c.Get(ctx, "encyclopedia/vehicles", url.Values{
		"tank_id": {strconv.Itoa(tankID)}, "fields": {"modules_tree"}, "language": {"en"},
	}, "")
	if err != nil {
		return nil, err
	}
	var data map[string]*struct {
		ModulesTree map[string]Module `json:"modules_tree"`
	}
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return nil, fmt.Errorf("encyclopedia/vehicles modules_tree: %w", err)
	}
	v := data[strconv.Itoa(tankID)]
	if v == nil {
		return nil, fmt.Errorf("encyclopedia/vehicles: no vehicle %d", tankID)
	}
	tree := make(map[int]Module, len(v.ModulesTree))
	for _, m := range v.ModulesTree {
		tree[m.ModuleID] = m
	}
	return tree, nil
}

// VehicleProfiles lists a vehicle's valid module combinations.
func (c *Client) VehicleProfiles(ctx context.Context, tankID int) ([]ProfileRef, error) {
	result, err := c.Get(ctx, "encyclopedia/vehicleprofiles", url.Values{"tank_id": {strconv.Itoa(tankID)}, "language": {"en"}}, "")
	if err != nil {
		return nil, err
	}
	var data map[string][]ProfileRef
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return nil, fmt.Errorf("encyclopedia/vehicleprofiles: %w", err)
	}
	refs := data[strconv.Itoa(tankID)]
	if len(refs) == 0 {
		return nil, fmt.Errorf("encyclopedia/vehicleprofiles: no profiles for vehicle %d", tankID)
	}
	return refs, nil
}

// VehicleProfile fetches one configuration; an empty profileID asks for the
// stock one.
func (c *Client) VehicleProfile(ctx context.Context, tankID int, profileID string) (Profile, error) {
	// Without a language, names come back in Wargaming's default for the
	// application, which was Czech on 2026-10-09.
	params := url.Values{"tank_id": {strconv.Itoa(tankID)}, "language": {"en"}}
	if profileID != "" {
		params.Set("profile_id", profileID)
	}
	result, err := c.Get(ctx, "encyclopedia/vehicleprofile", params, "")
	if err != nil {
		return Profile{}, err
	}
	var data map[string]*Profile
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return Profile{}, fmt.Errorf("encyclopedia/vehicleprofile: %w", err)
	}
	p := data[strconv.Itoa(tankID)]
	if p == nil {
		return Profile{}, fmt.Errorf("encyclopedia/vehicleprofile: no profile for vehicle %d", tankID)
	}
	return *p, nil
}

// Shell is one shell type's damage and penetration: minimum, average, maximum.
type Shell struct {
	Type        string `json:"type"`
	Damage      []int  `json:"damage"`
	Penetration []int  `json:"penetration"`
}
