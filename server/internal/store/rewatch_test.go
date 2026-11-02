package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// seriesWithEpisodes stores a series with one season of n real episodes (guids g:e1..)
// plus a placeholder for episode n+1 (g:e<n+1>).
func seriesWithEpisodes(t *testing.T, s *Store, lib *model.Library, ctx context.Context, n int) {
	t.Helper()
	// The profile row does not matter here.
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	series := &model.Item{ID: "sr", GUID: "g:sr", LibraryID: lib.ID, Type: model.TypeSeries, Title: "S"}
	season := &model.Item{ID: "se", GUID: "g:se", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series.ID, Title: "Season 1", IndexAired: ip(1)}
	for _, it := range []*model.Item{series, season} {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= n; i++ {
		ep := &model.Item{ID: fmt.Sprintf("e%d", i), GUID: fmt.Sprintf("g:e%d", i), LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID, Title: "E", IndexAired: ip(i), SeasonAired: ip(1)}
		if err := s.UpsertItem(ctx, ep); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO media_sources (id, item_id) VALUES (?, ?)`, "src"+ep.ID, ep.ID); err != nil {
			t.Fatal(err)
		}
	}
	ph := &model.Item{ID: fmt.Sprintf("e%d", n+1), GUID: fmt.Sprintf("g:e%d", n+1), LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID, Title: "E", IndexAired: ip(n + 1), SeasonAired: ip(1)}
	if _, _, err := s.EnsureVirtualItem(ctx, ph); err != nil {
		t.Fatal(err)
	}
}

func continueIDs(t *testing.T, s *Store, ctx context.Context) []string {
	t.Helper()
	items, err := s.ContinueItems(ctx, "u", 20)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

// Marking a season watched leaves its placeholders alone: the file that later arrives
// under the placeholder's GUID is not already watched.
func TestMarkPlayedUnderSkipsPlaceholders(t *testing.T) {
	s, lib, ctx := openTest(t)
	seriesWithEpisodes(t, s, lib, ctx, 2)
	if err := s.MarkPlayedUnder(ctx, "u", "se", true); err != nil {
		t.Fatal(err)
	}
	for guid, want := range map[string]bool{"g:e1": true, "g:e2": true, "g:e3": false} {
		d, _ := s.GetUserItemData(ctx, "u", guid)
		if got := d != nil && d.Played; got != want {
			t.Errorf("%s played = %v, want %v", guid, got, want)
		}
	}
}

// A rewatch keeps the show in Continue Watching: a watched episode started again is
// resumable, and finishing one offers the next in order although it is watched too.
func TestContinueWatchingRewatch(t *testing.T) {
	s, lib, ctx := openTest(t)
	seriesWithEpisodes(t, s, lib, ctx, 3)
	for _, g := range []string{"g:e1", "g:e2", "g:e3"} {
		if err := s.FinishPlay(ctx, "u", g); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	// Finished the whole show (the placeholder E4 does not count): nothing to continue.
	if ids := continueIDs(t, s, ctx); len(ids) != 0 {
		t.Fatalf("after the finale: %v, want nothing", ids)
	}
	// Rewatching E1, half way.
	if err := s.SetProgress(ctx, "u", "g:e1", 60_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ids := continueIDs(t, s, ctx); len(ids) != 1 || ids[0] != "e1" {
		t.Fatalf("rewatch in progress: %v, want [e1]", ids)
	}
	time.Sleep(2 * time.Millisecond)
	if err := s.FinishPlay(ctx, "u", "g:e1"); err != nil {
		t.Fatal(err)
	}
	if ids := continueIDs(t, s, ctx); len(ids) != 1 || ids[0] != "e2" {
		t.Fatalf("after rewatching E1: %v, want [e2]", ids)
	}
	// A first watch still skips what is already watched: unwatch E3, finish E1 again.
	if err := s.MarkPlayed(ctx, "u", "g:e3", false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := s.FinishPlay(ctx, "u", "g:e1"); err != nil {
		t.Fatal(err)
	}
	if ids := continueIDs(t, s, ctx); len(ids) != 1 || ids[0] != "e3" {
		t.Fatalf("with E3 unwatched: %v, want [e3]", ids)
	}
}
