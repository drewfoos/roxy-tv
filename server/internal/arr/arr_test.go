package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeSonarr answers the calls AddSeries makes and records the series it was sent.
func fakeSonarr(t *testing.T, sent *map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			w.Write([]byte(`[{"title":"Frieren","tvdbId":424536,"seriesType":"standard","seasons":[{"seasonNumber":0},{"seasonNumber":1}]}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			if err := json.NewDecoder(r.Body).Decode(sent); err != nil {
				t.Errorf("decode: %v", err)
			}
			w.Write([]byte(`{"id":7}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
}

func TestAddSeriesAnimeTypeDefaultsToStandard(t *testing.T) {
	for _, tc := range []struct {
		setting string
		isAnime bool
		want    string
	}{
		{setting: "", isAnime: true, want: "standard"},       // the default follows a SxxEyy library
		{setting: "anime", isAnime: true, want: "anime"},     // chosen in settings
		{setting: "ANIME ", isAnime: true, want: "anime"},    // tolerant of case and spaces
		{setting: "anime", isAnime: false, want: "standard"}, // not anime: Sonarr's own type
	} {
		var sent map[string]any
		srv := fakeSonarr(t, &sent)
		c := New("sonarr", Config{URL: srv.URL, APIKey: "k", RootFolder: "/data/TV", QualityProfileID: 4, AnimeRootFolder: "/data/Anime", AnimeQualityProfileID: 4, AnimeSeriesType: tc.setting})
		id, err := c.AddSeries(context.Background(), 424536, tc.isAnime, nil)
		srv.Close()
		if err != nil || id != 7 {
			t.Fatalf("setting %q: id %d err %v", tc.setting, id, err)
		}
		if got := sent["seriesType"]; got != tc.want {
			t.Errorf("setting %q anime=%v: seriesType %v, want %s", tc.setting, tc.isAnime, got, tc.want)
		}
		wantRoot := "/data/TV"
		if tc.isAnime {
			wantRoot = "/data/Anime"
		}
		if sent["rootFolderPath"] != wantRoot {
			t.Errorf("rootFolderPath %v, want %s", sent["rootFolderPath"], wantRoot)
		}
	}
}
