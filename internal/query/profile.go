package query

import (
	"context"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// ProfileCaveat travels with every characteristic.
const ProfileCaveat = "characteristics are Wargaming's nominal figures with a fully trained crew and no equipment, consumables, " +
	"directives or field modifications fitted; dpm uses the standard (first) shell and Wargaming's rate of fire"

// ShellLine is one shell's average damage and penetration.
type ShellLine struct {
	Type        string `json:"type"`
	Damage      int    `json:"damage"`
	Penetration int    `json:"penetration"`
}

// ConfigLine is a vehicle's characteristics in one configuration.
type ConfigLine struct {
	Gun        string `json:"gun"`
	Turret     string `json:"turret"`
	Engine     string `json:"engine"`
	Suspension string `json:"suspension"`
	Radio      string `json:"radio"`

	HP            int         `json:"hp"`
	DPM           int         `json:"dpm"`
	Shells        []ShellLine `json:"shells"`
	ReloadTime    float64     `json:"reload_time"`
	AimTime       float64     `json:"aim_time"`
	Dispersion    float64     `json:"dispersion"`
	GunDepression int         `json:"gun_depression"`
	GunElevation  int         `json:"gun_elevation"`

	ViewRange      int     `json:"view_range"`
	RadioRange     int     `json:"radio_range"`
	EnginePower    int     `json:"engine_power"`
	PowerToWeight  float64 `json:"power_to_weight"`
	SpeedForward   int     `json:"speed_forward"`
	SpeedBackward  int     `json:"speed_backward"`
	HullTraverse   int     `json:"hull_traverse"`
	TurretTraverse int     `json:"turret_traverse"`

	HullArmor   wg.ArmorFaces `json:"hull_armor"`
	TurretArmor wg.ArmorFaces `json:"turret_armor"`
}

func configLine(p wg.Profile) *ConfigLine {
	c := &ConfigLine{
		Gun: p.Gun.Name, Turret: p.Turret.Name, Engine: p.Engine.Name, Suspension: p.Suspension.Name, Radio: p.Radio.Name,
		HP: p.HP, DPM: meta.DPM(p), ReloadTime: p.Gun.ReloadTime, AimTime: p.Gun.AimTime, Dispersion: p.Gun.Dispersion,
		GunDepression: p.Gun.MoveDownArc, GunElevation: p.Gun.MoveUpArc,
		ViewRange: p.Turret.ViewRange, RadioRange: p.Radio.SignalRange, EnginePower: p.Engine.Power,
		PowerToWeight: meta.PowerToWeight(p), SpeedForward: p.SpeedForward, SpeedBackward: p.SpeedBackward,
		HullTraverse: p.Suspension.TraverseSpeed, TurretTraverse: p.Turret.TraverseSpeed,
		HullArmor: p.Armor.Hull, TurretArmor: p.Armor.Turret, Shells: []ShellLine{},
	}
	for _, a := range p.Ammo {
		if len(a.Damage) > 1 && len(a.Penetration) > 1 {
			c.Shells = append(c.Shells, ShellLine{Type: a.Type, Damage: a.Damage[1], Penetration: a.Penetration[1]})
		}
	}
	return c
}

// ModuleLine is one module to research.
type ModuleLine struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	XP      int    `json:"xp"`
	Credits int    `json:"credits"`
}

// VehicleProfile is `meta profile`.
type VehicleProfile struct {
	TankID int    `json:"tank_id"`
	Name   string `json:"name"`
	Tier   int    `json:"tier"`
	Class  string `json:"class"`

	Stock *ConfigLine `json:"stock"`
	// Top is absent when the stock configuration is the only one.
	Top      *ConfigLine  `json:"top,omitempty"`
	Research []ModuleLine `json:"research,omitempty"`
	XPToTop  int          `json:"xp_to_top,omitempty"`

	// The game client's figures for the player's vehicle, from the mod's dump.
	VehicleXP  *int       `json:"vehicle_xp,omitempty"`
	Elite      *bool      `json:"elite,omitempty"`
	CapturedAt *time.Time `json:"client_captured_at,omitempty"`
}

// Profile reports a vehicle's stock and top configurations from freshly
// fetched Wargaming profiles, with the player's XP on it when the client mod
// has it.
func (s *Service) Profile(ctx context.Context, ref string, fetch func(tankID int) (meta.Configs, error)) (Envelope, error) {
	v, err := s.resolveTank(ctx, ref)
	if err != nil {
		return Envelope{}, err
	}
	cfg, err := fetch(v.TankID)
	if err != nil {
		return Envelope{}, err
	}
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	r.use(srcVehicles)
	now := r.now
	r.meta.Sources = append(r.meta.Sources, Source{Name: "wg:encyclopedia/vehicleprofile", ObservedAt: now})
	r.caveat("%s", ProfileCaveat)

	out := VehicleProfile{TankID: v.TankID, Name: v.Name, Tier: v.Tier, Class: v.Type, Stock: configLine(cfg.Stock)}
	if cfg.Top == nil {
		r.caveat("nothing to research: the stock configuration is %s's only one", v.Name)
	} else {
		out.Top = configLine(*cfg.Top)
		out.XPToTop = cfg.XPToTop
		for _, m := range cfg.Research {
			out.Research = append(out.Research, ModuleLine{Type: m.Type, Name: m.Name, XP: m.PriceXP, Credits: m.PriceCredit})
		}
	}

	if c := r.client(); c != nil {
		if mv, ok := c.vehicles[v.TankID]; ok {
			out.VehicleXP, out.Elite = mv.XP, mv.Elite
			t := c.account.CapturedAt
			out.CapturedAt = &t
		}
	}
	if out.Top != nil {
		switch {
		case out.Elite != nil && *out.Elite:
			r.caveat("the game client shows the vehicle elite: every module above is researched")
		case out.VehicleXP != nil:
			r.caveat("which of these modules are already researched is not known (no API or dump records it); " +
				"vehicle_xp is the XP on the vehicle now, which the next module can be researched with")
		default:
			r.caveat("which of these modules are already researched is not known, and with no client mod dump for this vehicle, neither is its XP")
		}
	}
	return r.finish(out), nil
}
