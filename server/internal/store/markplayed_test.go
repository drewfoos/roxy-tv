package store

import (
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// "Mark all watched" over a season half seen must not count the seen episodes again or
// move their last played time; the unseen ones get one play now. Marking a single
// unplayed item watched still counts one play.
func TestMarkPlayedUnderKeepsHistoryOfPlayedEpisodes(t *testing.T) {
	s, lib, ctx := openTest(t)
	u := &model.User{ID: "u1", Name: "drew"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	scope, err := s.DefaultProfileID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	str := func(v string) *string { return &v }
	items := []*model.Item{
		{ID: "series", GUID: "g-series", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"},
		{ID: "season", GUID: "g-season", LibraryID: lib.ID, Type: model.TypeSeason, Title: "Season 1", ParentID: str("series")},
		{ID: "e1", GUID: "g-e1", LibraryID: lib.ID, Type: model.TypeEpisode, Title: "E1", ParentID: str("season")},
		{ID: "e2", GUID: "g-e2", LibraryID: lib.ID, Type: model.TypeEpisode, Title: "E2", ParentID: str("season")},
	}
	for _, it := range items {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	// E1 was watched twice, a while ago.
	old := "2026-01-01T00:00:00.000Z"
	if _, err := s.db.ExecContext(ctx, `INSERT INTO user_item_data (scope_id, item_guid, position_ms, play_count, played, last_played_at) VALUES (?, 'g-e1', 0, 2, 1, ?)`, scope, old); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPlayedUnder(ctx, scope, "series", true); err != nil {
		t.Fatal(err)
	}
	e1, _ := s.GetUserItemData(ctx, scope, "g-e1")
	if e1 == nil || e1.PlayCount != 2 || !e1.Played || !e1.LastPlayedAt.Equal(ParseTime(old)) {
		t.Fatalf("watched episode changed: %+v", e1)
	}
	e2, _ := s.GetUserItemData(ctx, scope, "g-e2")
	if e2 == nil || e2.PlayCount != 1 || !e2.Played || time.Since(e2.LastPlayedAt) > time.Minute {
		t.Fatalf("unwatched episode: %+v, want one play now", e2)
	}
	// Doing it again changes nothing.
	if err := s.MarkPlayedUnder(ctx, scope, "season", true); err != nil {
		t.Fatal(err)
	}
	if e2b, _ := s.GetUserItemData(ctx, scope, "g-e2"); e2b.PlayCount != 1 || !e2b.LastPlayedAt.Equal(e2.LastPlayedAt) {
		t.Fatalf("marking again changed E2: %+v", e2b)
	}
	// Unmark, then mark the single episode: an unplayed item counts one more play.
	if err := s.MarkPlayed(ctx, scope, "g-e1", false); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPlayed(ctx, scope, "g-e1", true); err != nil {
		t.Fatal(err)
	}
	if e1, _ = s.GetUserItemData(ctx, scope, "g-e1"); e1.PlayCount != 3 || time.Since(e1.LastPlayedAt) > time.Minute {
		t.Fatalf("explicit mark of an unplayed item: %+v, want count 3 and played now", e1)
	}
}
