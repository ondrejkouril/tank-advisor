package syncer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/ondrejkouril/tank-advisor/internal/mod"
	"github.com/ondrejkouril/tank-advisor/internal/store"
)

const (
	modSource   = "mod"
	modEndpoint = "garage"
)

// ModSourceKey names the client mod's dump in envelopes and reports.
var ModSourceKey = store.SourceKey(modSource, modEndpoint)

// syncModGarage stores the client mod's dump when it is newer than the last one
// kept. It is a file on this machine, not a request, so it has no TTL: a dump
// is taken as soon as the client writes a new one.
//
// No dump at all is not a failure. The mod is optional, and a machine without
// it syncs exactly as before.
func (s *Syncer) syncModGarage(ctx context.Context, report *Report) error {
	raw, err := os.ReadFile(s.ModDump)
	if errors.Is(err, fs.ErrNotExist) {
		s.logf("%s: no dump at %s (client mod not installed, or the game not yet started)", ModSourceKey, s.ModDump)
		return nil
	}
	if err != nil {
		return err
	}
	dump, err := mod.Parse(raw)
	if err != nil {
		return err
	}
	// A dump of another account - a second login on the same client - must
	// not be filed under this one.
	if dump.AccountID != s.AccountID {
		return fmt.Errorf("the dump is of account %d, not %d; skipped", dump.AccountID, s.AccountID)
	}

	if s.DB != nil {
		last, err := s.DB.LatestSnapshot(ctx, modSource, modEndpoint)
		switch {
		case err == nil && !dump.CapturedAt.After(last.RequestedAt):
			report.Skipped = append(report.Skipped, ModSourceKey)
			s.logf("%s: unchanged since %s, skipping", ModSourceKey, last.RequestedAt.Format("2006-01-02 15:04 MST"))
			return nil
		case err != nil && !errors.Is(err, store.ErrNotFound):
			return err
		}
	}
	if s.DryRun {
		return s.reportIntent(ModSourceKey, report)
	}

	// requested_at is when the client wrote the dump: that is when the data
	// was observed, which is what every age downstream is measured from.
	id, err := s.DB.PutSnapshot(ctx, store.Snapshot{
		Source: modSource, Endpoint: modEndpoint,
		RequestedAt: dump.CapturedAt, HTTPStatus: 200, Raw: raw,
	})
	if err != nil {
		return err
	}
	garage := storeModGarage(id, dump)
	if err := s.DB.PutModGarage(ctx, garage); err != nil {
		return err
	}

	report.Synced = append(report.Synced, ModSourceKey)
	report.Counts[ModSourceKey+":vehicles"] = len(garage.Vehicles)
	if n := len(dump.Errors); n > 0 {
		report.Caveats = append(report.Caveats, fmt.Sprintf(
			"%s: the client mod could not read %d part(s) - a game update may have renamed them; first: %s",
			ModSourceKey, n, oneLine(dump.Errors[0])))
	}
	s.logf("%s: %d vehicles, captured %s by mod %s on game %s", ModSourceKey, len(garage.Vehicles),
		dump.CapturedAt.Format("2006-01-02 15:04 MST"), dump.ModVersion, dump.GameVersion)
	return nil
}

// storeModGarage converts a parsed dump into the store's rows.
func storeModGarage(snapshotID int64, d mod.Dump) store.ModGarage {
	g := store.ModGarage{Account: store.ModAccount{
		SnapshotID:  snapshotID,
		CapturedAt:  d.CapturedAt,
		GameVersion: d.GameVersion,
		ModVersion:  d.ModVersion,
		Credits:     d.Resources.Credits,
		Gold:        d.Resources.Gold,
		Bonds:       d.Resources.Bonds,
		FreeXP:      d.Resources.FreeXP,
		Premium:     d.Premium.Premium,
		PremiumType: d.Premium.PremiumType,
		WotPlus:     d.Premium.WotPlus,
		Errors:      d.Errors,
	}, Unlocked: d.Unlocked}
	if d.Premium.ExpiresAt != nil {
		g.Account.PremiumExpiresAt = *d.Premium.ExpiresAt
	}

	for _, v := range d.Vehicles {
		sv := store.ModVehicle{
			TankID: v.TankID, Name: v.Name, UserName: v.UserName, Tier: v.Tier,
			XP: v.XP, Elite: v.Elite, Rented: !v.Owned(),
		}
		if v.Marks != nil {
			avg := v.Marks.MovingAvgDamage
			sv.MovingAvgDamage = &avg
			g.Marks = append(g.Marks, store.MarkProgress{TankID: v.TankID, Marks: v.Marks.Marks, Percent: v.Marks.Percent})
		}
		g.Vehicles = append(g.Vehicles, sv)

		g.Loadout = append(g.Loadout, loadoutItems(v.TankID, "equipment", v.Equipment)...)
		g.Loadout = append(g.Loadout, loadoutItems(v.TankID, "consumable", v.Consumables)...)
		g.Loadout = append(g.Loadout, loadoutItems(v.TankID, "directive", v.Directives)...)
		for i, sh := range v.Shells {
			if sh == nil {
				continue
			}
			g.Loadout = append(g.Loadout, store.LoadoutSlot{
				TankID: v.TankID, Kind: "shell", Index: i, ItemID: sh.ID, ItemName: sh.Name,
				UserName: sh.UserName, ShellKind: sh.Kind, Count: sh.Count,
			})
		}
		for _, seat := range v.Crew {
			if seat == nil {
				continue
			}
			c := store.CrewSeat{TankID: v.TankID, Slot: seat.Slot, BonusSkills: seat.BonusSkills}
			if seat.Role != nil {
				c.Role = *seat.Role
			}
			for _, sk := range seat.Skills {
				c.Skills = append(c.Skills, store.CrewSkill{Name: sk.Name, Level: sk.Level})
			}
			g.Crew = append(g.Crew, c)
		}
	}
	return g
}

// loadoutItems keeps each item's slot index, so an empty first slot is still
// visibly the first.
func loadoutItems(tankID int, kind string, items []*mod.Item) []store.LoadoutSlot {
	var out []store.LoadoutSlot
	for i, it := range items {
		if it == nil {
			continue
		}
		out = append(out, store.LoadoutSlot{
			TankID: tankID, Kind: kind, Index: i, ItemID: it.ID, ItemName: it.Name, UserName: it.UserName,
		})
	}
	return out
}
