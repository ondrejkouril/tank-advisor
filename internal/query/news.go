package query

import (
	"context"
	"errors"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/meta"
	"github.com/ondrejkouril/tank-advisor/internal/store"
)

const srcGameInfo = "wg:encyclopedia/info"

// GameVersion is the server's game version as the last sync saw it.
//
// A release is dated only by a change a sync has seen: Previous with
// ReleasedAfter and ReleasedBy. Without one, the version was already in use
// when recording began on RecordedSince, and its release date is unknown here.
type GameVersion struct {
	Version       string     `json:"version"`
	Previous      string     `json:"previous,omitempty"`
	ReleasedAfter *time.Time `json:"released_after,omitempty"`
	ReleasedBy    *time.Time `json:"released_by,omitempty"`
	RecordedSince *time.Time `json:"recorded_since,omitempty"`
}

// NewsEntry is one article from the feed.
type NewsEntry struct {
	Title     string    `json:"title"`
	Published time.Time `json:"published"`
	Link      string    `json:"link"`
	Category  string    `json:"category"`
	IsUpdate  bool      `json:"is_update"`
	Version   string    `json:"version,omitempty"`
	Summary   string    `json:"summary,omitempty"`
}

// News is `meta news`.
type News struct {
	ServerVersion *GameVersion `json:"server_game_version,omitempty"`
	Items         []NewsEntry  `json:"items"`
}

// News lists the feed's articles, optionally only updates or only those
// published since a date, beside the server's game version.
func (s *Service) News(ctx context.Context, feed *meta.News, since time.Time, updatesOnly bool) (Envelope, error) {
	r, err := s.begin(ctx)
	if err != nil {
		return Envelope{}, err
	}
	// The feed is live: its age is the fetch's.
	r.meta.Sources = append(r.meta.Sources, Source{Name: feed.URL, ObservedAt: feed.FetchedAt,
		AgeSeconds: max(0, int64(r.now.Sub(feed.FetchedAt).Seconds()))})

	out := News{Items: []NewsEntry{}}
	if v, err := s.DB.GameVersionState(ctx); err == nil {
		r.use(srcGameInfo)
		gv := &GameVersion{Version: v.Current.Version}
		if v.Previous != nil {
			after, by := v.Previous.LastSeen, v.Current.FirstSeen
			gv.Previous, gv.ReleasedAfter, gv.ReleasedBy = v.Previous.Version, &after, &by
		} else {
			since := v.Current.FirstSeen
			gv.RecordedSince = &since
			r.caveat("wotctx began recording the server's game version on %s, with %s already in use; when %s was released is not known from that, so take it from the update items' dates",
				since.Format("2006-01-02"), v.Current.Version, v.Current.Version)
		}
		out.ServerVersion = gv
	} else if !errors.Is(err, store.ErrNotFound) {
		return Envelope{}, err
	}

	for _, it := range feed.Items {
		if (updatesOnly && !it.IsUpdate) || it.Published.Before(since) {
			continue
		}
		out.Items = append(out.Items, NewsEntry{Title: it.Title, Published: it.Published, Link: it.Link,
			Category: it.Category, IsUpdate: it.IsUpdate, Version: it.Version, Summary: it.Summary})
	}
	r.caveat("the feed carries only the latest %d articles; an older update is not in it. Patch notes are at each update's link", len(feed.Items))
	return r.finish(out), nil
}
