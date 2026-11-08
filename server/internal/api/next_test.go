package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// The rule, with the cases of the web's nextToPlay tests (items.test.ts) and the apps'
// upNextOf: the three must agree.
func TestNextToPlayRule(t *testing.T) {
	ep := func(season, n int, pos int64, played bool) model.Item {
		s, i := season, n
		return model.Item{ID: fmt.Sprintf("s%de%d", season, n), Type: model.TypeEpisode, SeasonAired: &s, IndexAired: &i, HasMedia: true,
			UserData: &model.UserData{PositionMs: pos, Played: played}}
	}
	missing := ep(1, 1, 0, false)
	missing.Availability = "missing"
	cases := []struct {
		name string
		eps  []model.Item
		want string
	}{
		{"resumes the episode in progress", []model.Item{ep(1, 1, 0, true), ep(1, 2, 5000, false), ep(1, 3, 0, false)}, "s1e2"},
		{"two in progress: the later one", []model.Item{ep(2, 1, 9000, false), ep(1, 4, 5000, false), ep(1, 5, 0, false)}, "s2e1"},
		{"goes on after the last watched", []model.Item{ep(1, 1, 0, true), ep(1, 2, 0, false), ep(1, 3, 0, true), ep(1, 4, 0, false)}, "s1e4"},
		{"starts over when all are watched", []model.Item{ep(1, 1, 0, true), ep(1, 2, 0, true)}, "s1e1"},
		{"specials only when nothing else", []model.Item{ep(0, 1, 0, false), ep(1, 1, 0, false)}, "s1e1"},
		{"only specials", []model.Item{ep(0, 1, 0, false)}, "s0e1"},
		{"skips what cannot play", []model.Item{missing, ep(1, 2, 0, false)}, "s1e2"},
		{"nothing", nil, ""},
	}
	for _, c := range cases {
		got := ""
		if n := nextToPlay(c.eps); n != nil {
			got = n.ID
		}
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The endpoint on a long show (two seasons of 300): the first 450 watched, so Play means
// S2 E151, which the clients' single default request (the first 100) never saw. A film
// answers itself; a season is refused.
func TestNextEndpointLongShow(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	lib := &model.Library{Name: "TV", Kind: model.KindSeries, Root: "/tv"}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	u := &model.User{ID: "u1", Name: "drew"}
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
	up(&model.Item{ID: series, GUID: "g-show", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Long Show"})
	up(&model.Item{ID: "film", GUID: "g-film", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"})
	n := 0
	for season := 1; season <= 2; season++ {
		sid := fmt.Sprintf("season%d", season)
		sn := season
		up(&model.Item{ID: sid, GUID: "g-" + sid, LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series, IndexAired: &sn})
		for e := 1; e <= 300; e++ {
			id := fmt.Sprintf("s%de%d", season, e)
			en, ss := e, season
			up(&model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &sid, SeasonAired: &ss, IndexAired: &en, Title: id})
			if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "src-" + id, ItemID: id, Parts: []model.MediaPart{{ID: "p-" + id, Path: "/tv/" + id + ".mkv"}}}, "{}"); err != nil {
				t.Fatal(err)
			}
			n++
			if n <= 450 {
				if err := st.MarkPlayed(ctx, scope, "g-"+id, true); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	s := &Server{store: st}
	call := func(id string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, "/v1/items/"+id+"/next", nil)
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
		req = req.WithContext(auth.WithPrincipal(req.Context(), &model.Principal{UserID: u.ID, User: u, ScopeID: scope}))
		rec := httptest.NewRecorder()
		s.itemNext(rec, req)
		var out struct{ Item *model.Item }
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out.Item == nil {
			return rec.Code, ""
		}
		return rec.Code, out.Item.ID
	}
	if code, id := call(series); code != 200 || id != "s2e151" {
		t.Fatalf("next of the show: %d %q, want s2e151", code, id)
	}
	if code, id := call("film"); code != 200 || id != "film" {
		t.Fatalf("next of a film: %d %q", code, id)
	}
	if code, _ := call("season1"); code != 400 {
		t.Fatalf("next of a season: %d, want 400", code)
	}
}
