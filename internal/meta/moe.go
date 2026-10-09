// Package meta fetches server-wide reference data at question time (docs/spec.md
// section 3.5). Nothing here is stored: the data is about the server, not the
// account, changes on Wargaming's schedule, and is only honest when quoted
// with the page's own date.
package meta

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// MoE is one vehicle's mark thresholds.
//
// Thresholds are in combined damage per battle, the figure marks are earned on.
// Change30d is how much each threshold moved over the last 30 days, which says
// whether a target is getting harder.
type MoE struct {
	TankID     int
	Name       string
	Tier       int
	Thresholds map[int]int // 65, 85, 95, 100 -> combined damage
	Change30d  map[int]int
}

// MoETable is a fetched table with its provenance.
type MoETable struct {
	URL       string
	FetchedAt time.Time
	// Updated is the page's own timestamp: when tomato.gg last recomputed it.
	Updated time.Time
	ByTank  map[int]MoE
}

// MoE downloads and parses the MoE table for a server ("eu", "na", "asia").
func (t *Tomato) MoE(ctx context.Context, server string) (MoETable, error) {
	p, err := t.page(ctx, "/moe/"+server, "65")
	if err != nil {
		return MoETable{}, err
	}
	table, err := moeTable(p.Records)
	if err != nil {
		return MoETable{}, fmt.Errorf("%s: %w", p.URL, err)
	}
	table.URL, table.FetchedAt, table.Updated = p.URL, p.FetchedAt, p.Updated
	return table, nil
}

// ParseMoE extracts the table from the page's embedded data, as measured
// 2026-09-18:
//
//	{65:2198,85:3311,95:4242,100:5026,id:6769,"7diff65":123,…,name:"Tesák",tier:10,…}
func ParseMoE(page []byte) (MoETable, error) {
	updated, records, err := ParseTomato(page, "65")
	if err != nil {
		return MoETable{}, err
	}
	table, err := moeTable(records)
	if err != nil {
		return MoETable{}, err
	}
	table.Updated = updated
	return table, nil
}

func moeTable(records []Record) (MoETable, error) {
	table := MoETable{ByTank: map[int]MoE{}}
	for _, r := range records {
		id, ok := r.Int("id")
		if !ok {
			continue
		}
		moe := MoE{TankID: id, Name: r.String("name"), Thresholds: map[int]int{}, Change30d: map[int]int{}}
		moe.Tier, _ = r.Int("tier")
		for _, mark := range []int{65, 85, 95, 100} {
			key := strconv.Itoa(mark)
			if v, ok := r.Int(key); ok {
				moe.Thresholds[mark] = v
			}
			if v, ok := r.Int("30diff" + key); ok {
				moe.Change30d[mark] = v
			}
		}
		table.ByTank[id] = moe
	}
	if len(table.ByTank) == 0 {
		return table, fmt.Errorf("tomato.gg: no MoE records in the page; %w", ErrFormat)
	}
	return table, nil
}
