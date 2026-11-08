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

// High School DxD's shape: four seasons (2012, 2013, 2015, 2018), OVAs that came out
// between them, shorts that came out around an OVA (one entry per kind, not alternating),
// specials not in the library, and one with no date.
func TestSectionsInReleaseOrder(t *testing.T) {
	seasons := []seasonIn{
		{ID: "s3", Title: "Season 3", Index: 3, Start: "2015-04-04", Count: 12},
		{ID: "s1", Title: "Season 1", Index: 1, Start: "2012-01-06", Count: 12},
		{ID: "s2", Title: "Season 2", Index: 2, Start: "2013-07-07", Count: 12},
		{ID: "s4", Title: "Season 4", Index: 4, Start: "2018-04-10", Count: 13},
	}
	specials := []specialIn{
		{ID: "ova1", Title: "Highschool DxD OVA 1", Date: "2012-04-11"},
		{ID: "ova2", Title: "Highschool DxD OVA 2", Date: "2012-07-25"},
		{ID: "new", Title: "New OVA", Date: "2015-03-10"},
		{ID: "born", Title: "BorN OVA", Date: "2015-09-16"},
		{ID: "short1", Title: "Rias and Akeno, Girl Fight!?", Date: "2015-07-24"},
		{ID: "short2", Title: "Steamy Grayfia", Date: "2015-12-25"},
		{ID: "hero0", Title: "Hero Episode 0", Date: "2018-03-21"},
		{ID: "udon", Title: "Let's Make Udon", Date: "2012-07-25", Virtual: true},
		{ID: "nodate", Title: "Mystery Special"},
	}
	got := describe(buildSections(seasons, specials, nil))
	want := "season:Season 1 | specials:OVAs[ova1 ova2] | season:Season 2 | specials:OVA[new] | season:Season 3 | specials:Specials[short1 short2 hero0] | specials:OVA[born] | season:Season 4 | specials:More specials[nodate] | specials:Missing specials[udon]"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

// Before the first season (a prologue OVA); films each an entry of their own, splitting a
// run of specials; a film with no date at the end; no specials at all is just the seasons.
func TestSectionsFilmsAndEdges(t *testing.T) {
	year := 2020
	film := func(id, title, date string) movieIn {
		it := model.Item{ID: id, Title: title, Type: model.TypeMovie}
		if date == "" {
			it.Year = &year
		}
		return movieIn{Item: it, Date: date}
	}
	seasons := []seasonIn{{ID: "s1", Index: 1, Start: "2020-04-01", Count: 12}, {ID: "s2", Index: 2, Start: "2023-01-01", Count: 12}}
	specials := []specialIn{
		{ID: "pro", Title: "Prologue", Date: "2019-12-01"},
		{ID: "sp1", Title: "Recap", Date: "2021-01-01"},
		{ID: "sp2", Title: "Beach Episode", Date: "2022-06-01"},
	}
	movies := []movieIn{film("m1", "The Movie", "2021-08-01"), film("m2", "Undated Film", "")}
	got := describe(buildSections(seasons, specials, movies))
	want := "specials:Special[pro] | season:Season 1 | specials:Special[sp1] | movie:The Movie | specials:Special[sp2] | season:Season 2 | movie:Undated Film"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if got := describe(buildSections(seasons, nil, nil)); got != "season:Season 1 | season:Season 2" {
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
	ep("ova", "sea0", "Show OVA", "2012-09-01", 1)
	ep("odd", "sea0", "Odd Special", "", 2)
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
	want := "season:Season 1 | specials:OVA[ova] | season:Season 2 | movie:Show: The Movie | specials:More specials[odd]"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	for _, sec := range out.Sections {
		if sec.Kind == "specials" && sec.SeasonID != "sea0" {
			t.Errorf("%s: season %q, want the Specials season", sec.Title, sec.SeasonID)
		}
		if sec.Kind == "movie" && (sec.Item == nil || sec.Item.ID != "film" || !sec.Item.HasMedia) {
			t.Errorf("the film's entry: %+v", sec.Item)
		}
	}
}
