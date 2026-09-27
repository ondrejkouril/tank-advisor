// Package xvm fetches WN8 expected values from XVM (docs/spec.md section 3.3).
//
// One unauthenticated GET of a public static file, about 100 KB, refreshed on
// a patch-day TTL. It carries no account data in either direction.
package xvm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/useragent"
)

// DefaultURL is XVM's current expected values for the Wargaming servers.
//
// Not the older wn8-data-exp/json/wn8exp.json, which spec drafts named: that
// file stopped at version 2024-09-12, when XVM split its data between
// Wargaming and Lesta, and it lacks every vehicle released since, including
// Tier XI. Measured 2026-09-18: the wg/ file is version 2026-09-12, 992
// vehicles, regenerated nightly.
const DefaultURL = "https://static.modxvm.com/wn8-data-exp/json/wg/wn8exp.json"

// maxBody bounds the download. The real file is ~100 KB.
const maxBody = 8 << 20

// Expected is one vehicle's expected values.
type Expected struct {
	TankID  int
	Damage  float64
	Frags   float64
	Spot    float64
	Def     float64
	WinRate float64 // a percentage, e.g. 51.4, as XVM publishes it
}

// Result is one fetch, raw body included so it can be stored before parsing.
type Result struct {
	URL         string
	RequestedAt time.Time
	HTTPStatus  int
	Raw         []byte

	// Set by a successful parse.
	Version  string
	Expected []Expected
	// Skipped counts entries dropped for a non-positive value, which would
	// otherwise be a division by zero in the WN8 ratios.
	Skipped int
}

// Client fetches expected values.
type Client struct {
	URL  string
	HTTP *http.Client
	// Now supplies the clock; tests override it.
	Now func() time.Time
}

// New returns a client for DefaultURL.
func New() *Client {
	return &Client{URL: DefaultURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Fetch downloads and parses the expected values. The returned Result carries
// the raw body even when parsing fails, so the caller can record it first.
func (c *Client) Fetch(ctx context.Context) (Result, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	result := Result{URL: c.URL, RequestedAt: now().UTC()}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return result, fmt.Errorf("xvm: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", useragent.String)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return result, fmt.Errorf("xvm: %w", err)
	}
	defer resp.Body.Close()

	result.HTTPStatus = resp.StatusCode
	result.Raw, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return result, fmt.Errorf("xvm: reading body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("xvm: HTTP %d from %s", resp.StatusCode, c.URL)
	}

	version, expected, skipped, err := Parse(result.Raw)
	if err != nil {
		return result, err
	}
	result.Version, result.Expected, result.Skipped = version, expected, skipped
	return result, nil
}

// Parse decodes an expected-values file.
func Parse(raw []byte) (version string, expected []Expected, skipped int, err error) {
	var file struct {
		Header struct {
			Version string `json:"version"`
			Source  string `json:"source"`
		} `json:"header"`
		Data []struct {
			IDNum      int     `json:"IDNum"`
			ExpDamage  float64 `json:"expDamage"`
			ExpFrag    float64 `json:"expFrag"`
			ExpSpot    float64 `json:"expSpot"`
			ExpDef     float64 `json:"expDef"`
			ExpWinRate float64 `json:"expWinRate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return "", nil, 0, fmt.Errorf("xvm: parsing expected values: %w", err)
	}
	if len(file.Data) == 0 {
		return "", nil, 0, fmt.Errorf("xvm: file has no expected values")
	}
	if file.Header.Version == "" {
		// The version is what lets a WN8 figure say which vintage produced it,
		// so a file without one is not usable as-is.
		return "", nil, 0, fmt.Errorf("xvm: file has no header.version")
	}

	for _, d := range file.Data {
		if d.IDNum <= 0 || d.ExpDamage <= 0 || d.ExpFrag <= 0 || d.ExpSpot <= 0 ||
			d.ExpDef <= 0 || d.ExpWinRate <= 0 {
			skipped++
			continue
		}
		expected = append(expected, Expected{
			TankID: d.IDNum, Damage: d.ExpDamage, Frags: d.ExpFrag,
			Spot: d.ExpSpot, Def: d.ExpDef, WinRate: d.ExpWinRate,
		})
	}
	return file.Header.Version, expected, skipped, nil
}
