package meta

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// OnslaughtTank is one vehicle's figures across a season of Onslaught on the
// server. Unlike the random-battle table there is no players' win rate, so
// nothing separates the tank from the skill of those who play it.
type OnslaughtTank struct {
	TankID  int
	Name    string
	Tier    int
	Class   string
	Premium bool

	Battles                        int
	WinRate                        float64
	Damage                         int
	Frags, Spots                   float64
	SpottingAssist, TrackingAssist int
	DamageBlocked                  int
	Survival                       float64
	BaseXP                         int
}

// RatingBucket is the number of players whose Onslaught rating falls in
// [Rating, Rating+100).
type RatingBucket struct {
	Rating  int
	Players int
}

// OnslaughtTable is a fetched season with its provenance.
type OnslaughtTable struct {
	URL       string
	FetchedAt time.Time
	Updated   time.Time
	Season    string
	ByTank    map[int]OnslaughtTank
	Ratings   []RatingBucket // ascending
}

var seasonRe = regexp.MustCompile(`season:"([^"]+)"`)

// Onslaught fetches the current season's page for a server.
func (t *Tomato) Onslaught(ctx context.Context, server string) (OnslaughtTable, error) {
	path := "/onslaught/" + strings.ToUpper(server)
	p, err := t.page(ctx, path, "tank_id")
	if err != nil {
		return OnslaughtTable{}, err
	}
	table := OnslaughtTable{URL: p.URL, FetchedAt: p.FetchedAt, Updated: p.Updated, ByTank: map[int]OnslaughtTank{}}
	for _, r := range p.Records {
		if o, ok := onslaughtTank(r); ok {
			table.ByTank[o.TankID] = o
		}
	}
	if len(table.ByTank) == 0 {
		return OnslaughtTable{}, fmt.Errorf("%s: no tank rows; %w", p.URL, ErrFormat)
	}

	// The same page: the rating distribution and the season.
	if m := seasonRe.FindSubmatch(p.Body); m != nil {
		table.Season = string(m[1])
	} else {
		return OnslaughtTable{}, fmt.Errorf("%s: no season named; %w", p.URL, ErrFormat)
	}
	if _, buckets, err := ParseTomato(p.Body, "rating_bucket"); err == nil {
		seen := map[int]bool{}
		for _, b := range buckets {
			rating, ok1 := b.Int("rating_bucket")
			players, ok2 := b.Int("player_count")
			if ok1 && ok2 && !seen[rating] {
				seen[rating] = true
				table.Ratings = append(table.Ratings, RatingBucket{Rating: rating, Players: players})
			}
		}
		sort.Slice(table.Ratings, func(i, j int) bool { return table.Ratings[i].Rating < table.Ratings[j].Rating })
	}
	return table, nil
}

func onslaughtTank(r Record) (OnslaughtTank, bool) {
	id, okID := r.Int("tank_id")
	battles, okB := r.Int("battles")
	wr, okWR := r.Float("winrate")
	if !okID || !okB || !okWR {
		return OnslaughtTank{}, false
	}
	o := OnslaughtTank{TankID: id, Name: r.String("name"), Class: r.String("class"), Battles: battles, WinRate: wr}
	o.Tier, _ = r.Int("tier")
	o.Premium, _ = r.Bool("isPrem")
	o.Damage, _ = r.Int("damage")
	o.Frags, _ = r.Float("frags")
	o.Spots, _ = r.Float("spots")
	o.SpottingAssist, _ = r.Int("spotting_assist")
	o.TrackingAssist, _ = r.Int("tracking_assist")
	o.DamageBlocked, _ = r.Int("damage_blocked")
	o.Survival, _ = r.Float("survival")
	o.BaseXP, _ = r.Int("base_xp")
	return o, true
}
