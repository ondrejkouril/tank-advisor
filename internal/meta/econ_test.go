package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEconomics(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Write(page(t, "tomato-econ-30.html"))
	}))
	defer server.Close()
	c := &Tomato{Base: server.URL, Fetch: &Fetcher{HTTP: server.Client()}}

	table, err := c.Economics(context.Background(), "all", 30)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/economics/all/30" || len(table.ByTank) != 10 || table.Account != "all" || table.Days != 30 {
		t.Errorf("path %s, table %d rows, %s/%d", path, len(table.ByTank), table.Account, table.Days)
	}
	// The Gorilla as the live page had it on 2026-10-09.
	g := table.ByTank[23329]
	if g.Battles != 16120 || g.Earnings != 65434 || g.Profit != -11436 || g.AmmoCost != 42898 || g.ProfitPerMinute != -2076 {
		t.Errorf("Gorilla = %+v", g)
	}

	// tomato.gg serves "all" for any filter it does not know, so a typo
	// must fail here rather than be quoted as the filter asked for.
	for _, bad := range []struct {
		account string
		days    int
	}{{"prem", 30}, {"all", 7}} {
		if _, err := c.Economics(context.Background(), bad.account, bad.days); err == nil {
			t.Errorf("Economics(%q, %d) accepted", bad.account, bad.days)
		}
	}
}
