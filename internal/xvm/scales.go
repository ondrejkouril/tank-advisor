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

// ScalesURL is XVM's rating scales for the Wargaming servers: for each rating,
// the value at each of 100 steps of its "XVM scale", the share of the players
// XVM rates that have a lower figure. Measured 2026-10-09: 2.6 KB, regenerated
// daily (Last-Modified the day before), and identical to the copy the XVM mod
// keeps in the game's own folder.
const ScalesURL = "https://static.modxvm.com/xvmscales-wg.json"

// Scales holds the two scales wotctx ranks against.
type Scales struct {
	WN8     []float64
	WinRate []float64 // percent
}

// ScalesResult is one fetch, raw body included so it can be stored first.
type ScalesResult struct {
	URL          string
	RequestedAt  time.Time
	HTTPStatus   int
	Raw          []byte
	ETag         string
	LastModified string

	Scales Scales
}

// FetchScales downloads and parses the scales.
func (c *Client) FetchScales(ctx context.Context) (ScalesResult, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	url := c.ScalesURL
	if url == "" {
		url = ScalesURL
	}
	result := ScalesResult{URL: url, RequestedAt: now().UTC()}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return result, fmt.Errorf("xvm: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", useragent.String)
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("xvm: %w", err)
	}
	defer resp.Body.Close()

	result.HTTPStatus = resp.StatusCode
	result.ETag = resp.Header.Get("ETag")
	result.LastModified = resp.Header.Get("Last-Modified")
	result.Raw, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return result, fmt.Errorf("xvm: reading body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("xvm: HTTP %d from %s", resp.StatusCode, url)
	}
	result.Scales, err = ParseScales(result.Raw)
	return result, err
}

// ParseScales decodes a scales file. Both scales must have 100 steps in
// rising order, or a rank read from them would be wrong without saying so.
func ParseScales(raw []byte) (Scales, error) {
	var file struct {
		WN8 []float64 `json:"xwn8"`
		Win []float64 `json:"xwin"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return Scales{}, fmt.Errorf("xvm: parsing scales: %w", err)
	}
	for name, scale := range map[string][]float64{"xwn8": file.WN8, "xwin": file.Win} {
		if len(scale) != 100 {
			return Scales{}, fmt.Errorf("xvm: scale %s has %d steps, want 100", name, len(scale))
		}
		for i := 1; i < len(scale); i++ {
			if scale[i] < scale[i-1] {
				return Scales{}, fmt.Errorf("xvm: scale %s falls at step %d", name, i)
			}
		}
	}
	return Scales{WN8: file.WN8, WinRate: file.Win}, nil
}

// Rank places a value on a scale: the number of steps at or below it, 0 to
// 100, which is the share of rated players with a lower figure, in percent.
func Rank(scale []float64, value float64) int {
	n := 0
	for _, step := range scale {
		if value >= step {
			n++
		}
	}
	return n
}
