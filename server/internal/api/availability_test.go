package api

import (
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

func TestAvailabilityAiringToday(t *testing.T) {
	now := time.Date(2026, 11, 1, 23, 30, 0, 0, time.Local)
	items := []model.Item{
		{Type: model.TypeEpisode, Premiered: "2026-11-01"}, // airs today: not missing yet
		{Type: model.TypeEpisode, Premiered: "2026-11-02"},
		{Type: model.TypeEpisode, Premiered: "2026-10-31"},
		{Type: model.TypeEpisode, Premiered: "2026-11"}, // this month, day unknown
		{Type: model.TypeEpisode, Premiered: "2025"},
		{Type: model.TypeEpisode},
		{Type: model.TypeEpisode, Premiered: "2026-11-01", HasMedia: true},
	}
	fillAvailabilityAt(items, now)
	want := []string{"unaired", "unaired", "missing", "unaired", "missing", "missing", "available"}
	for i, w := range want {
		if items[i].Availability != w {
			t.Errorf("item %d (%q): got %q, want %q", i, items[i].Premiered, items[i].Availability, w)
		}
	}
}

// An episode with no date yet is announced ("TBA"), not missing; a special without a date
// (old ones often have none) stays missing.
func TestAvailabilityUndatedEpisode(t *testing.T) {
	two, zero := 2, 0
	items := []model.Item{
		{Type: model.TypeEpisode, SeasonAired: &two},
		{Type: model.TypeEpisode, SeasonAired: &zero},
	}
	fillAvailabilityAt(items, time.Now())
	if items[0].Availability != "unaired" || items[1].Availability != "missing" {
		t.Fatalf("got %q, %q", items[0].Availability, items[1].Availability)
	}
}
