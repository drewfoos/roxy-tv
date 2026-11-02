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
