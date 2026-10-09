package meta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func page(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "meta", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestParseTomatoPages: each page's fixture is ten records cut from the live
// page (2026-10-09). Every record must parse, and its update stamp be read,
// whichever of the two date formats the page uses.
func TestParseTomatoPages(t *testing.T) {
	for _, tc := range []struct {
		file    string
		lead    string
		records int
		updated time.Time
	}{
		{"tomato-moe.html", "65", 3, time.Date(2026, 9, 18, 11, 45, 43, 933249000, time.UTC)},
		{"tomato-perf-30.html", "tank_id", 10, time.Date(2026, 10, 9, 1, 25, 36, 258000000, time.UTC)},
		{"tomato-econ-30.html", "tank_id", 10, time.Date(2026, 10, 1, 17, 36, 16, 133000000, time.UTC)},
		{"tomato-onslaught.html", "tank_id", 8, time.Date(2026, 10, 8, 20, 9, 58, 725000000, time.UTC)},
		{"tomato-onslaught.html", "rating_bucket", 8, time.Date(2026, 10, 8, 20, 9, 58, 725000000, time.UTC)},
	} {
		updated, records, err := ParseTomato(page(t, tc.file), tc.lead)
		if err != nil {
			t.Errorf("%s: %v", tc.file, err)
			continue
		}
		if !updated.Equal(tc.updated) {
			t.Errorf("%s: updated = %v, want %v", tc.file, updated, tc.updated)
		}
		if len(records) != tc.records {
			t.Errorf("%s/%s: %d records, want %d", tc.file, tc.lead, len(records), tc.records)
		}
	}
}

// TestRecordFields: the Gorilla's row as the live performance page had it,
// read field by field, including a boolean written as !1.
func TestRecordFields(t *testing.T) {
	_, records, err := ParseTomato(page(t, "tomato-perf-30.html"), "tank_id")
	if err != nil {
		t.Fatal(err)
	}
	var gorilla Record
	for _, r := range records {
		if id, _ := r.Int("tank_id"); id == 23329 {
			gorilla = r
		}
	}
	if gorilla == nil {
		t.Fatal("no Gorilla row")
	}
	if gorilla.String("name") != "Gorilla" || gorilla.String("class") != "TD" {
		t.Errorf("name/class = %q/%q", gorilla.String("name"), gorilla.String("class"))
	}
	if n, _ := gorilla.Int("battles"); n != 245313 {
		t.Errorf("battles = %d", n)
	}
	if f, _ := gorilla.Float("winrate_differential"); f != -2.02 {
		t.Errorf("winrate_differential = %v", f)
	}
	if prem, ok := gorilla.Bool("isPrem"); !ok || prem {
		t.Errorf("isPrem = %v, %v; want false", prem, ok)
	}
}

func TestLiteral(t *testing.T) {
	for in, check := range map[string]func(Record) bool{
		// quoted keys, a negative, a back-reference, void 0, nesting, an escape
		`{tank_id:1,"7diff65":-3,ref:$R[4],u:void 0,n:null,a:$R[5]=[1,2],o:{x:"a\"b"},t:!0}`: func(r Record) bool {
			d, _ := r.Int("7diff65")
			_, hasRef := r["ref"]
			o, _ := r["o"].(map[string]any)
			arr, _ := r["a"].([]any)
			b, _ := r.Bool("t")
			return d == -3 && hasRef && r["ref"] == nil && r["u"] == nil && len(arr) == 2 && o["x"] == `a"b` && b
		},
		`{tank_id:2,name:"Tesák",f:1.5e2}`: func(r Record) bool {
			f, _ := r.Float("f")
			return r.String("name") == "Tesák" && f == 150
		},
	} {
		_, records, err := ParseTomato([]byte(`updated:"2026-10-09T00:00:00Z" `+in), "tank_id")
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if len(records) != 1 || !check(records[0]) {
			t.Errorf("%s: parsed as %v", in, records)
		}
	}
}

// TestParseTomatoFailsLoudly: a redesign must not produce a partial table
// that would be quoted as complete.
func TestParseTomatoFailsLoudly(t *testing.T) {
	for name, in := range map[string]string{
		"no timestamp":      `{tank_id:1,name:"x"}`,
		"bad timestamp":     `updated:"yesterday" {tank_id:1}`,
		"no records":        `updated:"2026-10-09T00:00:00Z"`,
		"unreadable record": `updated:"2026-10-09T00:00:00Z" {tank_id:1,when:new Date(0)}`,
		"unterminated":      `updated:"2026-10-09T00:00:00Z" {tank_id:1,name:"x`,
	} {
		if _, _, err := ParseTomato([]byte(in), "tank_id"); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err = %v, want ErrFormat", name, err)
		}
	}
}

// TestFetcherCaches: a second question within CacheFor reuses the page; after
// it, the page is fetched again.
func TestFetcherCaches(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Write([]byte("page"))
	}))
	defer server.Close()

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	f := &Fetcher{HTTP: server.Client(), Now: func() time.Time { return now }}
	ctx := context.Background()
	for range 3 {
		if p, err := f.Get(ctx, server.URL); err != nil || string(p.Body) != "page" {
			t.Fatalf("Get = %q, %v", p.Body, err)
		}
	}
	if hits != 1 {
		t.Errorf("%d requests within the cache window, want 1", hits)
	}
	now = now.Add(CacheFor)
	if _, err := f.Get(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Errorf("%d requests after the cache window, want 2", hits)
	}
}

// TestFetcherErrorsNameTheHost: the player is told whose page failed.
func TestFetcherErrorsNameTheHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	_, err := (&Fetcher{}).Get(context.Background(), server.URL+"/x")
	if err == nil || !strings.Contains(err.Error(), strings.TrimPrefix(server.URL, "http://")) || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v", err)
	}
}
