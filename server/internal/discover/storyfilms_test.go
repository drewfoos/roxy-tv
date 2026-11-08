package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// storyWorld: a library with Bunny Girl Senpai (TMDB 100, requested and in the library)
// whose relations name Dreaming Girl (a story film, not in the library), a side-story film,
// Knapsack Kid (a story film already in the library) and an OVA; TMDB and Radarr fakes.
type storyWorld struct {
	st     *store.Store
	s      *Service
	adds   atomic.Int32 // films Radarr was asked to add
	failRA atomic.Bool  // Radarr answers 500 to adds
	// onAdd runs inside Radarr's add, before it answers (what Roxy had saved by then).
	onAdd func()
}

func newStoryWorld(t *testing.T) *storyWorld {
	t.Helper()
	ctx := context.Background()
	w := &storyWorld{}
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w.st = st
	tmdb := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/tv/100":
			rw.Write([]byte(`{"id":100,"name":"Bunny Girl Senpai","first_air_date":"2018-10-04","poster_path":"/p.jpg","external_ids":{"tvdb_id":345596}}`))
		case r.URL.Path == "/movie/200":
			rw.Write([]byte(`{"id":200,"title":"Rascal Does Not Dream of a Dreaming Girl","release_date":"2019-06-15","poster_path":"/d.jpg"}`))
		case r.URL.Path == "/search/movie":
			q := r.URL.Query().Get("query")
			switch {
			case strings.Contains(q, "Dreaming Girl"):
				rw.Write([]byte(`{"results":[
					{"id":201,"title":"Rascal Does Not Dream of a Dreaming Girl","release_date":"2009-01-01","genre_ids":[16]},
					{"id":202,"title":"Rascal Does Not Dream of a Dreaming Girl","release_date":"2019-03-01","genre_ids":[18]},
					{"id":200,"title":"Rascal Does Not Dream of a Dreaming Girl","release_date":"2019-06-15","genre_ids":[16,18]}]}`))
			case strings.Contains(q, "Knapsack"):
				rw.Write([]byte(`{"results":[{"id":300,"title":"Rascal Does Not Dream of a Knapsack Kid","release_date":"2023-12-01","genre_ids":[16]}]}`))
			default:
				t.Errorf("searched TMDB for %q", q)
				rw.Write([]byte(`{"results":[]}`))
			}
		default:
			rw.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(tmdb.Close)
	radarr := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			rw.Write([]byte(`[]`))
		case r.URL.Path == "/api/v3/movie/lookup/tmdb":
			rw.Write([]byte(`{"title":"film","tmdbId":` + r.URL.Query().Get("tmdbId") + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/movie":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			if w.onAdd != nil {
				w.onAdd()
			}
			if w.failRA.Load() {
				http.Error(rw, "down", 500)
				return
			}
			w.adds.Add(1)
			rw.Write([]byte(`{"id":7}`))
		default:
			rw.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(radarr.Close)
	cfg := &config.Config{RadarrURL: radarr.URL, RadarrAPIKey: "k", RadarrRootFolder: "/data/Anime Movies", RadarrQualityProfile: "4"}
	w.s = New(st, cfg, metadata.NewTMDBAt("k", tmdb.URL))

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
	if err := st.UpsertRequest(ctx, &store.Request{ID: "req-tv", MediaType: "tv", TMDBID: 100, Title: "Bunny Girl Senpai", UserID: user.ID, UserName: "drewfoos", Status: "sent"}); err != nil {
		t.Fatal(err)
	}
	return w
}

// A requested series in the library brings its story films: Dreaming Girl is asked of
// Radarr once (TMDB's animated film of that title and year: not the 2009 one, not a
// live-action one of the same year); a side-story film is not; a story film already in
// the library is not; after the request is deleted it is not asked for again.
func TestStoryFilmsComeWithTheirSeries(t *testing.T) {
	ctx := context.Background()
	w := newStoryWorld(t)
	w.s.RequestStoryFilms(ctx)
	if n := w.adds.Load(); n != 1 {
		t.Fatalf("radarr asked %d times, want once", n)
	}
	film, err := w.st.GetRequest(ctx, "movie", 200)
	if err != nil || film == nil || film.Status != "sent" || film.UserName != "drewfoos" {
		t.Fatalf("film request %+v, %v; want TMDB 200 sent, by the series' requester", film, err)
	}
	if err := w.st.DeleteRequest(ctx, film.ID); err != nil {
		t.Fatal(err)
	}
	w.s.storyFilmTried = nil
	w.s.RequestStoryFilms(ctx)
	if n := w.adds.Load(); n != 1 {
		t.Fatalf("asked again after the request was deleted: %d adds", n)
	}
}

// Several runs at once (the requester is entered from more than one place over time):
// however they interleave, the film is sent once and requested once. Every run passes
// the per-relation lookup gate here (it is reset), so only the claim stops the others.
func TestStoryFilmSentOnceUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	w := newStoryWorld(t)
	rels, _ := w.st.RelationsFor(ctx, "series")
	var rel store.ItemRelation
	for _, r := range rels {
		if r.ProviderID == "11" {
			rel = r
		}
	}
	series, _ := w.st.GetRequestByID(ctx, "req-tv")
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w.s.mu.Lock()
			w.s.storyFilmTried = nil // as if each run were the first today
			w.s.mu.Unlock()
			w.s.requestStoryFilm(ctx, series, rel)
		}()
	}
	close(start)
	wg.Wait()
	if n := w.adds.Load(); n != 1 {
		t.Fatalf("radarr asked %d times by 10 concurrent runs, want once", n)
	}
	all, _ := w.st.ListRequests(ctx)
	films := 0
	for _, r := range all {
		if r.MediaType == "movie" {
			films++
		}
	}
	if films != 1 {
		t.Fatalf("%d film requests saved, want 1", films)
	}
}

// The request is saved before Radarr is asked (so a send can never happen unrecorded),
// and when Radarr fails it stays saved, failed, and is sent once Radarr is back
// (RetryPending), not dropped and not requested twice.
func TestStoryFilmSavedBeforeSendAndRetried(t *testing.T) {
	ctx := context.Background()
	w := newStoryWorld(t)
	savedAtSend := false
	w.onAdd = func() {
		r, err := w.st.GetRequest(ctx, "movie", 200)
		savedAtSend = err == nil && r != nil
	}
	w.failRA.Store(true)
	w.s.RequestStoryFilms(ctx)
	if !savedAtSend {
		t.Fatal("Radarr was asked before the request was saved")
	}
	film, _ := w.st.GetRequest(ctx, "movie", 200)
	if film == nil || film.Status != "failed" {
		t.Fatalf("after Radarr failed: %+v, want the request kept as failed", film)
	}
	// A later story-film run does not make a second request.
	w.s.storyFilmTried = nil
	w.s.RequestStoryFilms(ctx)
	all, _ := w.st.ListRequests(ctx)
	films := 0
	for _, r := range all {
		if r.MediaType == "movie" {
			films++
		}
	}
	if films != 1 {
		t.Fatalf("%d film requests, want 1", films)
	}
	w.failRA.Store(false)
	w.s.RetryPending(ctx)
	film, _ = w.st.GetRequest(ctx, "movie", 200)
	if film == nil || film.Status != "sent" || w.adds.Load() != 1 {
		t.Fatalf("after Radarr came back: %+v, %d adds; want sent once", film, w.adds.Load())
	}
}

// ClaimSetting: of many callers at once, exactly one wins.
func TestClaimSettingOneWinner(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := st.ClaimSetting(ctx, "storyfilm:1", "x"); err == nil && ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d winners, want 1", wins.Load())
	}
}

// pickFilm: TMDB's film for an AniList story film only when the search says clearly
// which; a wrong film would be downloaded with nobody asking.
func TestPickFilm(t *testing.T) {
	anim := []int{16}
	e := func(id int, title, date string, genres []int) tmdbListEntry {
		return tmdbListEntry{ID: id, Title: title, ReleaseDate: date, GenreIDs: genres}
	}
	const dg = "Rascal Does Not Dream of a Dreaming Girl"
	cases := []struct {
		name  string
		title string
		year  int
		res   []tmdbListEntry
		want  int
	}{
		{"the one match", dg, 2019, []tmdbListEntry{e(1, dg, "2019-06-15", anim), e(2, "Dreaming", "2019-01-01", anim)}, 1},
		{"a year off is still it", dg, 2020, []tmdbListEntry{e(1, dg, "2019-06-15", anim)}, 1},
		{"no close title", "Something Else Entirely", 2019, []tmdbListEntry{e(1, dg, "2019-06-15", anim)}, 0},
		{"no film that year", dg, 2012, []tmdbListEntry{e(1, dg, "2019-06-15", anim)}, 0},
		{"live-action remake of the same name and year is not it", "Your Name.", 2016,
			[]tmdbListEntry{e(1, "Your Name.", "2016-08-26", []int{16, 10749}), e(2, "Your Name.", "2016-11-01", []int{10749, 18})}, 1},
		{"only a live-action one: none", "Your Name.", 2016, []tmdbListEntry{e(2, "Your Name.", "2016-11-01", []int{10749})}, 0},
		{"the exact year beats a neighbour's", "Ghost in the Shell", 1995,
			[]tmdbListEntry{e(1, "Ghost in the Shell", "1996-03-01", anim), e(2, "Ghost in the Shell", "1995-11-18", anim)}, 2},
		{"two animated films of one name and year: ambiguous", "Kizumonogatari", 2016,
			[]tmdbListEntry{e(1, "Kizumonogatari", "2016-01-08", anim), e(2, "Kizumonogatari", "2016-08-19", anim)}, 0},
		{"a compilation titled alike but not the same", "Attack on Titan: The Final Season", 2023,
			[]tmdbListEntry{e(1, "Attack on Titan: The Final Season", "2023-03-04", anim), e(2, "Attack on Titan: The Final Season - Compilation", "2023-11-04", anim)}, 1},
		{"the same result twice counts once", dg, 2019, []tmdbListEntry{e(1, dg, "2019-06-15", anim), e(1, dg, "2019-06-15", anim)}, 1},
		{"no genres listed: not held against it", dg, 2019, []tmdbListEntry{e(1, dg, "2019-06-15", nil)}, 1},
	}
	for _, c := range cases {
		if got, why := pickFilm(c.title, c.year, c.res); got != c.want {
			t.Errorf("%s: got %d (%s), want %d", c.name, got, why, c.want)
		}
	}
}
