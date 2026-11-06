package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// A listing with include=sources whose source lookup fails is a 500, not a 200 with the
// sources left out (which reads to a client as episodes with nothing to play).
func TestListItemsReportsSourceLookupFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "L", Kind: model.KindMovies, Root: t.TempDir()}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	it := &model.Item{ID: "m1", GUID: "test:m1", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	src := &model.MediaSource{ID: "s1", ItemID: "m1", Container: "mkv", IsPrimary: true, Parts: []model.MediaPart{{ID: "p1", Path: "/m/film.mkv"}}}
	if err := st.ReplaceSource(ctx, src, "{}"); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: &config.Config{}, store: st}
	r := chi.NewRouter()
	r.Get("/v1/items", s.listItems)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/items?include=sources", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"s1"`) {
		t.Fatalf("working lookup: %d %s", rec.Code, rec.Body.String())
	}

	// Break the source lookup only (the listing itself does not read media_streams).
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`DROP TABLE media_streams`); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/items?include=sources", nil))
	if rec.Code != 500 {
		t.Fatalf("failed lookup answered %d: %s", rec.Code, rec.Body.String())
	}
	// Without include=sources the listing is unaffected.
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/items", nil))
	if rec.Code != 200 {
		t.Fatalf("plain listing answered %d", rec.Code)
	}
}
