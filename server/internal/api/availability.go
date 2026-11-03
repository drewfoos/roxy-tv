package api

import (
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// fillAvailability derives Availability for playable items: "available" when a media
// source exists, "unaired" when the provider air date is today or later (or a regular
// episode has no date yet: announced, "TBA"), else "missing".
// Series and seasons are containers and get no value.
func fillAvailability(items []model.Item) {
	fillAvailabilityAt(items, time.Now())
}

// fillAvailabilityAt is fillAvailability as of now. Air dates are calendar dates in the
// broadcaster's time zone with no time of day, so an episode airing today counts as
// unaired all day (it may not have aired yet, and the file needs time to arrive), and a
// partial date ("2026", "2026-11") still to come or in progress counts as unaired too.
func fillAvailabilityAt(items []model.Item, now time.Time) {
	today := now.Format("2006-01-02")
	for i := range items {
		it := &items[i]
		if it.Type != model.TypeEpisode && it.Type != model.TypeMovie {
			continue
		}
		switch {
		case it.HasMedia:
			it.Availability = "available"
		case it.Premiered != "" && (it.Premiered >= today || strings.HasPrefix(today, it.Premiered)):
			it.Availability = "unaired"
		case it.Premiered == "" && it.Type == model.TypeEpisode && it.SeasonAired != nil && *it.SeasonAired > 0:
			// No date yet: an announced episode (Alya S2E1 "TBA"), not one the library lacks.
			// Specials keep "missing": old ones often have no date and did air.
			it.Availability = "unaired"
		default:
			it.Availability = "missing"
		}
	}
}
