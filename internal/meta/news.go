package meta

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
)

// NewsURL is Wargaming's news feed for a realm's portal. It answers the
// question-time "what changed, and when" that the skill used to put to the
// news listing page through WebFetch.
func NewsURL(realm string) string {
	host := map[string]string{"eu": "worldoftanks.eu", "com": "worldoftanks.com", "asia": "worldoftanks.asia"}[realm]
	if host == "" {
		host = "worldoftanks.eu"
	}
	return "https://" + host + "/en/rss/news/"
}

// NewsItem is one article.
type NewsItem struct {
	Title     string
	Link      string
	Published time.Time
	Category  string
	Summary   string
	// IsUpdate marks the feed's "Updates" category, or a title naming an
	// update number: game updates, their preloads and micropatches.
	IsUpdate bool
	// Version is the update number the title names ("2.4.1"), when it names one.
	Version string
}

// News is a fetched feed.
type News struct {
	URL       string
	FetchedAt time.Time
	Items     []NewsItem // newest first, as the feed lists them
}

// News fetches and parses the feed at url.
func (f *Fetcher) News(ctx context.Context, url string) (News, error) {
	p, err := f.Get(ctx, url)
	if err != nil {
		return News{}, err
	}
	items, err := ParseNews(p.Body)
	if err != nil {
		return News{}, fmt.Errorf("%s: %w", url, err)
	}
	return News{URL: url, FetchedAt: p.FetchedAt, Items: items}, nil
}

var (
	versionRe = regexp.MustCompile(`\b\d+\.\d+(?:\.\d+)*\b`)
	// namedUpdateRe catches update articles filed elsewhere: on 2026-10-09
	// "Update 2.4.1: Map Reworks and Rebalances" sat in General News.
	namedUpdateRe = regexp.MustCompile(`(?i)\b(?:update|micropatch|patch)\s+\d+\.\d+`)
	tagRe         = regexp.MustCompile(`<[^>]*>`)
	spaceRe       = regexp.MustCompile(`\s+`)
)

// ParseNews reads an RSS 2.0 feed. An item without a title, link or a
// parseable date fails the whole feed: a dated list missing an entry would
// be quoted as complete.
func ParseNews(raw []byte) ([]NewsItem, error) {
	var feed struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			PubDate     string `xml:"pubDate"`
			Category    string `xml:"category"`
			Description string `xml:"description"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil, fmt.Errorf("news feed: %v; %w", err, ErrFormat)
	}
	if len(feed.Items) == 0 {
		return nil, fmt.Errorf("news feed: no items; %w", ErrFormat)
	}
	items := make([]NewsItem, 0, len(feed.Items))
	for _, it := range feed.Items {
		title, link := strings.TrimSpace(it.Title), strings.TrimSpace(it.Link)
		published, err := time.Parse(time.RFC1123, strings.TrimSpace(it.PubDate))
		if err != nil {
			published, err = time.Parse(time.RFC1123Z, strings.TrimSpace(it.PubDate))
		}
		if title == "" || link == "" || err != nil {
			return nil, fmt.Errorf("news feed: an item lacks a title, link or date (%q); %w", title, ErrFormat)
		}
		category := strings.TrimSpace(it.Category)
		item := NewsItem{
			Title: html.UnescapeString(title), Link: link, Published: published.UTC(), Category: category,
			Summary: summary(it.Description),
			IsUpdate: strings.EqualFold(category, "Updates") || strings.Contains(link, "/news/updates/") ||
				namedUpdateRe.MatchString(title),
		}
		if item.IsUpdate {
			item.Version = versionRe.FindString(title)
		}
		items = append(items, item)
	}
	return items, nil
}

// summary is the description's text without its markup and its "Read more"
// links, cut to a sentence or two.
func summary(description string) string {
	text := html.UnescapeString(tagRe.ReplaceAllString(description, " "))
	text = strings.TrimSpace(spaceRe.ReplaceAllString(text, " "))
	for _, tail := range []string{" Read more", " Discuss"} {
		if i := strings.Index(text, tail); i >= 0 {
			text = strings.TrimSpace(text[:i])
		}
	}
	if r := []rune(text); len(r) > 280 {
		text = string(r[:277]) + "..."
	}
	return text
}
