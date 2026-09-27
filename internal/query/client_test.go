package query

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/testseed"
)

func ptr[T any](v T) *T { return &v }

// seedWithDump is the seed account plus a client mod dump written at
// capturedAt. The seed's clock is 2026-09-18 12:00 and its last battle 09:00,
// so a dump from 10:00 is current and one from 08:00 is behind.
func seedWithDump(t *testing.T, capturedAt time.Time, edit func(*store.ModGarage)) *Service {
	t.Helper()
	s := seed(t)
	ctx := context.Background()
	id, err := s.DB.PutSnapshot(ctx, store.Snapshot{
		Source: "mod", Endpoint: "garage", RequestedAt: capturedAt, HTTPStatus: 200, Raw: []byte(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	g := store.ModGarage{
		Account: store.ModAccount{
			SnapshotID: id, CapturedAt: capturedAt, GameVersion: "2.4.0.1", ModVersion: "0.3.0",
			Premium: ptr(true), PremiumType: ptr(2), WotPlus: ptr(true),
			PremiumExpiresAt: testseed.Now.Add(10 * 24 * time.Hour),
		},
		Vehicles: []store.ModVehicle{
			{TankID: idBlesk, Name: "czech:Cz11_Blesk", UserName: "Vz. 64 Blesk", Tier: 8, XP: ptr(170000)},
			{TankID: idAMX1390, Name: "france:F87_AMX_13_90", UserName: "AMX 13 90", Tier: 9, XP: ptr(5000),
				Elite: ptr(true), MovingAvgDamage: ptr(2100)},
			{TankID: idLeox, Name: "france:F137_LEOX", UserName: "Leox", Tier: 9, Rented: true},
		},
		Marks: []store.MarkProgress{{TankID: idAMX1390, Marks: 2, Percent: 88.5}},
		Loadout: []store.LoadoutSlot{
			{TankID: idAMX1390, Kind: "equipment", Index: 0, ItemName: "improvedVentilation_tier3", UserName: "Improved Ventilation"},
			{TankID: idAMX1390, Kind: "shell", Index: 0, ItemName: "_90mm_AP", UserName: "AP", ShellKind: "ARMOR_PIERCING", Count: ptr(40)},
		},
		Crew: []store.CrewSeat{{TankID: idAMX1390, Slot: 0, Role: "commander",
			Skills: []store.CrewSkill{{Name: "commander_sixthSense", Level: 100}}}},
		// The Šelma is researched and has never been played; the Kranvagn,
		// which the overlay lists, is not researched in this dump.
		Unlocked: []int{idBlesk, idAMX1390, idSelma},
	}
	if edit != nil {
		edit(&g)
	}
	if err := s.DB.PutModGarage(ctx, g); err != nil {
		t.Fatal(err)
	}
	return s
}

var dumpAt = testseed.Now.Add(-2 * time.Hour)

func hasSource(env Envelope, name string) bool {
	for _, s := range env.Meta.Sources {
		if s.Name == name {
			return true
		}
	}
	return false
}

func hasCaveat(env Envelope, part string) bool {
	for _, c := range env.Meta.Caveats {
		if strings.Contains(c, part) {
			return true
		}
	}
	return false
}

func TestResourcesTakePremiumFromTheClient(t *testing.T) {
	env, err := seedWithDump(t, dumpAt, nil).Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := env.Data.(Resources).Premium
	if p.Source != "client mod" || !*p.PremiumAccount || !*p.WoTPlus || p.ExpiresAt == nil {
		t.Errorf("premium = %+v", p)
	}
	if !hasSource(env, srcMod) {
		t.Error("mod:garage is not among the sources")
	}
}

// A dump can be days old: a Premium Account that has run out since is not
// active, whatever the dump said.
func TestExpiredPremiumIsNotActive(t *testing.T) {
	s := seedWithDump(t, dumpAt, func(g *store.ModGarage) {
		g.Account.PremiumExpiresAt = testseed.Now.Add(-time.Hour)
	})
	env, err := s.Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p := env.Data.(Resources).Premium; *p.PremiumAccount {
		t.Errorf("premium = %+v, want inactive after expiry", p)
	}
}

func TestGarageLeavesOutRentals(t *testing.T) {
	env, err := seedWithDump(t, dumpAt, nil).Garage(context.Background(), GarageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tank := range env.Data.(Garage).Tanks {
		if tank.TankID == idLeox {
			t.Error("the rented Leox is listed as owned")
		}
	}
	if !hasCaveat(env, "left out as rentals") {
		t.Errorf("no rental caveat: %v", env.Meta.Caveats)
	}
}

func TestTankCarriesTheClientBlock(t *testing.T) {
	env, err := seedWithDump(t, dumpAt, nil).Tank(context.Background(), "AMX 13 90")
	if err != nil {
		t.Fatal(err)
	}
	c := env.Data.(Tank).Client
	if c == nil {
		t.Fatal("no client block")
	}
	if *c.XP != 5000 || !*c.Elite || !*c.Researched || !c.CapturedAt.Equal(dumpAt) {
		t.Errorf("client = %+v", c)
	}
	if c.Marks == nil || c.Marks.Marks != 2 || c.Marks.Percent != 88.5 || *c.Marks.MovingAvgDamage != 2100 {
		t.Errorf("marks = %+v", c.Marks)
	}
	if len(c.Equipment) != 1 || c.Equipment[0].Name != "improvedVentilation_tier3" {
		t.Errorf("equipment = %+v", c.Equipment)
	}
	if len(c.Shells) != 1 || *c.Shells[0].Count != 40 || c.Shells[0].Kind != "ARMOR_PIERCING" {
		t.Errorf("shells = %+v", c.Shells)
	}
	if len(c.Crew) != 1 || c.Crew[0].Skills[0].Name != "commander_sixthSense" {
		t.Errorf("crew = %+v", c.Crew)
	}
}

// Goal XP comes from the game; the overlay's hand-kept figure is the backup.
func TestGoalXPComesFromTheClient(t *testing.T) {
	env, err := seedWithDump(t, dumpAt, nil).Tank(context.Background(), "Selma")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, g := range env.Data.(Tank).Overlay.Goals {
		if g.TargetID != idSelma {
			continue
		}
		found = true
		if *g.XPBanked != 170000 || g.XPBankedSource != "client mod" || *g.XPRemaining != 15490 {
			t.Errorf("Šelma goal = banked %v (%s), remaining %v; want 170,000 from the client, 15,490 left",
				*g.XPBanked, g.XPBankedSource, *g.XPRemaining)
		}
	}
	if !found {
		t.Fatal("no Šelma goal")
	}
	if !hasCaveat(env, "own XP in the game client") {
		t.Errorf("goal caveat does not name the client: %v", env.Meta.Caveats)
	}
	if hasCaveat(env, "hand-maintained in the overlay") {
		t.Error("the overlay caveat is still given for a figure from the client")
	}
}

func TestCandidatesKnowResearchFromTheClient(t *testing.T) {
	env, err := seedWithDump(t, dumpAt, nil).Candidates(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int]Candidate{}
	for _, c := range env.Data.(Candidates).Candidates {
		byID[c.TankID] = c
	}
	if c := byID[idSelma]; c.Origin != OriginResearched || !*c.Researched || c.XPCost != 0 {
		t.Errorf("Šelma = %+v, want researched, not bought, costing no XP", c)
	}
	if _, ok := byID[idKranvagn]; ok {
		t.Error("the Kranvagn is listed from the overlay although the client says it is not researched")
	}
	if c, ok := byID[testseed.AMX13105]; !ok || c.Researched == nil || *c.Researched {
		t.Errorf("AMX 13 105 = %+v, want researched: false", c)
	}
}

func TestADumpBehindTheLastBattleSaysSo(t *testing.T) {
	env, err := seedWithDump(t, testseed.Now.Add(-4*time.Hour), nil).Tank(context.Background(), "AMX 13 90")
	if err != nil {
		t.Fatal(err)
	}
	if !hasCaveat(env, "battles were played after it") {
		t.Errorf("caveats = %v", env.Meta.Caveats)
	}
}

func TestMoEShowsTheClientsMarks(t *testing.T) {
	table := meta.MoETable{
		URL: "https://tomato.gg/moe/eu", Updated: testseed.Now.Add(-2 * time.Hour),
		ByTank: map[int]meta.MoE{idAMX1390: {TankID: idAMX1390, Tier: 9,
			Thresholds: map[int]int{65: 1891, 85: 2862, 95: 3696, 100: 4300}}},
	}
	env, err := seedWithDump(t, dumpAt, nil).MoE(context.Background(), "AMX 13 90", &table)
	if err != nil {
		t.Fatal(err)
	}
	m := env.Data.(Marks)
	if m.Client == nil || m.Client.Percent != 88.5 || m.ClientCapturedAt == nil {
		t.Errorf("client marks = %+v", m.Client)
	}
	if !hasCaveat(env, "actual marks and progress") || hasCaveat(env, "Ask the player for their actual mark percentage") {
		t.Errorf("caveats = %v", env.Meta.Caveats)
	}
}

// The client lists everything ever researched, starters included. Only tier
// VIII and up is offered, the rest counted; a tank sold earlier stays, marked.
func TestResearchedListIsCurrentPlayOnly(t *testing.T) {
	const low, sold, step = 1001, 2001, 3001
	s := seedWithDump(t, dumpAt, func(g *store.ModGarage) {
		g.Unlocked = append(g.Unlocked, low, sold, step)
	})
	ctx := context.Background()
	if _, err := s.DB.UpsertVehicles(ctx, []store.Vehicle{
		{TankID: low, Name: "Low Tank", Tier: 5, Type: "lightTank", Nation: "czech", SyncedAt: testseed.Now},
		{TankID: sold, Name: "Sold Tank", Tier: 10, Type: "mediumTank", Nation: "czech", PriceCredit: 6100000, SyncedAt: testseed.Now},
		{TankID: step, Name: "Step Tank", Tier: 9, Type: "mediumTank", Nation: "czech", PriceCredit: 3500000, SyncedAt: testseed.Now},
	}); err != nil {
		t.Fatal(err)
	}
	// The step tank leads to the sold tier X, which is researched: it was
	// passed through on the way up.
	if _, err := s.DB.ReplaceEdgesFromSource(ctx, "test", []store.Edge{{From: step, To: sold, XPCost: 180000}}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.DB.LatestSnapshot(ctx, "wg", "tanks/stats")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.PutTankStats(ctx, []store.TankStats{
		{SnapshotID: snap.ID, TankID: sold, Mode: "random", Battles: 37, Wins: 20},
		{SnapshotID: snap.ID, TankID: step, Mode: "random", Battles: 90, Wins: 45},
	}); err != nil {
		t.Fatal(err)
	}

	env, err := s.Candidates(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int]Candidate{}
	for _, c := range env.Data.(Candidates).Candidates {
		byID[c.TankID] = c
	}
	if _, ok := byID[low]; ok {
		t.Error("a tier V researched vehicle is listed")
	}
	if _, ok := byID[step]; ok {
		t.Error("a sold tank whose next step is researched is listed")
	}
	if !hasCaveat(env, "1 below tier 8, and 1 sold after their next step was researched") {
		t.Errorf("caveats = %v", env.Meta.Caveats)
	}
	if c, ok := byID[sold]; !ok || c.Origin != OriginResearched || c.PlayedBefore != 37 {
		t.Errorf("sold tank = %+v, want researched, not bought, played before 37 times", c)
	}
}

// TestNotEliteCarriesItsMeaning: runs 8 and 9 read elite: false as "modules
// are locked". The query now says what false does and does not show, where
// the figure is.
func TestNotEliteCarriesItsMeaning(t *testing.T) {
	s := seedWithDump(t, testseed.Now.Add(-time.Hour), func(g *store.ModGarage) {
		g.Vehicles[0].Elite = ptr(false) // the Blesk
	})
	env, err := s.Tank(context.Background(), "blesk")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(env.Meta.Caveats, NotEliteCaveat) {
		t.Errorf("caveats = %q, want the not-elite caveat", env.Meta.Caveats)
	}
	env, err = s.Tank(context.Background(), "AMX 13 90") // elite: true
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(env.Meta.Caveats, NotEliteCaveat) {
		t.Error("an elite tank carries the not-elite caveat")
	}
}
