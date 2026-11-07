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
