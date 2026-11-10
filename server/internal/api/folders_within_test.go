package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// A folder whose name starts with two dots is inside its parent like any other; only
// ".." itself and "../…" lead out. "..extras" under a library used to be judged outside
// it, so it could be added as a second library reading the same files.
func TestWithin(t *testing.T) {
	sep := string(filepath.Separator)
	for _, c := range []struct {
		child, parent string
		want          bool
	}{
		{sep + "media" + sep + "tv" + sep + "show", sep + "media" + sep + "tv", true},
		{sep + "media" + sep + "tv" + sep + "..extras", sep + "media" + sep + "tv", true},
		{sep + "media" + sep + "tv" + sep + "...", sep + "media" + sep + "tv", true},
		{sep + "media" + sep + "tv", sep + "media" + sep + "tv", false},
		{sep + "media", sep + "media" + sep + "tv", false},
		{sep + "media" + sep + "films", sep + "media" + sep + "tv", false},
		{sep + "media" + sep + "tv2", sep + "media" + sep + "tv", false},
	} {
		if got := within(c.child, c.parent); got != c.want {
			t.Errorf("within(%q, %q) = %v, want %v", c.child, c.parent, got, c.want)
		}
	}
}

// When a library's files cannot be noted as departed, the library is not removed: going
// on used to drop it anyway, leaving its watch history unable to follow the files to
// wherever they are added again.
func TestRemoveLibraryStopsWhenDeparturesFail(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: t.TempDir()}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertItem(ctx, &model.Item{ID: "m", GUID: "g-m", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "src", ItemID: "m", IsPrimary: true,
		Parts: []model.MediaPart{{ID: "part", Path: filepath.Join(lib.Root, "Film (2000).mkv"), Size: 4242}}}, "{}"); err != nil {
		t.Fatal(err)
	}
	// The write of the departure fails (stands in for a full disk or a locked database).
	if _, err := st.DB().ExecContext(ctx, `ALTER TABLE departures RENAME TO departures_gone`); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	admin := &model.Principal{UserID: "a", User: &model.User{ID: "a", Name: "admin", IsAdmin: true}, IsAdmin: true}
	req := httptest.NewRequest(http.MethodDelete, "/v1/library/folders/x", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", strconv.FormatInt(lib.ID, 10))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	req = req.WithContext(auth.WithPrincipal(req.Context(), admin))
	rec := httptest.NewRecorder()
	s.removeFolder(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("remove with departures failing: %d %s, want 500", rec.Code, rec.Body)
	}
	if it, _ := st.GetItem(ctx, "m"); it == nil {
		t.Error("the library's item was removed although its departure could not be noted")
	}
	libs, _ := st.ListLibraries(ctx)
	if len(libs) != 1 {
		t.Errorf("libraries after the refused removal: %d, want 1", len(libs))
	}
}
