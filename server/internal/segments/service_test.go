package segments

import (
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

func TestSweepNeeds(t *testing.T) {
	long := int64(1400000)
	intro := model.MediaSegment{Type: "intro", Origin: Origin}
	outro := model.MediaSegment{Type: "outro", Origin: Origin}
	done := store.SeasonEpisode{Index: 1, DurationMs: long, DetectVersion: DetectorVersion, Segments: []model.MediaSegment{intro, outro}}
	finale := store.SeasonEpisode{Index: 12, DurationMs: long, DetectVersion: DetectorVersion, Segments: []model.MediaSegment{intro}} // no ED: normal
	if needsFingerprints([]store.SeasonEpisode{done, finale}) {
		t.Error("a season whose episodes were all detected must not be redone for a missing outro")
	}
	fresh := store.SeasonEpisode{Index: 13, DurationMs: long}
	if !needsFingerprints([]store.SeasonEpisode{done, fresh}) {
		t.Error("an episode never detected brings its season back")
	}
	short := store.SeasonEpisode{Index: 1, DurationMs: 60000, DetectVersion: DetectorVersion, Segments: []model.MediaSegment{intro}}
	if !needsFingerprints([]store.SeasonEpisode{short}) {
		t.Error("fingerprint results on a too-short file are stale")
	}

	if needsAniSkip(done, "123") {
		t.Error("an episode with both segments needs no AniSkip")
	}
	if !needsAniSkip(finale, "123") {
		t.Error("never asked: ask once")
	}
	finale.AniSkipAsked, finale.AniSkipMal = true, "123"
	if needsAniSkip(finale, "123") {
		t.Error("asked with this MAL id already: not again on every restart")
	}
	if !needsAniSkip(finale, "456") {
		t.Error("a new MAL id asks again")
	}
}
