package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// The library's specials are numbered as their files are (TheTVDB's count); the list
// they are ticked on is TMDB's. Which special is which goes by title, then air date.
func TestHeldSpecials(t *testing.T) {
	base := []Episode{
		{Number: 1, Title: "God's Blessing on This Wonderful Choker!", AirDate: "2016-06-24"},
		{Number: 2, Title: "God's Blessing on These Wonderful Works of Art!", AirDate: "2017-07-24"},
		{Number: 3, Title: "A Recap", AirDate: "2018-01-01"},
		{Number: 4, Title: "Bonus Stage", AirDate: "2019-02-02"},
		{Number: 5, Title: "Episode 5", AirDate: "2020-03-03"},
		{Number: 6, Title: "New Year's Short", AirDate: "2021-01-01"},
	}
	for _, c := range []struct {
		name string
		lib  []store.SpecialFile
		want []int
	}{
		{"nothing in the library", nil, nil},
		{"by title, whatever the file's number", []store.SpecialFile{
			{Number: 3, Title: "God's Blessing on This Wonderful Choker!", AirDate: "2016-06-24"},
			{Number: 4, Title: "God's Blessing on These Wonderful Works of Art!"},
		}, []int{1, 2}},
		{"by air date when the file's title is its own", []store.SpecialFile{
			{Number: 9, Title: "Extra Stage (BD)", AirDate: "2019-02-02"},
		}, []int{4}},
		{"by number when nothing else tells them apart", []store.SpecialFile{
			{Number: 5, Title: "Special 5"},
		}, []int{5}},
		{"a film filed under another special's number is not that special", []store.SpecialFile{
			{Number: 6, Title: "The Film", AirDate: "2021-12-24"},
		}, nil},
		{"a special the list does not have ticks nothing", []store.SpecialFile{
			{Number: 12, Title: "Commentary", AirDate: "2022-05-05"},
		}, nil},
	} {
		var got []int
		held := heldSpecials(base, c.lib)
		for _, e := range base {
			if held[e.Number] {
				got = append(got, e.Number)
			}
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: held %v, want %v", c.name, got, c.want)
		}
	}
}

// Through Episodes(): the OVA on disk as S00E03 is TMDB's special 1. It used to tick
// special 3 (a recap the library does not have) and leave 1 looking missing. Regular
// seasons still go by the number.
func TestSpecialsHeldAreTickedByTitleNotNumber(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	tmdb := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tv/100/season/0":
			rw.Write([]byte(`{"episodes":[
				{"episode_number":1,"name":"The Wonderful Choker","air_date":"2016-06-24"},
				{"episode_number":2,"name":"Works of Art","air_date":"2017-07-24"},
				{"episode_number":3,"name":"A Recap","air_date":"2018-01-01"}]}`))
		case "/tv/100/season/1":
			rw.Write([]byte(`{"episodes":[{"episode_number":1,"name":"One"},{"episode_number":2,"name":"Two"},{"episode_number":3,"name":"Three"}]}`))
		default:
			rw.Write([]byte(`{"id":100,"name":"Show"}`))
		}
	}))
	t.Cleanup(tmdb.Close)
	s := New(st, &config.Config{}, metadata.NewTMDBAt("k", tmdb.URL))

	lib := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: "/a"}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	n := func(i int) *int { return &i }
	series := "series"
	up := func(it *model.Item, file bool) {
		t.Helper()
		it.GUID, it.LibraryID = "g-"+it.ID, lib.ID
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		if file {
			if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "src-" + it.ID, ItemID: it.ID, IsPrimary: true}, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	up(&model.Item{ID: series, Type: model.TypeSeries, Title: "Show", ProviderIDs: map[string]string{"tmdb": "100"}}, false)
	s0, s1 := "s0", "s1"
	up(&model.Item{ID: s0, Type: model.TypeSeason, ParentID: &series, Title: "Specials", IndexAired: n(0)}, false)
	up(&model.Item{ID: s1, Type: model.TypeSeason, ParentID: &series, Title: "Season 1", IndexAired: n(1)}, false)
	up(&model.Item{ID: "ova", Type: model.TypeEpisode, ParentID: &s0, Title: "The Wonderful Choker", SeasonAired: n(0), IndexAired: n(3)}, true)
	up(&model.Item{ID: "nofile", Type: model.TypeEpisode, ParentID: &s0, Title: "Works of Art", SeasonAired: n(0), IndexAired: n(4)}, false)
	up(&model.Item{ID: "e3", Type: model.TypeEpisode, ParentID: &s1, Title: "Three", SeasonAired: n(1), IndexAired: n(3)}, true)

	have := func(season int) []int {
		t.Helper()
		eps, err := s.Episodes(ctx, 100, season)
		if err != nil {
			t.Fatal(err)
		}
		var out []int
		for _, e := range eps {
			if e.Have {
				out = append(out, e.Number)
			}
		}
		return out
	}
	if got := have(0); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("specials held = %v, want [1] (the OVA filed as S00E03 is the list's special 1)", got)
	}
	if got := have(1); !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("season 1 held = %v, want [3]", got)
	}
}
