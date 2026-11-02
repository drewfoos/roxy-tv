package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

func fixture(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func TestTMDBMovieResult(t *testing.T) {
	var m tmdbMovie
	fixture(t, "tmdb_movie.json", &m)
	r := NewTMDB("k").movieResult(&m)

	if r.Provider != "tmdb" || r.ID != "526702" {
		t.Errorf("provider/id = %s/%s", r.Provider, r.ID)
	}
	if r.Title != "KonoSuba: God's Blessing on This Wonderful World! Legend of Crimson" || r.Year != 2019 {
		t.Errorf("title/year = %q/%d", r.Title, r.Year)
	}
	if r.Premiered != "2019-08-30" || r.Tagline != "A legend of crimson!" || r.Status != "Released" {
		t.Errorf("premiered/tagline/status = %q/%q/%q", r.Premiered, r.Tagline, r.Status)
	}
	if r.CommunityRating != 7.8 {
		t.Errorf("rating = %v, want 7.8", r.CommunityRating)
	}
	if len(r.Genres) != 3 || r.Genres[1] != "Comedy" {
		t.Errorf("genres = %v", r.Genres)
	}
	if r.Studio != "J.C.STAFF" || r.OfficialRating != "PG-13" {
		t.Errorf("studio/rating = %q/%q", r.Studio, r.OfficialRating)
	}
	if r.ProviderIDs["imdb"] != "tt9381180" || r.ProviderIDs["tmdb"] != "526702" {
		t.Errorf("provider ids = %v", r.ProviderIDs)
	}
	want := map[string]string{
		"poster":   "https://image.tmdb.org/t/p/original/poster.jpg",
		"backdrop": "https://image.tmdb.org/t/p/original/backdrop.jpg",
		"logo":     "https://image.tmdb.org/t/p/w500/logo-en.png",
	}
	for k, v := range want {
		if r.Images[k] != v {
			t.Errorf("images[%s] = %q, want %q", k, r.Images[k], v)
		}
	}
}

func TestTMDBTVResultAndAbsoluteMapping(t *testing.T) {
	var s tmdbTV
	fixture(t, "tmdb_tv.json", &s)
	r := NewTMDB("k").tvResult(&s)

	if r.Title != "KonoSuba: God's Blessing on This Wonderful World!" || r.Year != 2016 {
		t.Errorf("title/year = %q/%d", r.Title, r.Year)
	}
	if r.Status != "Continuing" || r.Studio != "Tokyo MX" || r.OfficialRating != "TV-14" {
		t.Errorf("status/studio/rating = %q/%q/%q", r.Status, r.Studio, r.OfficialRating)
	}
	if r.ProviderIDs["tvdb"] != "301311" || r.ProviderIDs["imdb"] != "tt5370118" {
		t.Errorf("provider ids = %v", r.ProviderIDs)
	}
	if r.EpisodeCount != 31 || len(r.Seasons) != 4 || r.Seasons[1].Poster != "https://image.tmdb.org/t/p/w780/s1.jpg" || r.Seasons[3].Poster != "" {
		t.Errorf("seasons = %+v", r.Seasons)
	}
	if _, ok := r.Images["logo"]; ok {
		t.Error("no logos in fixture, but a logo was set")
	}
	cases := []struct {
		abs, season, ep int
		ok              bool
	}{
		{1, 1, 1, true}, {10, 1, 10, true}, {11, 2, 1, true}, {20, 2, 10, true}, {21, 3, 1, true}, {31, 3, 11, true}, {32, 0, 0, false}, {0, 0, 0, false},
	}
	for _, c := range cases {
		se, ep, ok := r.AbsoluteToAired(c.abs)
		if se != c.season || ep != c.ep || ok != c.ok {
			t.Errorf("AbsoluteToAired(%d) = (%d, %d, %v), want (%d, %d, %v)", c.abs, se, ep, ok, c.season, c.ep, c.ok)
		}
	}
}

func TestTMDBSeasonResult(t *testing.T) {
	var s tmdbSeason
	fixture(t, "tmdb_season.json", &s)
	se := NewTMDB("k").seasonResult(&s)
	if se.Number != 2 || se.Poster != "https://image.tmdb.org/t/p/w780/s2.jpg" || len(se.Episodes) != 2 {
		t.Fatalf("season = %+v", se)
	}
	e := se.Episodes[0]
	if e.Season != 2 || e.Episode != 1 || e.Title != "Give Me Deliverance from this Judicial Injustice!" || e.AirDate != "2017-01-12" || e.Thumb != "https://image.tmdb.org/t/p/w780/s2e1.jpg" {
		t.Errorf("episode 1 = %+v", e)
	}
	if se.Episodes[1].Thumb != "" {
		t.Errorf("null still_path should give no thumb, got %q", se.Episodes[1].Thumb)
	}
}

// TestTMDBMatchFlow drives search -> detail against a fake server, and the /find path
// for a TVDB id, without touching the network.
func TestTMDBMatchFlow(t *testing.T) {
	movie, _ := os.ReadFile(filepath.Join("testdata", "tmdb_movie.json"))
	tv, _ := os.ReadFile(filepath.Join("testdata", "tmdb_tv.json"))
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.URL.Query().Get("api_key") != "k" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/search/movie":
			if r.URL.Query().Get("query") != "Kono Subarashii Sekai ni Shukufuku o! Kurenai Densetsu" {
				t.Errorf("query = %q", r.URL.Query().Get("query"))
			}
			w.Write([]byte(`{"results":[
				{"id":1,"title":"Some Other Film","original_title":"x","release_date":"2019-01-01"},
				{"id":526702,"title":"KonoSuba: God's Blessing on This Wonderful World! Legend of Crimson","original_title":"Kono Subarashii Sekai ni Shukufuku wo! Kurenai Densetsu","release_date":"2019-08-30"}]}`))
		case "/movie/526702":
			w.Write(movie)
		case "/find/301311":
			if r.URL.Query().Get("external_source") != "tvdb_id" {
				t.Errorf("external_source = %q", r.URL.Query().Get("external_source"))
			}
			w.Write([]byte(`{"movie_results":[],"tv_results":[{"id":65844}]}`))
		case "/tv/65844":
			w.Write(tv)
		case "/search/tv":
			w.Write([]byte(`{"results":[{"id":9,"name":"Completely Unrelated","original_name":"","first_air_date":"2024-01-01"}]}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"status_message":"not found"}`))
		}
	}))
	defer srv.Close()
	p := NewTMDB("k")
	p.base = srv.URL

	year := 2019
	r, err := p.Match(context.Background(), &model.Item{Type: model.TypeMovie, Title: "Kono Subarashii Sekai ni Shukufuku o! Kurenai Densetsu", Year: &year}, Hints{})
	if err != nil || r == nil || r.ID != "526702" {
		t.Fatalf("movie match = %+v, %v", r, err)
	}

	r, err = p.Match(context.Background(), &model.Item{Type: model.TypeSeries, Title: "Whatever", ProviderIDs: map[string]string{"tvdb": "301311"}}, Hints{})
	if err != nil || r == nil || r.ID != "65844" || r.ProviderIDs["tvdb"] != "301311" {
		t.Fatalf("tvdb find match = %+v, %v", r, err)
	}

	y := 2024
	r, err = p.Match(context.Background(), &model.Item{Type: model.TypeSeries, Title: "Roxy Test Show", Year: &y}, Hints{})
	if err != nil || r != nil {
		t.Fatalf("unrelated search hit must not match: %+v, %v", r, err)
	}
	if p.key == "" {
		t.Fatal("key lost")
	}
	if len(calls) == 0 {
		t.Fatal("no calls recorded")
	}
}

func TestLimiterAnd429Retry(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	var out struct{ OK bool }
	start := time.Now()
	if err := getJSON(context.Background(), srv.Client(), newLimiter(2, time.Second), http.MethodGet, srv.URL, nil, nil, &out); err != nil || !out.OK {
		t.Fatalf("err = %v, out = %+v", err, out)
	}
	if n != 2 || time.Since(start) < 900*time.Millisecond {
		t.Errorf("expected one retry after ~1s, got %d calls in %s", n, time.Since(start))
	}
}

// A folder named after a release title the main title lacks: TMDB's search finds
// "Pokémon the Movie 2000" for "Pokemon - The Power of One" (1999), which only its
// US/UK alternative title matches.
func TestTMDBMatchesByAlternativeTitle(t *testing.T) {
	alt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/movie":
			w.Write([]byte(`{"results":[{"id":12599,"title":"Pokémon the Movie 2000","original_title":"劇場版ポケットモンスター 幻のポケモン ルギア爆誕","release_date":"1999-07-17"}]}`))
		case "/movie/12599/alternative_titles":
			alt++
			w.Write([]byte(`{"id":12599,"titles":[{"iso_3166_1":"US","title":"Pokémon the Movie 2000: The Power of One"}]}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"status_message":"not found"}`))
		}
	}))
	defer srv.Close()
	p := NewTMDB("k")
	p.base = srv.URL
	year := 1999
	id, err := p.search(context.Background(), &model.Item{Type: model.TypeMovie, Title: "Pokemon - The Power of One", Year: &year}, true)
	if err != nil || id != 12599 {
		t.Fatalf("id = %d, %v", id, err)
	}
	if alt != 1 {
		t.Errorf("alternative titles fetched %d times", alt)
	}
}

// A remake shares the original's title; a movie more than two years off is not a match.
func TestPickBestRejectsDistantMovieYears(t *testing.T) {
	cands := []candidate{{names: []string{"Pokémon: Mewtwo Strikes Back—Evolution"}, year: 2019}}
	y := 1998
	if i := pickBest("Pokemon - Mewtwo Strikes Back", &y, cands, movieYearGap); i != -1 {
		t.Fatalf("1998 folder matched the 2019 remake")
	}
	if i := pickBest("Pokemon - Mewtwo Strikes Back", &y, cands, 0); i != 0 {
		t.Fatalf("without the movie rule the title still matches (series keep the soft penalty)")
	}
}
