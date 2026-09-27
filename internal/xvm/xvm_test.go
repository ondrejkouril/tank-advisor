package xvm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "xvm", "wn8exp.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return raw
}

func TestParseRealExcerpt(t *testing.T) {
	version, expected, skipped, err := Parse(fixture(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if version != "2026-09-12" {
		t.Errorf("version = %q", version)
	}
	if len(expected) != 5 || skipped != 1 {
		t.Errorf("%d expected, %d skipped; want 5 and 1", len(expected), skipped)
	}
	for _, e := range expected {
		if e.TankID == 26705 && (e.Damage != 2147.761 || e.Def != 0.539 || e.WinRate != 48.847) {
			t.Errorf("Executor = %+v", e)
		}
	}
}

func TestParseRejectsUnusableFiles(t *testing.T) {
	for name, body := range map[string]string{
		"not json":   "<html>",
		"no data":    `{"data":[],"header":{"version":"2026-09-12"}}`,
		"no version": `{"data":[{"IDNum":1,"expDef":1,"expFrag":1,"expSpot":1,"expDamage":1,"expWinRate":50}]}`,
	} {
		if _, _, _, err := Parse([]byte(body)); err == nil {
			t.Errorf("Parse(%s) = nil error", name)
		}
	}
}

func TestFetchKeepsTheBodyOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("gone"))
	}))
	defer server.Close()

	c := &Client{URL: server.URL, HTTP: server.Client()}
	result, err := c.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("Fetch = %v, want a 404 error", err)
	}
	if result.HTTPStatus != 404 || string(result.Raw) != "gone" || result.RequestedAt.IsZero() {
		t.Errorf("result = %+v; the failure must still be recordable", result)
	}
}

func TestDefaultURLIsThePostSplitFile(t *testing.T) {
	// The pre-split file froze at 2024-09-12 and lacks Tier XI.
	if !strings.Contains(DefaultURL, "/json/wg/") {
		t.Errorf("DefaultURL = %s, want the wg/ file", DefaultURL)
	}
}
