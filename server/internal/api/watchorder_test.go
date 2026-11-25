package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/seasonlist"
	"github.com/roxy-media/roxy/server/internal/store"
)

// A show with OVAs between its seasons, as the season list shows them: two that came out
// between seasons 1 and 2, one before season 3, a special of an episode's length after
// the last, a special as long as a film (an entry of its own in the list, whatever it is
// called); and the extras at the list's foot (a short, a recap, an undated one, one the
// library has no file of). What plays next went by the seasons' numbers alone, specials
// last: after season 1 came season 2, past the two OVAs.
type ovaShow struct {
	st    *store.Store
	s     *Server
	user  *model.User
	scope string
}

// The order the season list reads in, extras last.
const ovaShowOrder = "s1e1 s1e2 s1e3 ova1 ova2 s2e1 s2e2 film newova s3e1 hero0 | short recap undated nofile"

func newOVAShow(t *testing.T) *ovaShow {
	t.Helper()
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
	u := &model.User{ID: "u1", Name: "somebody"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	scope, err := st.DefaultProfileID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	up := func(it *model.Item) {
		t.Helper()
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	series := "show"
	up(&model.Item{ID: series, GUID: "g-show", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"})
	for n, title := range []string{"Specials", "Season 1", "Season 2", "Season 3"} {
		n := n
		up(&model.Item{ID: "sea" + string(rune('0'+n)), GUID: "g-sea" + string(rune('0'+n)), LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series, IndexAired: &n, Title: title})
	}
	ep := func(id string, season, n int, title, date string, minutes float64, file bool) {
		t.Helper()
		sid, sn, en := "sea"+string(rune('0'+season)), season, n
		it := &model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &sid, SeasonAired: &sn, IndexAired: &en, Title: title, IsVirtual: !file}
		if minutes > 0 {
			ms := int64(minutes * 60 * 1000)
			it.RuntimeMs = &ms
		}
		up(it)
		if file {
			if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "src-" + id, ItemID: id, IsPrimary: true, Parts: []model.MediaPart{{ID: "p-" + id, Path: "/a/" + id + ".mkv"}}}, "{}"); err != nil {
				t.Fatal(err)
			}
		}
		if date != "" {
			if err := st.SetAirDateIfUnset(ctx, id, date); err != nil {
				t.Fatal(err)
			}
		}
	}
	ep("s1e1", 1, 1, "One", "2012-01-06", 24, true)
	ep("s1e2", 1, 2, "Two", "2012-01-13", 24, true)
	ep("s1e3", 1, 3, "Three", "2012-01-20", 24, true)
	ep("s2e1", 2, 1, "Four", "2013-07-07", 24, true)
	ep("s2e2", 2, 2, "Five", "2013-07-14", 24, true)
	ep("s3e1", 3, 1, "Six", "2015-04-04", 24, true)
	ep("nofile", 0, 1, "Before the Show", "2011-12-19", 0, false)
	ep("short", 0, 2, "Going to the Beach", "2012-03-21", 3.5, true)
	ep("ova1", 0, 8, "Show OVA 1", "2012-09-20", 20.4, true)
	ep("ova2", 0, 9, "Show OVA 2", "2013-05-21", 20.4, true)
	ep("newova", 0, 10, "New OVA", "2015-03-10", 20.7, true)
	ep("hero0", 0, 18, "Hero Episode 0", "2018-04-10", 23.7, true)
	ep("recap", 0, 20, "Recap of It All", "2013-06-01", 24, true)
	ep("undated", 0, 21, "Somewhere in Between", "", 22, true)
	ep("film", 0, 22, "The Compilation Film", "2014-01-01", 95, true)
	return &ovaShow{st: st, s: &Server{store: st}, user: u, scope: scope}
}

func (o *ovaShow) get(t *testing.T, handler http.HandlerFunc, url, id string, into any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	req = req.WithContext(auth.WithPrincipal(req.Context(), &model.Principal{UserID: o.user.ID, User: o.user, ScopeID: o.scope}))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%s: %d %s", url, rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatal(err)
	}
}

func (o *ovaShow) watched(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := o.st.MarkPlayed(context.Background(), o.scope, "g-"+id, true); err != nil {
			t.Fatal(err)
		}
	}
}

// byWatchOrder is what a player does with a show's episodes: sorts them by the place the
// server gives each.
func byWatchOrder(t *testing.T, items []model.Item) []string {
	t.Helper()
	for _, it := range items {
		if it.WatchOrder == nil {
			t.Fatalf("%s has no place in the order", it.ID)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return *items[i].WatchOrder < *items[j].WatchOrder })
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return ids
}

// Every episode of a show comes with its place in the order of watching, and sorted by
// it they read as the season list does from the top: whatever the list's own order was.
func TestEpisodesComeWithTheirPlaceInTheWatchOrder(t *testing.T) {
	o := newOVAShow(t)
	want := strings.Fields(strings.ReplaceAll(ovaShowOrder, "|", ""))

	var all struct{ Items []model.Item }
	o.get(t, o.s.listItems, "/v1/items?type=episode&parent=show&recursive=true&sort=index", "", &all)
	if got := byWatchOrder(t, all.Items); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("a show's episodes by their place:\n got %v\nwant %v", got, want)
	}

	// The season list itself, read from the top, is that order: one rule for both.
	var list struct{ Sections []seasonlist.Section }
	o.get(t, o.s.seriesSections, "/v1/items/show/sections", "show", &list)
	var read []string
	for _, sec := range list.Sections {
		switch sec.Kind {
		case "season":
			var season struct{ Items []model.Item }
			o.get(t, o.s.listItems, "/v1/items?type=episode&parent="+sec.SeasonID+"&sort=index", "", &season)
			for _, it := range season.Items {
				read = append(read, it.ID)
			}
		case "specials":
			read = append(read, sec.EpisodeIDs...)
		case "movie": // a special as long as a film is one of the show's own episodes
			if sec.Item != nil && sec.Item.Type == model.TypeEpisode {
				read = append(read, sec.Item.ID)
			}
		}
	}
	if strings.Join(read, " ") != strings.Join(want, " ") {
		t.Fatalf("the season list read from the top:\n got %v\nwant %v", read, want)
	}

	// One season's list, and one episode asked for by itself, carry the same places as
	// the whole show's: a player that holds an episode from one answer and its
	// neighbours from another sorts them rightly.
	place := map[string]int64{}
	for _, it := range all.Items {
		place[it.ID] = *it.WatchOrder
	}
	var specials struct{ Items []model.Item }
	o.get(t, o.s.listItems, "/v1/items?type=episode&parent=sea0&sort=index", "", &specials)
	if len(specials.Items) != 9 {
		t.Fatalf("the specials season lists %d", len(specials.Items))
	}
	for _, it := range specials.Items {
		if it.WatchOrder == nil || *it.WatchOrder != place[it.ID] {
			t.Errorf("%s in its season's list: %v, in the show's %d", it.ID, it.WatchOrder, place[it.ID])
		}
	}
	var one model.Item
	o.get(t, o.s.getItem, "/v1/items/ova2", "ova2", &one)
	if one.WatchOrder == nil || *one.WatchOrder != place["ova2"] {
		t.Errorf("an episode by itself: %v, in the show's list %d", one.WatchOrder, place["ova2"])
	}
	// A series, a season, a film: no place (it is an episode's).
	var show model.Item
	o.get(t, o.s.getItem, "/v1/items/show", "show", &show)
	if show.WatchOrder != nil {
		t.Errorf("a series was given a place: %d", *show.WatchOrder)
	}
}

// An episode added to the library does not move the places of the others: a place kept
// from an earlier look (with a download) still sorts rightly among newer ones.
func TestAPlaceInTheWatchOrderStaysWhenEpisodesAreAdded(t *testing.T) {
	o := newOVAShow(t)
	var before struct{ Items []model.Item }
	o.get(t, o.s.listItems, "/v1/items?type=episode&parent=show&recursive=true&sort=index", "", &before)
	ctx := context.Background()
	add := func(id, season string, n int, title, date string, minutes float64) {
		sid, en := season, n
		ms := int64(minutes * 60 * 1000)
		if err := o.st.UpsertItem(ctx, &model.Item{ID: id, GUID: "g-" + id, LibraryID: 1, Type: model.TypeEpisode, ParentID: &sid, IndexAired: &en, Title: title, RuntimeMs: &ms}); err != nil {
			t.Fatal(err)
		}
		if err := o.st.ReplaceSource(ctx, &model.MediaSource{ID: "src-" + id, ItemID: id, IsPrimary: true, Parts: []model.MediaPart{{ID: "p-" + id, Path: "/a/" + id + ".mkv"}}}, "{}"); err != nil {
			t.Fatal(err)
		}
		if err := o.st.SetAirDateIfUnset(ctx, id, date); err != nil {
			t.Fatal(err)
		}
	}
	add("s1e4", "sea1", 4, "Three and a Half", "2012-01-27", 24)
	add("ova0", "sea0", 30, "Show OVA 0", "2012-08-01", 20) // an OVA earlier in the same gap (by its date, not its number)
	add("s2e3", "sea2", 3, "Five and a Half", "2013-07-21", 24)
	var after struct{ Items []model.Item }
	o.get(t, o.s.listItems, "/v1/items?type=episode&parent=show&recursive=true&sort=index", "", &after)
	now := map[string]int64{}
	for _, it := range after.Items {
		now[it.ID] = *it.WatchOrder
	}
	for _, it := range before.Items {
		if *it.WatchOrder != now[it.ID] {
			t.Errorf("%s moved from %d to %d", it.ID, *it.WatchOrder, now[it.ID])
		}
	}
	want := "s1e1 s1e2 s1e3 s1e4 ova0 ova1 ova2 s2e1 s2e2 s2e3 film newova s3e1 hero0 short recap undated nofile"
	if got := strings.Join(byWatchOrder(t, after.Items), " "); got != want {
		t.Fatalf("with the new episodes:\n got %s\nwant %s", got, want)
	}
}

// A show's Play means the next thing in the season list: the OVA after a season's last
// episode, not the next season's first. The extras at the list's foot never, while
// there is anything else.
func TestPlayMeansTheNextInTheSeasonList(t *testing.T) {
	o := newOVAShow(t)
	next := func() string {
		t.Helper()
		var out struct{ Item *model.Item }
		o.get(t, o.s.itemNext, "/v1/items/show/next", "show", &out)
		if out.Item == nil {
			return "<none>"
		}
		return out.Item.ID
	}
	if got := next(); got != "s1e1" {
		t.Fatalf("nothing watched: %s", got)
	}
	o.watched(t, "s1e1", "s1e2", "s1e3")
	if got := next(); got != "ova1" {
		t.Fatalf("season 1 watched: Play means %s, want the first OVA", got)
	}
	o.watched(t, "ova1")
	if got := next(); got != "ova2" {
		t.Fatalf("after the first OVA: %s", got)
	}
	o.watched(t, "ova2")
	if got := next(); got != "s2e1" {
		t.Fatalf("after the OVAs: %s", got)
	}
	// Season 2 begun with the OVAs passed over: Play goes on from there, not back.
	o2 := newOVAShow(t)
	o2.watched(t, "s1e1", "s1e2", "s1e3", "s2e1")
	var out struct{ Item *model.Item }
	o2.get(t, o2.s.itemNext, "/v1/items/show/next", "show", &out)
	if out.Item == nil || out.Item.ID != "s2e2" {
		t.Fatalf("season 2 begun: %+v, want s2e2", out.Item)
	}
	// The last of everything watched: an OVA in progress is gone back to; a short at the
	// foot in progress is not what Play means.
	o.watched(t, "s2e1", "s2e2", "film", "newova", "s3e1")
	if got := next(); got != "hero0" {
		t.Fatalf("before the last special of an episode's length: %s", got)
	}
	if err := o.st.SetProgress(context.Background(), o.scope, "g-short", 60000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := next(); got != "hero0" {
		t.Fatalf("with a short in progress: %s", got)
	}
	if err := o.st.SetProgress(context.Background(), o.scope, "g-newova", 60000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := next(); got != "newova" {
		t.Fatalf("with an OVA in progress: %s", got)
	}
}
