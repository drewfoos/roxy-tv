package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

func describe(secs []section) string {
	var parts []string
	for _, s := range secs {
		d := s.Kind + ":" + s.Title
		if s.Kind == "specials" {
			d += fmt.Sprint(s.EpisodeIDs)
		}
		parts = append(parts, d)
	}
	return strings.Join(parts, " | ")
}

// High School DxD's shape: four seasons; OVAs between them (by name, or by length: an
// episode-long special not called an OVA is one too, but its own entry); three-minute BD
// shorts, an undated special and missing ones all in "Specials" at the bottom, the ones
// the library has first, by date.
func TestSectionsInReleaseOrder(t *testing.T) {
	const min = 60 * 1000
	seasons := []seasonIn{
		{ID: "s3", Title: "Season 3", Index: 3, Start: "2015-04-04", Count: 12},
		{ID: "s1", Title: "Season 1", Index: 1, Start: "2012-01-06", Count: 12},
		{ID: "s2", Title: "Season 2", Index: 2, Start: "2013-07-07", Count: 12},
		{ID: "s4", Title: "Season 4", Index: 4, Start: "2018-04-17", Count: 12},
	}
	specials := []specialIn{
		{ID: "ova1", Title: "Highschool DxD OVA 1", Date: "2012-09-20", RuntimeMs: 24 * min},
		{ID: "ova2", Title: "Highschool DxD OVA 2", Date: "2013-05-21", RuntimeMs: 24 * min},
		{ID: "new", Title: "New OVA", Date: "2015-03-10", RuntimeMs: 3 * min}, // named an OVA: placed whatever its length
		{ID: "short2", Title: "Steamy Grayfia", Date: "2015-12-25", RuntimeMs: 4 * min},
		{ID: "short1", Title: "Rias and Akeno, Girl Fight!", Date: "2015-07-24", RuntimeMs: 3 * min},
		{ID: "born", Title: "BorN OVA", Date: "2015-12-09", RuntimeMs: 24 * min},
		{ID: "hero0", Title: "Hero Episode 0", Date: "2018-04-10", RuntimeMs: 24 * min},
		{ID: "nodate", Title: "Mystery Special", RuntimeMs: 24 * min},
		{ID: "harem", Title: "Before Harem Special", Date: "2011-12-19", Virtual: true},
		{ID: "unknown", Title: "Making Of", Date: "2012-02-01"}, // no length known: an extra
	}
	got := describe(buildSections(seasons, specials, nil, 0))
	want := "season:Season 1 | specials:OVAs[ova1 ova2] | season:Season 2 | specials:New OVA[new] | season:Season 3 | specials:BorN OVA[born] | specials:Hero Episode 0[hero0] | season:Season 4 | specials:Specials[unknown short1 short2 nodate harem]"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

// KonoSuba: its OVAs are not named so, only their length says what they are; the film
// filed among the specials is an entry of its own (seriesSections), a film of the
// franchise splits a gap's OVAs; a film with no date goes last; no specials, just seasons.
func TestSectionsFilmsAndEdges(t *testing.T) {
	const min = 60 * 1000
	year := 2020
	film := func(id, title, date string) movieIn {
		it := model.Item{ID: id, Title: title, Type: model.TypeMovie}
		if date == "" {
			it.Year = &year
		}
		return movieIn{Item: it, Date: date}
	}
	seasons := []seasonIn{{ID: "s1", Index: 1, Start: "2016-01-14", Count: 10}, {ID: "s2", Index: 2, Start: "2017-01-12", Count: 10}, {ID: "s3", Index: 3, Start: "2024-04-10", Count: 11}}
	specials := []specialIn{
		{ID: "pro", Title: "Prologue", Date: "2015-12-01", RuntimeMs: 25 * min},
		{ID: "choker", Title: "God's Blessing on This Wonderful Choker!", Date: "2016-06-24", RuntimeMs: 24 * min},
		{ID: "art", Title: "God's Blessing on These Wonderful Works of Art!", Date: "2017-07-24", RuntimeMs: 24 * min},
		{ID: "red", Title: "Red Stream Explosion!", Date: "2025-04-25", RuntimeMs: 24 * min},
		{ID: "imp", Title: "Watch Out for Impostors!", Date: "2025-04-25", RuntimeMs: 24 * min},
		{ID: "late", Title: "Another One", Date: "2020-01-01", RuntimeMs: 24 * min},
	}
	movies := []movieIn{film("m1", "Legend of Crimson", "2019-08-30"), film("m2", "Undated Film", "")}
	got := describe(buildSections(seasons, specials, movies, 24*min))
	want := "specials:Prologue[pro] | season:Season 1 | specials:God's Blessing on This Wonderful Choker![choker] | season:Season 2 | specials:God's Blessing on These Wonderful Works of Art![art] | movie:Legend of Crimson | specials:Another One[late] | season:Season 3 | specials:OVAs[red imp] | movie:Undated Film"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if got := describe(buildSections(seasons, nil, nil, 0)); got != "season:Season 1 | season:Season 2 | season:Season 3" {
		t.Fatalf("seasons only: %s", got)
	}
}

// The endpoint over a real store: seasons and specials as the library has them, and a
// film the franchise's relations name that the library holds.
func TestSeriesSectionsEndpoint(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	lib := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: "/a"}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	movies := &model.Library{Name: "Anime Movies", Kind: model.KindMovies, Root: "/m"}
	if err := st.CreateLibrary(ctx, movies); err != nil {
		t.Fatal(err)
	}
	up := func(it *model.Item) {
		t.Helper()
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	file := func(id string) {
		t.Helper()
		if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "s-" + id, ItemID: id, Parts: []model.MediaPart{{ID: "p-" + id, Path: "/a/" + id + ".mkv"}}}, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	series := "series"
	up(&model.Item{ID: series, GUID: "g-series", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"})
	idx := func(n int) *int { return &n }
	up(&model.Item{ID: "sea0", GUID: "g-sea0", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series, IndexAired: idx(0), Title: "Specials"})
	up(&model.Item{ID: "sea1", GUID: "g-sea1", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series, IndexAired: idx(1), Title: "Season 1"})
	up(&model.Item{ID: "sea2", GUID: "g-sea2", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series, IndexAired: idx(2), Title: "Season 2"})
	ep := func(id, season, title, date string, n int) {
		sid := season
		up(&model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &sid, IndexAired: &n, Title: title})
		file(id)
		if date != "" {
			if err := st.SetAirDateIfUnset(ctx, id, date); err != nil {
				t.Fatal(err)
			}
		}
	}
	ep("e1", "sea1", "One", "2012-01-06", 1)
	ep("e2", "sea2", "Two", "2013-07-07", 1)
	ep24 := int64(24 * 60 * 1000) // the show's episodes: what a film-length special is measured against
	up(&model.Item{ID: "e1", GUID: "g-e1", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: ptr("sea1"), IndexAired: idx(1), Title: "One", RuntimeMs: &ep24})
	up(&model.Item{ID: "e2", GUID: "g-e2", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: ptr("sea2"), IndexAired: idx(1), Title: "Two", RuntimeMs: &ep24})
	ep("ova", "sea0", "Show OVA", "2012-09-01", 1)
	ep("odd", "sea0", "Odd Special", "", 2)
	// A film filed as a special (KonoSuba's Legend of Crimson, S00E05): an entry of its own.
	ep("crimson", "sea0", "Movie: Legend of Crimson", "2019-08-30", 5)
	long := int64(90 * 60 * 1000)
	up(&model.Item{ID: "crimson", GUID: "g-crimson", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: ptr("sea0"), IndexAired: idx(5), Title: "Movie: Legend of Crimson", RuntimeMs: &long})
	up(&model.Item{ID: "film", GUID: "g-film", LibraryID: movies.ID, Type: model.TypeMovie, Title: "Show: The Movie", ProviderIDs: map[string]string{"anilist": "555"}})
	file("film")
	if err := st.SetAirDateIfUnset(ctx, "film", "2014-06-01"); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceRelations(ctx, series, []store.ItemRelation{
		{Provider: "anilist", ProviderID: "555", Relation: "movie", Title: "Show: The Movie", Format: "MOVIE", Year: 2014},
		{Provider: "anilist", ProviderID: "999", Relation: "ova", Title: "Not Here OVA", Format: "OVA", Year: 2015},
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	req := httptest.NewRequest(http.MethodGet, "/v1/items/series/sections", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", series)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	s.seriesSections(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out struct{ Sections []section }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	got := describe(out.Sections)
	want := "season:Season 1 | specials:Show OVA[ova] | season:Season 2 | movie:Show: The Movie | movie:Movie: Legend of Crimson | specials:Specials[odd]"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	for _, sec := range out.Sections {
		if sec.Kind == "specials" && sec.SeasonID != "sea0" {
			t.Errorf("%s: season %q, want the Specials season", sec.Title, sec.SeasonID)
		}
		if sec.Kind == "movie" && (sec.Item == nil || sec.Item.ID != "film" && sec.Item.ID != "crimson" || !sec.Item.HasMedia) {
			t.Errorf("the film's entry: %+v", sec.Item)
		}
	}
}

func ptr(s string) *string { return &s }

// Where a special goes by its name and length, at the edges, for a show whose episode
// length is unknown (the 15-minute fallback): 15 minutes is an episode,
// a second less is not; an unknown length counts as short unless the name says OVA; a
// long recap is not story; nothing undated or missing is placed between seasons.
func TestSpecialPlacementByLength(t *testing.T) {
	const min, sec = 60 * 1000, 1000
	seasons := []seasonIn{{ID: "s1", Index: 1, Start: "2020-01-01", Count: 12}, {ID: "s2", Index: 2, Start: "2022-01-01", Count: 12}}
	sp := func(id, title string, runtime int64) specialIn {
		return specialIn{ID: id, Title: title, Date: "2021-01-01", RuntimeMs: runtime}
	}
	cases := []struct {
		sp      specialIn
		between bool
	}{
		{sp("at15", "Beach Day", 15*min), true},
		{sp("under15", "Beach Day", 15*min-sec), false},
		{sp("unknown", "Beach Day", 0), false},
		{sp("unknownOVA", "Show OVA", 0), true},
		{sp("shortOVA", "OVA: Short", 3*min), true},
		{sp("recap", "Recap Special", 24*min), false},
		{sp("digest", "Season 1 Digest", 46*min), false},
		{sp("recapOVA", "OVA Recap", 24*min), true}, // named an OVA: the name wins
		{specialIn{ID: "undated", Title: "Beach Day", RuntimeMs: 24 * min}, false},
		{specialIn{ID: "missing", Title: "Beach Day", Date: "2021-01-01", RuntimeMs: 24 * min, Virtual: true}, false},
	}
	for _, c := range cases {
		out := buildSections(append([]seasonIn(nil), seasons...), []specialIn{c.sp}, nil, 0)
		between := len(out) == 3 && out[1].Kind == "specials" && out[2].Kind == "season"
		bottom := len(out) == 3 && out[2].Kind == "specials" && out[2].Title == "Specials"
		if between != c.between || (!c.between && !bottom) {
			t.Errorf("%s: %s; want between=%v", c.sp.ID, describe(out), c.between)
		}
	}
}

// A special an hour or longer is a film's entry of its own; a second less is an OVA in
// its gap; an unknown length never makes one; an undated film goes after the seasons; a
// missing one (no file) is listed with the specials at the bottom, never as a film.
func TestFilmLengthSpecials(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	lib := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: "/a"}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	up := func(it *model.Item) {
		t.Helper()
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	idx := func(n int) *int { return &n }
	series := "series"
	up(&model.Item{ID: series, GUID: "g-series", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"})
	up(&model.Item{ID: "sea0", GUID: "g-sea0", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series, IndexAired: idx(0), Title: "Specials"})
	up(&model.Item{ID: "sea1", GUID: "g-sea1", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series, IndexAired: idx(1), Title: "Season 1"})
	up(&model.Item{ID: "sea2", GUID: "g-sea2", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series, IndexAired: idx(2), Title: "Season 2"})
	ep := func(id, season, title, date string, n int, runtime int64, virtual bool) {
		t.Helper()
		it := &model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: ptr(season), IndexAired: idx(n), Title: title, IsVirtual: virtual}
		if runtime > 0 {
			it.RuntimeMs = &runtime
		}
		up(it)
		if !virtual {
			if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "s-" + id, ItemID: id, Parts: []model.MediaPart{{ID: "p-" + id, Path: "/a/" + id + ".mkv"}}}, "{}"); err != nil {
				t.Fatal(err)
			}
		}
		if date != "" {
			if err := st.SetAirDateIfUnset(ctx, id, date); err != nil {
				t.Fatal(err)
			}
		}
	}
	const min, sec = int64(60 * 1000), int64(1000)
	ep("e1", "sea1", "One", "2016-01-14", 1, 24*min, false)
	ep("e2", "sea2", "Two", "2017-01-12", 1, 24*min, false)
	ep("at60", "sea0", "Legend of Crimson", "2016-06-01", 1, 60*min, false)
	ep("under60", "sea0", "Long Special", "2016-07-01", 2, 60*min-sec, false)
	ep("unknown", "sea0", "Mystery Film", "2016-08-01", 3, 0, false)
	ep("undated", "sea0", "Undated Film", "", 4, 95*min, false)
	ep("missing", "sea0", "Missing Film", "2016-09-01", 5, 95*min, true)

	s := &Server{store: st}
	req := httptest.NewRequest(http.MethodGet, "/v1/items/series/sections", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", series)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	s.seriesSections(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out struct{ Sections []section }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	got := describe(out.Sections)
	want := "season:Season 1 | movie:Legend of Crimson | specials:Long Special[under60] | season:Season 2 | specials:Specials[unknown missing] | movie:Undated Film"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

// Lengths are judged against the show's own episodes. A film needs an hour and 2.5 times
// an episode: a 90-minute special of a 43-minute drama, or of a show whose episodes are
// 88 minutes (Sherlock), is an episode; an unknown episode length never makes a film.
// Episode-length is half an episode, between 5 and 15 minutes: an 11-minute special of
// an 11-minute cartoon is an episode, a 3-minute short of a 24-minute anime is not.
func TestLengthsAgainstTheShow(t *testing.T) {
	const min, sec = int64(60 * 1000), int64(1000)
	for _, c := range []struct {
		name             string
		runtime, typical int64
		film             bool
	}{
		{"KonoSuba's film", 90 * min, 24 * min, true},
		{"JJK 0", 105 * min, 24 * min, true},
		{"Christmas special of a drama", 90 * min, 43 * min, false},
		{"Sherlock special", 90 * min, 88 * min, false},
		{"exactly 2.5 times", 60 * min, 24 * min, true},
		{"a second short of 2.5 times", 60*min - sec, 24 * min, false},
		{"2.5 times but under an hour", 55 * min, 22 * min, false},
		{"episode length unknown", 120 * min, 0, false},
	} {
		if got := isFilm(c.runtime, c.typical); got != c.film {
			t.Errorf("%s: film %v, want %v", c.name, got, c.film)
		}
	}
	for _, c := range []struct {
		name             string
		runtime, typical int64
		episode          bool
	}{
		{"cartoon special, 11-minute show", 11 * min, 11 * min, true},
		{"cartoon short, 11-minute show", 5*min - sec, 11 * min, false},
		{"anime OVA", 23 * min, 24 * min, true},
		{"DxD short", 3 * min, 24 * min, false},
		{"anime half-length special", 12 * min, 24 * min, true},
		{"drama 14-minute special", 14 * min, 43 * min, false}, // capped at 15
		{"drama 15-minute special", 15 * min, 43 * min, true},
		{"unknown show length, 15 minutes", 15 * min, 0, true},
		{"unknown show length, 10 minutes", 10 * min, 0, false}, // the fallback is 15, not the 5 floor
		{"unknown special length", 0, 24 * min, false},
	} {
		sp := specialIn{ID: "x", Title: "Something", RuntimeMs: c.runtime}
		if got := sp.isEpisode(episodeLength(c.typical)); got != c.episode {
			t.Errorf("%s: episode %v, want %v (threshold %d min)", c.name, got, c.episode, episodeLength(c.typical)/min)
		}
	}
	idx := func(id string) *string { return &id }
	rt := func(m int64) *int64 { return &m }
	eps := []model.Item{
		{ParentID: idx("s1"), RuntimeMs: rt(24 * min)}, {ParentID: idx("s1"), RuntimeMs: rt(23 * min)}, {ParentID: idx("s1"), RuntimeMs: rt(48 * min)},
		{ParentID: idx("s0"), RuntimeMs: rt(90 * min)},                   // a special: not counted
		{ParentID: idx("s1"), RuntimeMs: rt(200 * min), IsVirtual: true}, // a placeholder: not counted
		{ParentID: idx("s1")}, // no length: not counted
	}
	if got := typicalRuntime(eps, map[string]bool{"s1": true}); got != 24*min {
		t.Errorf("typical %d min, want the median of 23, 24, 48 = 24", got/min)
	}
	if got := typicalRuntime(nil, nil); got != 0 {
		t.Errorf("typical with no episodes: %d, want 0", got)
	}
}
