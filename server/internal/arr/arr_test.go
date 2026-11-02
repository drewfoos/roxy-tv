package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeSonarr answers the calls AddSeries makes and records the series it was sent and
// the commands (searches) it was asked to run.
func fakeSonarr(t *testing.T, sent *map[string]any, commands ...*[]map[string]any) *httptest.Server {
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
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
			var cmd map[string]any
			_ = json.NewDecoder(r.Body).Decode(&cmd)
			if len(commands) > 0 {
				*commands[0] = append(*commands[0], cmd)
			}
			w.Write([]byte(`{"id":1}`))
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

// A new series is added with Sonarr's per-episode search off (it grabs a stray episode
// that then blocks the season pack) and searched by season instead.
func TestAddSeriesSearchesBySeason(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seasons []int
		want    string // the command sent
	}{
		{"whole series", nil, "SeriesSearch"},
		{"one season", []int{1}, "SeasonSearch"},
	} {
		var sent map[string]any
		var cmds []map[string]any
		srv := fakeSonarr(t, &sent, &cmds)
		c := New("sonarr", Config{URL: srv.URL, APIKey: "k", RootFolder: "/data/TV", QualityProfileID: 4})
		if _, err := c.AddSeries(context.Background(), 424536, false, tc.seasons); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		srv.Close()
		opts, _ := sent["addOptions"].(map[string]any)
		if opts["searchForMissingEpisodes"] != false {
			t.Errorf("%s: searchForMissingEpisodes %v, want false", tc.name, opts["searchForMissingEpisodes"])
		}
		if len(cmds) != 1 || cmds[0]["name"] != tc.want || cmds[0]["seriesId"] != float64(7) {
			t.Errorf("%s: commands %v, want one %s for series 7", tc.name, cmds, tc.want)
		}
	}
	// Search on add turned off in settings: no search at all.
	var sent map[string]any
	var cmds []map[string]any
	srv := fakeSonarr(t, &sent, &cmds)
	off := false
	c := New("sonarr", Config{URL: srv.URL, APIKey: "k", RootFolder: "/data/TV", QualityProfileID: 4, SearchOnAdd: &off})
	if _, err := c.AddSeries(context.Background(), 424536, false, nil); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	if len(cmds) != 0 {
		t.Errorf("search off: commands %v, want none", cmds)
	}
}

func TestReadyForRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want bool
	}{
		{"nothing", Config{}, false},
		{"address and key only (from the environment)", Config{URL: "http://s:8989", APIKey: "k"}, false},
		{"no quality profile", Config{URL: "http://s:8989", APIKey: "k", RootFolder: "/data/TV"}, false},
		{"no root folder", Config{URL: "http://s:8989", APIKey: "k", QualityProfileID: 4}, false},
		{"set up", Config{URL: "http://s:8989", APIKey: "k", RootFolder: "/data/TV", QualityProfileID: 4}, true},
	} {
		if got := tc.cfg.ReadyForRequests(); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
