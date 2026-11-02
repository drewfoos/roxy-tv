package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/arr"
	"github.com/roxy-media/roxy/server/internal/discover"
)

// arrSettingsPut decodes the body over the saved settings: a field an older app does not
// send keeps its value, and 0 still clears one.
func TestArrSettingsDecodeOverSaved(t *testing.T) {
	cur := discover.ArrSettings{Sonarr: arr.Config{URL: "http://s", RootFolder: "/data/TV", QualityProfileID: 4, OldAnimeQualityProfileID: 7, AnimeSeriesType: "standard"}}

	body := cur
	// An app without the older-anime setting changes the anime profile.
	if err := json.NewDecoder(strings.NewReader(`{"sonarr":{"url":"http://s","rootFolder":"/data/TV","qualityProfileId":4,"animeQualityProfileId":4}}`)).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Sonarr.OldAnimeQualityProfileID != 7 || body.Sonarr.AnimeQualityProfileID != 4 || body.Sonarr.AnimeSeriesType != "standard" {
		t.Errorf("got %+v, want old-anime 7 kept, anime 4, type kept", body.Sonarr)
	}

	body = cur
	if err := json.NewDecoder(strings.NewReader(`{"sonarr":{"oldAnimeQualityProfileId":0}}`)).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Sonarr.OldAnimeQualityProfileID != 0 {
		t.Errorf("0 should clear the older-anime profile, got %d", body.Sonarr.OldAnimeQualityProfileID)
	}
}
