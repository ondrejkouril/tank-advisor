// Package meta fetches server-wide reference data at question time (docs/spec.md
// section 3.5). Nothing here is stored: the data is about the server, not the
// account, changes on Wargaming's schedule, and is only honest when quoted
// with the page's own date.
package meta

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/useragent"
)

// MoEURL is tomato.gg's Marks of Excellence table for one server.
const MoEURL = "https://tomato.gg/moe/%s"

// maxPage bounds the download; the real page is ~650 KB.
const maxPage = 16 << 20

// Server maps a wotctx realm to tomato.gg's server segment.
func Server(realm string) string {
	switch realm {
	case "com":
		return "na"
	default:
		return realm
	}
}

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

// MoEClient fetches the table.
type MoEClient struct {
	HTTP *http.Client
	// URL overrides MoEURL, for tests; it is used as given.
	URL string
	Now func() time.Time
}

// FetchMoE downloads and parses the MoE table for a server ("eu", "na", "asia").
func (c *MoEClient) FetchMoE(ctx context.Context, server string) (MoETable, error) {
	url := c.URL
	if url == "" {
		url = fmt.Sprintf(MoEURL, server)
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return MoETable{}, fmt.Errorf("tomato.gg: %w", err)
	}
	req.Header.Set("User-Agent", useragent.String)
	resp, err := client.Do(req)
	if err != nil {
		return MoETable{}, fmt.Errorf("tomato.gg: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return MoETable{}, fmt.Errorf("tomato.gg: HTTP %d from %s", resp.StatusCode, url)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPage))
	if err != nil {
		return MoETable{}, fmt.Errorf("tomato.gg: reading page: %w", err)
	}

	table, err := ParseMoE(raw)
	if err != nil {
		return MoETable{}, err
	}
	table.URL = url
	table.FetchedAt = now().UTC()
	return table, nil
}

var (
	// A record in the page's embedded data, as measured 2026-09-18:
	//   {65:2198,85:3311,95:4242,100:5026,id:6769,"7diff65":123,…,name:"Tesák",tier:10,…}
	recordRe  = regexp.MustCompile(`\{65:(\d+),85:(\d+),95:(\d+),100:(\d+),id:(\d+),([^{}]*)\}`)
	nameRe    = regexp.MustCompile(`(?:^|,)name:"((?:[^"\\]|\\.)*)"`)
	tierRe    = regexp.MustCompile(`(?:^|,)tier:(\d+)`)
	diff30Re  = regexp.MustCompile(`"30diff(65|85|95|100)":(-?\d+)`)
	updatedRe = regexp.MustCompile(`updated:"(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d(?:\.\d+)?[+-]\d\d:\d\d)"`)
)

// ParseMoE extracts the table from the page's embedded data.
//
// This reads a site's internal page data, not an API, so it can break with any
// redesign. It fails loudly - no records, or no update stamp - rather than
// returning a partial table that would be quoted as if complete.
func ParseMoE(page []byte) (MoETable, error) {
	table := MoETable{ByTank: map[int]MoE{}}

	m := updatedRe.FindSubmatch(page)
	if m == nil {
		return table, fmt.Errorf("tomato.gg: no update timestamp in the page; its format has changed")
	}
	updated, err := time.Parse("2006-01-02 15:04:05.999999-07:00", string(m[1]))
	if err != nil {
		return table, fmt.Errorf("tomato.gg: update timestamp %q: %w", m[1], err)
	}
	table.Updated = updated.UTC()

	for _, rec := range recordRe.FindAllSubmatch(page, -1) {
		atoi := func(b []byte) int { n, _ := strconv.Atoi(string(b)); return n }
		moe := MoE{
			TankID: atoi(rec[5]),
			Thresholds: map[int]int{
				65: atoi(rec[1]), 85: atoi(rec[2]), 95: atoi(rec[3]), 100: atoi(rec[4]),
			},
			Change30d: map[int]int{},
		}
		rest := rec[6]
		if n := nameRe.FindSubmatch(rest); n != nil {
			if s, err := strconv.Unquote(`"` + string(n[1]) + `"`); err == nil {
				moe.Name = s
			} else {
				moe.Name = string(n[1])
			}
		}
		if t := tierRe.FindSubmatch(rest); t != nil {
			moe.Tier = atoi(t[1])
		}
		for _, d := range diff30Re.FindAllSubmatch(rest, -1) {
			moe.Change30d[atoi(d[1])] = atoi(d[2])
		}
		table.ByTank[moe.TankID] = moe
	}
	if len(table.ByTank) == 0 {
		return table, fmt.Errorf("tomato.gg: no MoE records in the page; its format has changed")
	}
	return table, nil
}
