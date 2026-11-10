package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// A phone downloaded an episode, the file moved on the server (a new item), and the phone
// comes back with progress it saved offline under the old item's id. The report lands on
// the new item, and so does a watched mark; they used to be refused as unknown items (404)
// and dropped by the phone.
func TestReportsForAMovedItemReachItsNewItem(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	u := &model.User{ID: "u1", Name: "drew"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	scope, err := st.DefaultProfileID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	lib := &model.Library{Name: "TV", Kind: model.KindMovies, Root: t.TempDir()}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	runtime := int64(3_600_000)
	for _, id := range []string{"old", "new"} {
		if err := st.UpsertItem(ctx, &model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeMovie, Title: id, RuntimeMs: &runtime}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "src-new", ItemID: "new", IsPrimary: true,
		Parts: []model.MediaPart{{ID: "part-new", Path: filepath.Join(lib.Root, "new.mkv")}}}, "{}"); err != nil {
		t.Fatal(err)
	}
	chain, _ := st.ItemChain(ctx, "old")
	if _, err := st.CarryChain(ctx, chain, "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PruneOrphans(ctx); err != nil { // the old item, with no file, goes
		t.Fatal(err)
	}
	if it, _ := st.GetItem(ctx, "old"); it != nil {
		t.Fatal("old item still there")
	}
	s := &Server{store: st}
	principal := &model.Principal{UserID: u.ID, User: u, ScopeID: scope}

	req := httptest.NewRequest(http.MethodPost, "/v1/me/progress", strings.NewReader(`{"itemId":"old","positionMs":120000}`))
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec := httptest.NewRecorder()
	s.progress(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("progress for the old id: %d %s", rec.Code, rec.Body)
	}
	if d, _ := st.GetUserItemData(ctx, scope, "g-new"); d == nil || d.PositionMs != 120_000 {
		t.Fatalf("progress not on the new item: %+v", d)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/me/played/old", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("itemId", "old")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec = httptest.NewRecorder()
	s.played(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"itemId":"new"`) {
		t.Fatalf("played for the old id: %d %s", rec.Code, rec.Body)
	}
	if d, _ := st.GetUserItemData(ctx, scope, "g-new"); d == nil || !d.Played {
		t.Fatalf("watched mark not on the new item: %+v", d)
	}
}

// The other calls that take an id a client may still hold from before a file moved: a
// list by parent and an edit of the details. Both used the id as given: the list came
// back empty and the edit answered "item not found".
func TestListAndEditFollowAMovedItem(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	lib := &model.Library{Name: "TV", Kind: model.KindSeries, Root: t.TempDir()}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	series, one := "sr-new", 1
	for _, it := range []*model.Item{
		{ID: series, GUID: "g-sr", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"},
		{ID: "se", GUID: "g-se", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series, IndexAired: &one, Title: "Season 1"},
	} {
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO item_redirects (old_id, new_id, created_at) VALUES ('sr-old', 'sr-new', ?)`, store.Now()); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}

	rec := httptest.NewRecorder()
	s.listItems(rec, httptest.NewRequest(http.MethodGet, "/v1/items?parent=sr-old&type=season&sort=index", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"se"`) {
		t.Errorf("seasons listed by the old series id: %d %s", rec.Code, rec.Body)
	}

	admin := &model.Principal{UserID: "a", User: &model.User{ID: "a", Name: "admin", IsAdmin: true}, IsAdmin: true}
	edit := func(id, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/v1/items/"+id+"/metadata", strings.NewReader(body))
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
		req = req.WithContext(auth.WithPrincipal(req.Context(), admin))
		rec := httptest.NewRecorder()
		s.editMetadata(rec, req)
		return rec
	}
	if rec := edit("sr-old", `{"title":"Show, renamed","year":2019,"premiered":"2019-04-06"}`); rec.Code != 200 {
		t.Fatalf("edit by the old id: %d %s", rec.Code, rec.Body)
	}
	if it, _ := st.GetItem(ctx, series); it == nil || it.Title != "Show, renamed" {
		t.Errorf("the edit did not reach the item that replaced the old one: %+v", it)
	}
	// What is not a date or a year is refused, not stored.
	for _, body := range []string{
		`{"title":"Show","premiered":"April 2019"}`,
		`{"title":"Show","premiered":"2019-13-40"}`,
		`{"title":"Show","year":19}`,
	} {
		if rec := edit(series, body); rec.Code != 400 {
			t.Errorf("edit %s: %d, want 400", body, rec.Code)
		}
	}
	if it, _ := st.GetItem(ctx, series); it == nil || it.Title != "Show, renamed" || it.Year == nil || *it.Year != 2019 {
		t.Errorf("a refused edit changed the item: %+v", it)
	}
}
