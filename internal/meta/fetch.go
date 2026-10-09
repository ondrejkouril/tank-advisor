package meta

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/useragent"
)

// maxPage bounds a download; the largest page read, tomato.gg's tank
// performance table, is ~0.9 MB.
const maxPage = 16 << 20

// CacheFor is how long a fetched page is reused within one process. The MCP
// server lives for a conversation, and a conversation about five tanks should
// download a 0.9 MB page once, not five times. Nothing is written to disk:
// the data stays question-time (docs/spec.md section 3.5).
const CacheFor = 30 * time.Minute

// Page is one fetched document with its provenance.
type Page struct {
	URL       string
	Body      []byte
	FetchedAt time.Time
}

// Fetcher downloads server-wide pages with an honest User-Agent, and keeps
// each in memory for CacheFor.
type Fetcher struct {
	HTTP *http.Client
	Now  func() time.Time

	mu    sync.Mutex
	pages map[string]Page
}

func (f *Fetcher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// Get returns the page at rawURL, from memory when it was fetched less than
// CacheFor ago. Errors name the host, so a failure says whose page it was.
func (f *Fetcher) Get(ctx context.Context, rawURL string) (Page, error) {
	f.mu.Lock()
	if p, ok := f.pages[rawURL]; ok && f.now().Sub(p.FetchedAt) < CacheFor {
		f.mu.Unlock()
		return p, nil
	}
	f.mu.Unlock()

	host := rawURL
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		host = u.Host
	}
	client := f.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Page{}, fmt.Errorf("%s: %w", host, err)
	}
	req.Header.Set("User-Agent", useragent.String)
	resp, err := client.Do(req)
	if err != nil {
		return Page{}, fmt.Errorf("%s: %w", host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Page{}, fmt.Errorf("%s: HTTP %d from %s", host, resp.StatusCode, rawURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPage))
	if err != nil {
		return Page{}, fmt.Errorf("%s: reading page: %w", host, err)
	}

	p := Page{URL: rawURL, Body: body, FetchedAt: f.now().UTC()}
	f.mu.Lock()
	if f.pages == nil {
		f.pages = map[string]Page{}
	}
	f.pages[rawURL] = p
	f.mu.Unlock()
	return p, nil
}
