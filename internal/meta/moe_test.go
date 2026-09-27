package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "meta", "tomato-moe.html"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestParseMoERealExcerpt: three records cut from the live page (2026-09-18),
// including one with a non-ASCII name and a tier XI.
func TestParseMoERealExcerpt(t *testing.T) {
	table, err := ParseMoE(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 18, 11, 45, 43, 933249000, time.UTC); !table.Updated.Equal(want) {
		t.Errorf("updated = %v, want %v", table.Updated, want)
	}
	if len(table.ByTank) != 3 {
		t.Fatalf("records = %d, want 3", len(table.ByTank))
	}
	tesak := table.ByTank[6769]
	if tesak.Name != "Tesák" || tesak.Tier != 10 {
		t.Errorf("Tesák = %+v", tesak)
	}
	for mark, want := range map[int]int{65: 2198, 85: 3311, 95: 4242, 100: 5026} {
		if tesak.Thresholds[mark] != want {
			t.Errorf("Tesák %d%% = %d, want %d", mark, tesak.Thresholds[mark], want)
		}
	}
	if tesak.Change30d[95] != 443 {
		t.Errorf("Tesák 30-day change at 95%% = %d, want 443", tesak.Change30d[95])
	}
	if br := table.ByTank[67361]; br.Name != "Black Rock" || br.Tier != 11 {
		t.Errorf("Black Rock = %+v", br)
	}
}

// TestParseMoEFailsLoudly: a redesign must not produce a partial table that
// would be quoted as complete.
func TestParseMoEFailsLoudly(t *testing.T) {
	for name, page := range map[string]string{
		"no timestamp": `{65:1,85:2,95:3,100:4,id:1,name:"x",tier:5}`,
		"no records":   `updated:"2026-09-18 11:45:43.933249+00:00"`,
		"empty":        ``,
	} {
		if _, err := ParseMoE([]byte(page)); err == nil || !strings.Contains(err.Error(), "format has changed") {
			t.Errorf("%s: err = %v, want a format-change error", name, err)
		}
	}
}

func TestFetchMoE(t *testing.T) {
	var agent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		w.Write(fixture(t))
	}))
	defer server.Close()

	c := &MoEClient{URL: server.URL, HTTP: server.Client()}
	table, err := c.FetchMoE(context.Background(), "eu")
	if err != nil {
		t.Fatal(err)
	}
	if table.URL != server.URL || table.FetchedAt.IsZero() || len(table.ByTank) != 3 {
		t.Errorf("table = %+v", table)
	}
	if !strings.Contains(agent, "wotctx") {
		t.Errorf("User-Agent = %q, want one naming wotctx", agent)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer failing.Close()
	if _, err := (&MoEClient{URL: failing.URL}).FetchMoE(context.Background(), "eu"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("FetchMoE on a 503 = %v", err)
	}
}

func TestServer(t *testing.T) {
	for realm, want := range map[string]string{"eu": "eu", "com": "na", "asia": "asia"} {
		if got := Server(realm); got != want {
			t.Errorf("Server(%q) = %q, want %q", realm, got, want)
		}
	}
}
