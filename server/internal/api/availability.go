package api

import (
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// fillAvailability derives Availability for playable items: "available" when a media
// source exists, "unaired" when the provider air date is in the future, else "missing".
// Series and seasons are containers and get no value.
func fillAvailability(items []model.Item) {
	today := time.Now().UTC().Format("2006-01-02")
	for i := range items {
		it := &items[i]
		if it.Type != model.TypeEpisode && it.Type != model.TypeMovie {
			continue
		}
		switch {
		case it.HasMedia:
			it.Availability = "available"
		case it.Premiered != "" && it.Premiered > today:
			it.Availability = "unaired"
		default:
			it.Availability = "missing"
		}
	}
}
