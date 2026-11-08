package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// A requested series in the library brings its story films: Bunny Girl Senpai's
// Dreaming Girl is asked of Radarr once (TMDB's film of that title and year, not a
// same-named one from another year); a side-story film is not; a story film already in
// the library is not; after the request is deleted it is not asked for again.
func TestStoryFilmsComeWithTheirSeries(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/tv/100":
			w.Write([]byte(`{"id":100,"name":"Bunny Girl Senpai","first_air_date":"2018-10-04","poster_path":"/p.jpg","external_ids":{"tvdb_id":345596}}`))
		case r.URL.Path == "/movie/200":
			w.Write([]byte(`{"id":200,"title":"Rascal Does Not Dream of a Dreaming Girl","release_date":"2019-06-15","poster_path":"/d.jpg"}`))
		case r.URL.Path == "/movie/300":
			w.Write([]byte(`{"id":300,"title":"Rascal Does Not Dream of a Knapsack Kid","release_date":"2023-12-01","poster_path":"/k.jpg"}`))
		case r.URL.Path == "/search/movie":
			q := r.URL.Query().Get("query")
			switch {
			case strings.Contains(q, "Dreaming Girl"):
				w.Write([]byte(`{"results":[
					{"id":201,"title":"Rascal Does Not Dream of a Dreaming Girl","release_date":"2009-01-01"},
					{"id":200,"title":"Rascal Does Not Dream of a Dreaming Girl","release_date":"2019-06-15"}]}`))
			case strings.Contains(q, "Knapsack"):
				w.Write([]byte(`{"results":[{"id":300,"title":"Rascal Does Not Dream of a Knapsack Kid","release_date":"2023-12-01"}]}`))
			default:
				t.Errorf("searched TMDB for %q", q)
				w.Write([]byte(`{"results":[]}`))
			}
		default:
			w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(tmdb.Close)

	var mu sync.Mutex
	var added []float64
	radarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			w.Write([]byte(`[]`))
		case r.URL.Path == "/api/v3/movie/lookup/tmdb":
			w.Write([]byte(`{"title":"film","tmdbId":` + r.URL.Query().Get("tmdbId") + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/movie":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			mu.Lock()
			added = append(added, m["tmdbId"].(float64))
			mu.Unlock()
			w.Write([]byte(`{"id":7}`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(radarr.Close)

	cfg := &config.Config{RadarrURL: radarr.URL, RadarrAPIKey: "k", RadarrRootFolder: "/data/Anime Movies", RadarrQualityProfile: "4"}
	s := New(st, cfg, metadata.NewTMDBAt("k", tmdb.URL))
	if !s.RequestsEnabled("movie") {
		t.Fatal("radarr not ready")
	}

	// The library: the series (matched to TMDB 100) and Knapsack Kid already there.
	tv := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: "/a"}
	mv := &model.Library{Name: "Anime Movies", Kind: model.KindMovies, Root: "/m"}
	for _, l := range []*model.Library{tv, mv} {
		if err := st.CreateLibrary(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	up := func(it *model.Item) {
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	up(&model.Item{ID: "series", GUID: "g-series", LibraryID: tv.ID, Type: model.TypeSeries, Title: "Bunny Girl Senpai", ProviderIDs: map[string]string{"tmdb": "100"}})
	up(&model.Item{ID: "knap", GUID: "g-knap", LibraryID: mv.ID, Type: model.TypeMovie, Title: "Knapsack Kid", ProviderIDs: map[string]string{"anilist": "13"}})
	if err := st.ReplaceRelations(ctx, "series", []store.ItemRelation{
		{Provider: "anilist", ProviderID: "11", Relation: "movie", Title: "Rascal Does Not Dream of a Dreaming Girl", Format: "MOVIE", Year: 2019, Story: true},
		{Provider: "anilist", ProviderID: "12", Relation: "movie", Title: "Beach Side Story", Format: "MOVIE", Year: 2020},
		{Provider: "anilist", ProviderID: "13", Relation: "movie", Title: "Rascal Does Not Dream of a Knapsack Kid", Format: "MOVIE", Year: 2023, Story: true},
		{Provider: "anilist", ProviderID: "14", Relation: "ova", Title: "An OVA", Format: "OVA", Year: 2020, Story: true},
	}); err != nil {
		t.Fatal(err)
	}
	user := &model.User{Name: "drewfoos", Role: model.RoleAdmin}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	series := &store.Request{ID: "req-tv", MediaType: "tv", TMDBID: 100, Title: "Bunny Girl Senpai", UserID: user.ID, UserName: "drewfoos", Status: "sent"}
	if err := st.UpsertRequest(ctx, series); err != nil {
		t.Fatal(err)
	}

	s.RequestStoryFilms(ctx)
	mu.Lock()
	got := append([]float64(nil), added...)
	mu.Unlock()
	if len(got) != 1 || got[0] != 200 {
		t.Fatalf("radarr asked for %v, want only TMDB 200 (Dreaming Girl of 2019)", got)
	}
	film, err := st.GetRequest(ctx, "movie", 200)
	if err != nil || film == nil || film.Status != "sent" || film.UserName != "drewfoos" {
		t.Fatalf("film request %+v, %v; want sent, by the series' requester", film, err)
	}

	// Deleted by someone: not asked for again, even once the lookup may run again.
	if err := st.DeleteRequest(ctx, film.ID); err != nil {
		t.Fatal(err)
	}
	s.storyFilmTried = nil
	s.RequestStoryFilms(ctx)
	mu.Lock()
	defer mu.Unlock()
	if len(added) != 1 {
		t.Fatalf("asked again after the request was deleted: %v", added)
	}
}

// pickFilm: the closest title within a year of AniList's; nothing when no title is close.
func TestPickFilm(t *testing.T) {
	res := []tmdbListEntry{
		{ID: 1, Title: "Rascal Does Not Dream of a Dreaming Girl", ReleaseDate: "2019-06-15"},
		{ID: 2, Title: "Dreaming", ReleaseDate: "2019-01-01"},
		{ID: 3, Title: "Rascal Does Not Dream of a Dreaming Girl", ReleaseDate: "2005-01-01"},
	}
	if got := pickFilm("Rascal Does Not Dream of a Dreaming Girl", 2019, res); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
	if got := pickFilm("Rascal Does Not Dream of a Dreaming Girl", 2020, res); got != 1 {
		t.Errorf("a year off: got %d, want 1", got)
	}
	if got := pickFilm("Something Else Entirely", 2019, res); got != 0 {
		t.Errorf("no close title: got %d, want 0", got)
	}
	if got := pickFilm("Rascal Does Not Dream of a Dreaming Girl", 2012, res); got != 0 {
		t.Errorf("no film that year: got %d, want 0", got)
	}
}
