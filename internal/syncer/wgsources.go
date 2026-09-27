package syncer

import (
	"context"
	"fmt"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// syncAccountInfo refreshes account-level state: resources, the garage and
// Personal Reserves.
func (s *Syncer) syncAccountInfo(ctx context.Context, report *Report) error {
	const (
		source   = "wg"
		endpoint = "account/info"
	)
	key := store.SourceKey(source, endpoint)

	due, err := s.due(ctx, source, endpoint, key, report)
	if err != nil || !due {
		return err
	}
	if s.DryRun {
		return s.reportIntent(key, report)
	}

	result, fetchErr := s.WG.AccountInfo(ctx, s.AccountID, s.AccessToken)
	snapshotID, err := s.record(ctx, source, endpoint, result.Result)
	if err != nil {
		return err
	}
	if fetchErr != nil {
		return fetchErr
	}

	account := result.Account

	// A request without a usable token still succeeds, just without the private
	// block. Saying so matters: an answer that quietly reported zero credits
	// would be worse than one admitting it could not look.
	if !account.HasPrivate {
		report.Caveats = append(report.Caveats,
			key+": private data unavailable (no valid access token); credits, free XP and garage are missing")
	}

	boosters := make([]store.Booster, 0, len(account.Boosters))
	for _, b := range account.Boosters {
		boosters = append(boosters, store.Booster{
			Kind: b.Kind, Count: b.Count, State: b.State, ExpiresAt: b.ExpiresAt,
		})
	}

	if err := s.DB.PutAccountState(ctx, store.AccountState{
		SnapshotID:       snapshotID,
		ObservedAt:       result.Result.RequestedAt,
		Credits:          account.Credits,
		Gold:             account.Gold,
		Bonds:            account.Bonds,
		FreeXP:           account.FreeXP,
		IsPremium:        account.IsPremium,
		PremiumExpiresAt: account.PremiumExpiresAt,
		GlobalRating:     account.GlobalRating,
		LastBattleTime:   account.LastBattleTime,
		BattleLifeTime:   account.BattleLifeTime,
	}, account.Garage, boosters); err != nil {
		return err
	}

	if len(account.PersonalMissions) > 0 {
		n, err := s.DB.PutMissionStatuses(ctx, snapshotID, account.PersonalMissions)
		if err != nil {
			return err
		}
		report.Counts[key+":personal_missions"] = n
	}

	report.Counts[key+":garage"] = len(account.Garage)
	report.Counts[key+":boosters"] = len(boosters)
	report.Synced = append(report.Synced, key)
	s.logf("%s: %d vehicles in the garage, %d reserve type(s)", key, len(account.Garage), len(boosters))
	return nil
}

// syncTankStats refreshes per-tank cumulative statistics and mastery badges.
//
// Mastery lives on a different endpoint, so the two are fetched together and
// merged: a mastery badge with no statistics beside it cannot be interpreted.
func (s *Syncer) syncTankStats(ctx context.Context, report *Report) error {
	const (
		source   = "wg"
		endpoint = "tanks/stats"
	)
	key := store.SourceKey(source, endpoint)

	due, err := s.due(ctx, source, endpoint, key, report)
	if err != nil || !due {
		return err
	}
	if s.DryRun {
		return s.reportIntent(key, report)
	}

	statsResult, fetchErr := s.WG.TankStats(ctx, s.AccountID, s.AccessToken)
	snapshotID, err := s.record(ctx, source, endpoint, statsResult.Result)
	if err != nil {
		return err
	}
	if fetchErr != nil {
		return fetchErr
	}

	// Mastery is a second request. Losing it is a caveat rather than a failure
	// of the whole source, because the statistics are the substance.
	mastery := map[int]int{}
	masteryResult, masteryErr := s.WG.AccountTanks(ctx, s.AccountID, s.AccessToken)
	if _, err := s.record(ctx, source, "account/tanks", masteryResult.Result); err != nil {
		return err
	}
	if masteryErr != nil {
		report.Caveats = append(report.Caveats,
			"wg:account/tanks: "+oneLine(masteryErr.Error())+"; mastery badges unavailable")
	} else {
		for _, m := range masteryResult.Mastery {
			mastery[m.TankID] = m.MarkOfMastery
		}
		report.Counts["wg:account/tanks:mastery"] = len(mastery)
	}

	rows := make([]store.TankStats, 0, len(statsResult.Stats))
	for _, st := range statsResult.Stats {
		rows = append(rows, storeTankStats(snapshotID, st, mastery[st.TankID]))
	}

	n, err := s.DB.PutTankStats(ctx, rows)
	if err != nil {
		return err
	}

	report.Counts[key+":rows"] = n
	report.Synced = append(report.Synced, key)
	s.logf("%s: %d rows across modes", key, n)
	return nil
}

// syncAchievementDefs refreshes the achievement definitions.
//
// The counts alone are unreadable, since the API reports codes like "medalKay".
// The definitions carry the name and the earning condition, which is what makes
// a question like "which medals am I close to" answerable rather than just
// enumerable.
func (s *Syncer) syncAchievementDefs(ctx context.Context, report *Report) error {
	const (
		source   = "wg"
		endpoint = "encyclopedia/achievements"
	)
	key := store.SourceKey(source, endpoint)

	due, err := s.due(ctx, source, endpoint, key, report)
	if err != nil || !due {
		return err
	}
	if s.DryRun {
		return s.reportIntent(key, report)
	}

	result, fetchErr := s.WG.EncyclopediaAchievements(ctx, "")
	if _, err := s.record(ctx, source, endpoint, result.Result); err != nil {
		return err
	}
	if fetchErr != nil {
		return fetchErr
	}

	defs := make([]store.AchievementDef, 0, len(result.Defs))
	syncedAt := s.now()
	for _, d := range result.Defs {
		defs = append(defs, store.AchievementDef{
			Code: d.Code, Name: d.Name, Description: d.Description, Condition: d.Condition,
			HeroInfo: d.HeroInfo, Section: d.Section, SectionOrder: d.SectionOrder,
			Order: d.Order, Image: d.Image, SyncedAt: syncedAt,
		})
	}

	n, err := s.DB.UpsertAchievementDefs(ctx, defs)
	if err != nil {
		return err
	}

	report.Counts[key+":defs"] = n
	report.Synced = append(report.Synced, key)
	s.logf("%s: %d definitions", key, n)
	return nil
}

// syncAccountAchievements refreshes account-wide achievement counts.
func (s *Syncer) syncAccountAchievements(ctx context.Context, report *Report) error {
	const (
		source   = "wg"
		endpoint = "account/achievements"
	)
	key := store.SourceKey(source, endpoint)

	due, err := s.due(ctx, source, endpoint, key, report)
	if err != nil || !due {
		return err
	}
	if s.DryRun {
		return s.reportIntent(key, report)
	}

	result, fetchErr := s.WG.AccountAchievements(ctx, s.AccountID)
	snapshotID, err := s.record(ctx, source, endpoint, result.Result)
	if err != nil {
		return err
	}
	if fetchErr != nil {
		return fetchErr
	}

	counts := make([]store.AchievementCount, 0, len(result.Counts))
	for _, c := range result.Counts {
		counts = append(counts, store.AchievementCount{Kind: c.Kind, Code: c.Code, Count: c.Count})
	}

	n, err := s.DB.PutAccountAchievements(ctx, snapshotID, counts)
	if err != nil {
		return err
	}

	report.Counts[key+":counts"] = n
	report.Synced = append(report.Synced, key)
	s.logf("%s: %d counts", key, n)
	return nil
}

// syncTankAchievements refreshes per-tank achievement counts.
func (s *Syncer) syncTankAchievements(ctx context.Context, report *Report) error {
	const (
		source   = "wg"
		endpoint = "tanks/achievements"
	)
	key := store.SourceKey(source, endpoint)

	due, err := s.due(ctx, source, endpoint, key, report)
	if err != nil || !due {
		return err
	}
	if s.DryRun {
		return s.reportIntent(key, report)
	}

	result, fetchErr := s.WG.TankAchievements(ctx, s.AccountID, s.AccessToken)
	snapshotID, err := s.record(ctx, source, endpoint, result.Result)
	if err != nil {
		return err
	}
	if fetchErr != nil {
		return fetchErr
	}

	var counts []store.AchievementCount
	for _, tank := range result.Tanks {
		for _, c := range tank.Counts {
			counts = append(counts, store.AchievementCount{
				TankID: tank.TankID, Kind: c.Kind, Code: c.Code, Count: c.Count,
			})
		}
	}

	n, err := s.DB.PutTankAchievements(ctx, snapshotID, counts)
	if err != nil {
		return err
	}

	report.Counts[key+":counts"] = n
	report.Counts[key+":tanks"] = len(result.Tanks)
	report.Synced = append(report.Synced, key)
	s.logf("%s: %d counts across %d tanks", key, n, len(result.Tanks))
	return nil
}

// due reports whether a source should be fetched, recording a skip if not.
func (s *Syncer) due(ctx context.Context, source, endpoint, key string, report *Report) (bool, error) {
	if s.Force {
		return true, nil
	}
	fresh, err := s.isFresh(ctx, source, endpoint, key)
	if err != nil {
		return false, err
	}
	if fresh {
		report.Skipped = append(report.Skipped, key)
		s.logf("%s: still fresh, skipping", key)
		return false, nil
	}
	return true, nil
}

// reportIntent records what a dry run would have fetched.
func (s *Syncer) reportIntent(key string, report *Report) error {
	report.Synced = append(report.Synced, key)
	s.logf("%s: would fetch", key)
	return nil
}

// record stores a response and returns its snapshot id.
func (s *Syncer) record(ctx context.Context, source, endpoint string, result wg.Result) (int64, error) {
	return s.DB.PutSnapshot(ctx, snapshotFrom(source, endpoint, result))
}

// oneLine flattens an error message, so one caveat stays one line in the
// newline-separated sync run notes.
func oneLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " ")
}

// storeTankStats converts one parsed row into the store's shape.
func storeTankStats(snapshotID int64, st wg.TankStats, mastery int) store.TankStats {
	return store.TankStats{
		SnapshotID:             snapshotID,
		TankID:                 st.TankID,
		Mode:                   st.Mode,
		Battles:                st.Battles,
		Wins:                   st.Wins,
		Losses:                 st.Losses,
		Draws:                  st.Draws,
		Survived:               st.SurvivedBattles,
		DamageDealt:            st.DamageDealt,
		DamageReceived:         st.DamageReceived,
		Frags:                  st.Frags,
		Spotted:                st.Spotted,
		XP:                     st.XP,
		BattleAvgXP:            st.BattleAvgXP,
		HitsPercents:           st.HitsPercents,
		AvgDamageAssisted:      st.AvgDamageAssisted,
		AvgDamageAssistedRadio: st.AvgDamageAssistedRadio,
		AvgDamageAssistedTrack: st.AvgDamageAssistedTrack,
		AvgDamageAssistedStun:  st.AvgDamageAssistedStun,
		AvgDamageBlocked:       st.AvgDamageBlocked,
		TankingFactor:          st.TankingFactor,
		MarkOfMastery:          mastery,
		CapturePoints:          st.CapturePoints,
		DroppedCapturePoints:   st.DroppedCapturePoints,
		RadioAssistedDamage:    st.RadioAssistedDamage,
		TrackAssistedDamage:    st.TrackAssistedDamage,
		StunAssistedDamage:     st.StunAssistedDamage,
	}
}

// ReparseTankStats brings tank_stats rows written by an older parser up to
// date from the raw bodies already stored, so a field added to the parser
// reaches back through history without a single request.
//
// It is safe to run every sync: with nothing stale it is one query.
func (s *Syncer) ReparseTankStats(ctx context.Context) (int, error) {
	if s.DB == nil {
		return 0, nil
	}
	ids, err := s.DB.TankStatsSnapshotsToReparse(ctx)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, id := range ids {
		snap, err := s.DB.Snapshot(ctx, id)
		if err != nil {
			return updated, fmt.Errorf("re-parsing snapshot %d: %w", id, err)
		}
		parsed, err := wg.ParseTankStatsBody(snap.Raw, s.AccountID)
		if err != nil {
			return updated, fmt.Errorf("re-parsing snapshot %d: %w", id, err)
		}
		rows := make([]store.TankStats, 0, len(parsed))
		for _, st := range parsed {
			rows = append(rows, storeTankStats(id, st, 0)) // mastery is not touched
		}
		n, err := s.DB.UpdateTankStatsTotals(ctx, rows)
		if err != nil {
			return updated, err
		}
		updated += n
	}
	if updated > 0 {
		s.logf("tank_stats: re-parsed %d row(s) from %d stored snapshot(s)", updated, len(ids))
	}
	return updated, nil
}

// syncPersonalMissions refreshes the personal-mission encyclopedia: names,
// classes, tiers and conditions for the statuses account/info reports.
func (s *Syncer) syncPersonalMissions(ctx context.Context, report *Report) error {
	const (
		source   = "wg"
		endpoint = "encyclopedia/personalmissions"
	)
	key := store.SourceKey(source, endpoint)

	due, err := s.due(ctx, source, endpoint, key, report)
	if err != nil || !due {
		return err
	}
	if s.DryRun {
		return s.reportIntent(key, report)
	}

	result, fetchErr := s.WG.PersonalMissions(ctx)
	if _, err := s.record(ctx, source, endpoint, result.Result); err != nil {
		return err
	}
	if fetchErr != nil {
		return fetchErr
	}

	rows := make([]store.PersonalMission, 0, len(result.Missions))
	for _, m := range result.Missions {
		rows = append(rows, store.PersonalMission{
			MissionID: m.MissionID, CampaignID: m.CampaignID, Campaign: m.Campaign,
			OperationID: m.OperationID, Operation: m.Operation, SetID: m.SetID,
			Name: m.Name, Class: m.Class, MinTier: m.MinTier, MaxTier: m.MaxTier,
			Primary: m.Primary, Secondary: m.Secondary,
		})
	}
	n, err := s.DB.ReplacePersonalMissions(ctx, rows, s.now())
	if err != nil {
		return err
	}
	report.Counts[key+":missions"] = n
	report.Synced = append(report.Synced, key)
	s.logf("%s: %d missions", key, n)
	return nil
}
