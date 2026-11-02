package store

import (
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// The character worker's bookkeeping: a series with a provider id is due once; after a
// good lookup it is not due again; a failed one comes back after the retry time, at most
// three tries; suggestions list each character once, newest series first.
func TestSeriesCharacters(t *testing.T) {
	s, lib, ctx := openTest(t)
	mk := func(id, title string, provider string) {
		it := &model.Item{ID: id, GUID: "g:" + id, LibraryID: lib.ID, Type: model.TypeSeries, Title: title}
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		if provider != "" {
			if err := s.SetProviderIDs(ctx, id, map[string]string{provider: "1"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("a", "Anime", "anilist")
	mk("b", "Show", "tmdb")
	mk("c", "Unmatched", "")

	due, err := s.SeriesNeedingCharacters(ctx, 10, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 2 {
		t.Fatalf("due %v, want a and b (c has no provider id)", due)
	}
	if err := s.SaveSeriesCharacters(ctx, "a", []SeriesCharacter{{Source: "anilist", ID: 1, Name: "Roxy", Image: "x"}, {Source: "anilist", ID: 2, Name: "Rudeus", Image: "y"}}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSeriesCharacters(ctx, "b", nil, false); err != nil {
		t.Fatal(err)
	}
	due, _ = s.SeriesNeedingCharacters(ctx, 10, time.Now().Add(-time.Hour))
	if len(due) != 0 {
		t.Fatalf("due right after lookups: %v", due)
	}
	due, _ = s.SeriesNeedingCharacters(ctx, 10, time.Now().Add(time.Hour))
	if len(due) != 1 || due[0] != "b" {
		t.Fatalf("failed lookup not retried: %v", due)
	}
	_ = s.SaveSeriesCharacters(ctx, "b", nil, false)
	_ = s.SaveSeriesCharacters(ctx, "b", nil, false)
	due, _ = s.SeriesNeedingCharacters(ctx, 10, time.Now().Add(time.Hour))
	if len(due) != 0 {
		t.Fatalf("retried past three tries: %v", due)
	}

	got, err := s.SuggestedCharacters(ctx, 6, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "Roxy" {
		t.Fatalf("suggestions %+v", got)
	}
}
