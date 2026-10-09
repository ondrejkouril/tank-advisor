package meta

import (
	"errors"
	"testing"
	"time"
)

// TestParseNews: eight items cut from the live EU feed (2026-10-09).
func TestParseNews(t *testing.T) {
	items, err := ParseNews(page(t, "wot-news-eu.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("%d items, want 8", len(items))
	}
	var preload *NewsItem
	updates := 0
	for i := range items {
		if items[i].IsUpdate {
			updates++
		}
		if items[i].Title == "Preload Update 2.4.1 Right Now!" {
			preload = &items[i]
		}
	}
	if preload == nil {
		t.Fatal("no preload item")
	}
	if !preload.IsUpdate || preload.Version != "2.4.1" || preload.Category != "Updates" ||
		!preload.Published.Equal(time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("preload = %+v", preload)
	}
	if preload.Summary != "Download Update 2.4.1 in advance and play as soon as it launches!" {
		t.Errorf("summary = %q", preload.Summary)
	}
	if updates < 2 {
		t.Errorf("%d update items, want the preload and the patch notes at least", updates)
	}
}

func TestParseNewsFailsLoudly(t *testing.T) {
	for name, in := range map[string]string{
		"not xml":  `<html>`,
		"no items": `<rss><channel></channel></rss>`,
		"bad date": `<rss><channel><item><title>x</title><link>https://x</link><pubDate>yesterday</pubDate></item></channel></rss>`,
	} {
		if _, err := ParseNews([]byte(in)); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err = %v, want ErrFormat", name, err)
		}
	}
}
