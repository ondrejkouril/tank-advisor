package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOnslaught(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Write(page(t, "tomato-onslaught.html"))
	}))
	defer server.Close()

	// No fetcher given: the client makes its own.
	table, err := (&Tomato{Base: server.URL}).Onslaught(context.Background(), "eu")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/onslaught/EU" || table.Season != "azure_phoenix" || len(table.ByTank) != 8 || len(table.Ratings) != 8 {
		t.Fatalf("path %s, season %q, %d tanks, %d buckets", path, table.Season, len(table.ByTank), len(table.Ratings))
	}
	if o := table.ByTank[16897]; o.Name != "Obj. 140" || o.Battles != 365597 || o.WinRate != 51.48 {
		t.Errorf("Obj. 140 = %+v", o)
	}
	if b := table.Ratings[0]; b.Rating != 0 || b.Players != 2104 || table.Ratings[7].Rating != 700 {
		t.Errorf("ratings = %+v", table.Ratings)
	}
}
