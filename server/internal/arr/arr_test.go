package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
				cmdMu.Lock()
				*commands[0] = append(*commands[0], cmd)
				cmdMu.Unlock()
			}
			w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			// No episodes for the first call, as right after an add; then some.
			if episodeCalls.Add(1) == 1 {
				w.Write([]byte(`[]`))
			} else {
				w.Write([]byte(`[{"id":1,"seasonNumber":1,"episodeNumber":1}]`))
			}
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
		id, err := c.AddSeries(context.Background(), 424536, tc.isAnime, nil, nil, nil)
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

var (
	cmdMu        sync.Mutex
	episodeCalls atomic.Int32
)

// commandsAfter waits briefly for the background search to send its commands.
func commandsAfter(cmds *[]map[string]any, want int) []map[string]any {
	for i := 0; i < 200; i++ {
		cmdMu.Lock()
		n := len(*cmds)
		cmdMu.Unlock()
		if n >= want {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // and nothing more
	cmdMu.Lock()
	defer cmdMu.Unlock()
	return append([]map[string]any(nil), (*cmds)...)
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
		readyPoll.Store(time.Millisecond)
		episodeCalls.Store(0)
		srv := fakeSonarr(t, &sent, &cmds)
		c := New("sonarr", Config{URL: srv.URL, APIKey: "k", RootFolder: "/data/TV", QualityProfileID: 4})
		if _, err := c.AddSeries(context.Background(), 424536, false, tc.seasons, nil, nil); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		cmds = commandsAfter(&cmds, 1)
		srv.Close()
		if episodeCalls.Load() < 2 {
			t.Errorf("%s: searched before the episodes loaded", tc.name)
		}
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
	if _, err := c.AddSeries(context.Background(), 424536, false, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	cmds = commandsAfter(&cmds, 0)
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

// fakeSonarrWith serves a lookup result and an optional existing series, recording what was sent.
func fakeSonarrWith(t *testing.T, lookup, existing string, sent *map[string]any, calls *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series":
			w.Write([]byte(existing))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			w.Write([]byte(lookup))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			_ = json.NewDecoder(r.Body).Decode(sent)
			w.Write([]byte(`{"id":7}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
			w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
}

// Older anime (before 2010 and ended) takes the older-anime profile; a long runner that
// started earlier but still airs, and modern anime, keep the anime profile.
func TestAddSeriesOlderAnimeProfile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		lookup string
		anime  bool
		want   float64
	}{
		{"old and ended", `[{"tvdbId":1,"year":2000,"status":"ended","seasons":[]}]`, true, 7},
		{"old but still airing", `[{"tvdbId":1,"year":1999,"status":"continuing","seasons":[]}]`, true, 4},
		{"modern", `[{"tvdbId":1,"year":2023,"status":"ended","seasons":[]}]`, true, 4},
		{"old cartoon, not anime", `[{"tvdbId":1,"year":1999,"status":"ended","seasons":[]}]`, false, 4},
	} {
		var sent map[string]any
		var calls []string
		srv := fakeSonarrWith(t, tc.lookup, `[]`, &sent, &calls)
		c := New("sonarr", Config{URL: srv.URL, APIKey: "k", RootFolder: "/data/TV", QualityProfileID: 4, AnimeQualityProfileID: 4, OldAnimeQualityProfileID: 7})
		if _, err := c.AddSeries(context.Background(), 1, tc.anime, nil, nil, nil); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		srv.Close()
		if sent["qualityProfileId"] != tc.want {
			t.Errorf("%s: profile %v, want %v", tc.name, sent["qualityProfileId"], tc.want)
		}
	}
}

// A series Sonarr has but does not monitor is kept by hand: the request is refused and
// nothing is changed or searched.
func TestAddSeriesLeavesUnmonitoredAlone(t *testing.T) {
	var sent map[string]any
	var calls []string
	srv := fakeSonarrWith(t, `[]`, `[{"id":3,"tvdbId":1,"monitored":false,"seasons":[{"seasonNumber":1,"monitored":false}]}]`, &sent, &calls)
	defer srv.Close()
	c := New("sonarr", Config{URL: srv.URL, APIKey: "k", RootFolder: "/data/TV", QualityProfileID: 4})
	if _, err := c.AddSeries(context.Background(), 1, false, nil, nil, nil); err != ErrManagedByHand {
		t.Fatalf("err %v, want ErrManagedByHand", err)
	}
	for _, call := range calls {
		if call != "GET /api/v3/series" {
			t.Errorf("unexpected call %s", call)
		}
	}
}

// Sonarr's one-line-per-episode "unexpected considering the folder name" becomes one sentence.
func TestMessagesFoldsUnexpectedFolder(t *testing.T) {
	folder := "[Judas] Sono Bisque Doll wa Koi o Suru (My Dress-Up Darling) (Season 02) [1080p]"
	var msgs []string
	for e := 1; e <= 12; e++ {
		msgs = append(msgs, fmt.Sprintf("Episode 2x%02d was unexpected considering the %s folder name", e, folder))
	}
	q := QueueItem{ErrorMessage: "One or more episodes expected in this release were not imported or missing from the release", StatusMessages: []StatusMessage{{Title: "x.mkv", Messages: msgs}}}
	got := q.Messages()
	if !strings.Contains(got, "S2E1–E12") || strings.Contains(got, "2x05") || !strings.Contains(got, "Manual Import") {
		t.Errorf("got %q", got)
	}
	if got := episodeRange(1, []int{5, 1, 3}); got != "S1E1, E3, E5" {
		t.Errorf("gaps: %q", got)
	}
	// Other messages are untouched.
	if got := (QueueItem{ErrorMessage: "No files found"}).Messages(); got != "No files found" {
		t.Errorf("plain: %q", got)
	}
}

// A request for one special of a series Sonarr does not have: no season is monitored or
// searched whole, and the special is monitored and searched once Sonarr lists it (asking
// straight after the add found no episodes, so nothing was ever monitored).
func TestAddSeriesEpisodesOnly(t *testing.T) {
	var mu sync.Mutex
	var sent map[string]any
	var calls []string
	var monitored []any
	epCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+fmt.Sprint(body["name"]))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			w.Write([]byte(`[{"tvdbId":1,"seasons":[{"seasonNumber":0},{"seasonNumber":1}]}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			sent = body
			w.Write([]byte(`{"id":7}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			epCalls++
			if epCalls == 1 {
				w.Write([]byte(`[]`))
				return
			}
			w.Write([]byte(`[{"id":50,"seasonNumber":0,"episodeNumber":5},{"id":51,"seasonNumber":0,"episodeNumber":6},{"id":1,"seasonNumber":1,"episodeNumber":1}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
			monitored, _ = body["episodeIds"].([]any)
			w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
			w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	readyPoll.Store(time.Millisecond)
	c := New("sonarr", Config{URL: srv.URL, APIKey: "k", RootFolder: "/data/TV", QualityProfileID: 4})
	if _, err := c.AddSeries(context.Background(), 1, false, []int{-1}, []string{"S0E5"}, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		mu.Lock()
		done := monitored != nil
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for _, x := range sent["seasons"].([]any) {
		if m := x.(map[string]any); m["monitored"] == true {
			t.Errorf("season %v monitored, want none", m["seasonNumber"])
		}
	}
	if sent["monitorNewItems"] != "none" {
		t.Errorf("monitorNewItems %v, want none", sent["monitorNewItems"])
	}
	if len(monitored) != 1 || monitored[0] != float64(50) {
		t.Errorf("monitored %v, want [50] (S0E5 only)", monitored)
	}
	searches := 0
	for _, call := range calls {
		if strings.Contains(call, "SeasonSearch") || strings.Contains(call, "SeriesSearch") {
			t.Errorf("unexpected %s", call)
		}
		if strings.Contains(call, "EpisodeSearch") {
			searches++
		}
	}
	if searches != 1 {
		t.Errorf("episode searches %d, want 1; calls %v", searches, calls)
	}
}

// A movie Radarr already has (dropped, unmonitored, no file) is monitored again and
// searched; returning its id alone left the request doing nothing.
func TestAddMovieExistingIsMonitoredAndSearched(t *testing.T) {
	var calls []string
	var put map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+fmt.Sprint(body["name"]))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			w.Write([]byte(`[{"id":9,"tmdbId":550,"title":"Fight Club","monitored":false,"hasFile":false}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/movie/9":
			put = body
			w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
			w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := New("radarr", Config{URL: srv.URL, APIKey: "k", RootFolder: "/data/Movies", QualityProfileID: 4})
	id, err := c.AddMovie(context.Background(), 550, false)
	if err != nil || id != 9 {
		t.Fatalf("id %d err %v", id, err)
	}
	if put["monitored"] != true || put["title"] != "Fight Club" {
		t.Errorf("PUT %v, want the movie monitored", put)
	}
	if last := calls[len(calls)-1]; last != "POST /api/v3/command MoviesSearch" {
		t.Errorf("calls %v, want a MoviesSearch last", calls)
	}
}

// A series found by its TheTVDB id, and its episodes' air dates (the day as Sonarr has
// it), which date specials TMDB numbers differently.
func TestSeriesByTVDBAndAirDates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series":
			if r.URL.Query().Get("tvdbId") == "254653" {
				_, _ = w.Write([]byte(`[{"id":42,"title":"High School DxD"}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case "/api/v3/episode":
			if r.URL.Query().Get("seriesId") != "42" {
				t.Errorf("episodes of series %s", r.URL.Query().Get("seriesId"))
			}
			_, _ = w.Write([]byte(`[{"id":1,"seasonNumber":0,"episodeNumber":1,"airDate":"2012-04-11","airDateUtc":"2012-04-10T15:00:00Z"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New("sonarr", Config{URL: srv.URL, APIKey: "k"})
	ctx := context.Background()
	if id, err := c.SeriesIDByTVDB(ctx, 254653); err != nil || id != 42 {
		t.Fatalf("by tvdb: %d %v", id, err)
	}
	if id, err := c.SeriesIDByTVDB(ctx, 1); err != nil || id != 0 {
		t.Fatalf("unknown tvdb: %d %v", id, err)
	}
	eps, err := c.Episodes(ctx, 42)
	if err != nil || len(eps) != 1 || eps[0].AirDate != "2012-04-11" {
		t.Fatalf("episodes: %+v %v", eps, err)
	}
}

// A long show's episode list is over a megabyte (Pokemon: 1.4 MB). The reply used to be
// cut at 1 MB and handed to the JSON decoder: "unexpected end of JSON input", for every
// call about that series.
func TestLargeReplyIsReadWhole(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	const n = 3000
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%d,"seasonNumber":1,"episodeNumber":%d,"overview":%q}`, i, i, strings.Repeat("x", 500))
	}
	b.WriteString("]")
	body := b.String()
	if len(body) < 1<<20 {
		t.Fatalf("test reply is only %d bytes", len(body))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer srv.Close()
	c := New("sonarr", Config{URL: srv.URL, APIKey: "k"})
	eps, err := c.Episodes(context.Background(), 23)
	if err != nil || len(eps) != n {
		t.Fatalf("got %d episodes, err %v; want %d", len(eps), err, n)
	}
}

// A reply past the limit is an error that says so, not a JSON error from a cut body.
func TestOversizedReplyIsNamed(t *testing.T) {
	old := maxReply
	maxReply = 1 << 10
	defer func() { maxReply = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":1,"seasonNumber":1,"episodeNumber":1,"overview":"` + strings.Repeat("x", 4<<10) + `"}]`))
	}))
	defer srv.Close()
	c := New("sonarr", Config{URL: srv.URL, APIKey: "k"})
	_, err := c.Episodes(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "larger than") || strings.Contains(err.Error(), "JSON") {
		t.Fatalf("err %v, want one that says the reply is too large", err)
	}
}

// A new series asked for with a whole season and single specials: the season search (the
// pack finder's indexer searches) can outlast the wait for Sonarr's episodes, and the
// specials were then monitored on a context already expired, so never.
func TestSingleEpisodesMonitoredAfterALongSeasonSearch(t *testing.T) {
	var mu sync.Mutex
	var monitored []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/lookup":
			w.Write([]byte(`[{"tvdbId":1,"seasons":[{"seasonNumber":0},{"seasonNumber":1}]}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/series":
			w.Write([]byte(`{"id":7}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			w.Write([]byte(`[{"id":50,"seasonNumber":0,"episodeNumber":5},{"id":1,"seasonNumber":1,"episodeNumber":1}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
			monitored, _ = body["episodeIds"].([]any)
			w.Write([]byte(`[]`))
		default:
			w.Write([]byte(`{"id":1}`))
		}
	}))
	defer srv.Close()
	readyPoll.Store(time.Millisecond)
	oldWait := readyTimeout.Load()
	readyTimeout.Store(300 * time.Millisecond)
	defer readyTimeout.Store(oldWait)
	c := New("sonarr", Config{URL: srv.URL, APIKey: "k", RootFolder: "/data/TV", QualityProfileID: 4})
	// The season search takes longer than the whole wait.
	c.SetPackFinder(func(ctx context.Context, seriesID, season int) bool {
		time.Sleep(600 * time.Millisecond)
		return true
	})
	if _, err := c.AddSeries(context.Background(), 1, false, []int{1}, []string{"S0E5"}, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400; i++ {
		mu.Lock()
		done := monitored != nil
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(monitored) != 1 || monitored[0] != float64(50) {
		t.Fatalf("monitored %v, want [50] (S0E5)", monitored)
	}
}
