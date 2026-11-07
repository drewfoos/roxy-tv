package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// An empty library folder whose records the scan kept (it cannot tell the folder from an
// unmounted drive) is listed on the admin screen, and an admin's confirmation removes the
// records. The confirmation is refused for a library not in that state, so it cannot be
// given ahead of time, and to anyone but an admin.
func TestAcceptEmptyLibrary(t *testing.T) {
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
	// A record of a file the folder no longer has (and no scan has recorded the folder).
	if err := st.UpsertItem(ctx, &model.Item{ID: "m", GUID: "g-m", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Gone"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "src", ItemID: "m", IsPrimary: true,
		Parts: []model.MediaPart{{ID: "part", Path: filepath.Join(lib.Root, "Gone (2000).mkv")}}}, "{}"); err != nil {
		t.Fatal(err)
	}
	l := library.New(st, nil, nil)
	s := &Server{store: st, lib: l}
	admin := &model.Principal{UserID: "a", User: &model.User{ID: "a", Name: "admin", IsAdmin: true}, IsAdmin: true}
	viewer := &model.Principal{UserID: "v", User: &model.User{ID: "v", Name: "kid"}}
	accept := func(p *model.Principal) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/library/folders/x/accept-empty", nil)
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", strconv.FormatInt(lib.ID, 10))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
		req = req.WithContext(auth.WithPrincipal(req.Context(), p))
		rec := httptest.NewRecorder()
		s.acceptEmptyFolder(rec, req)
		return rec.Code
	}
	manage := func() []map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/v1/library/manage", nil)
		req = req.WithContext(auth.WithPrincipal(req.Context(), admin))
		rec := httptest.NewRecorder()
		s.libraryManage(rec, req)
		var out struct {
			EmptyLibraries []map[string]any `json:"emptyLibraries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("manage: %v %s", err, rec.Body)
		}
		return out.EmptyLibraries
	}
	recorded := func() int {
		paths, err := st.ListPartPaths(ctx, lib.ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(paths)
	}

	if code := accept(admin); code != http.StatusConflict {
		t.Fatalf("confirming before any scan found it empty: %d, want 409", code)
	}
	if err := l.Scan(ctx, *lib); err == nil {
		t.Fatal("scan of the empty folder should report it")
	}
	if e := manage(); len(e) != 1 || e[0]["name"] != "Movies" || e[0]["files"] != float64(1) {
		t.Fatalf("emptyLibraries = %v", e)
	}
	if code := accept(viewer); code != http.StatusForbidden {
		t.Fatalf("a non-admin confirmed: %d", code)
	}
	if code := accept(admin); code != http.StatusAccepted {
		t.Fatalf("admin confirmation: %d", code)
	}
	for deadline := time.Now().Add(10 * time.Second); recorded() != 0; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("confirmed, but %d records kept", recorded())
		}
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if running, _, _ := l.ScanStatus(); !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scan never finished")
		}
	}
	if e := manage(); len(e) != 0 {
		t.Fatalf("still listed after the confirmation: %v", e)
	}
}

// Removing a library notes its files as departed: added again at another path (the drive
// mounted elsewhere, the folder moved and added as a new library), their watch history
// can follow them. It used to vanish with the library.
func TestRemovedLibraryFilesAreDepartures(t *testing.T) {
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
	s := &Server{store: st}
	admin := &model.Principal{UserID: "a", User: &model.User{ID: "a", Name: "admin", IsAdmin: true}, IsAdmin: true}
	req := httptest.NewRequest(http.MethodDelete, "/v1/library/folders/x", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", strconv.FormatInt(lib.ID, 10))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	req = req.WithContext(auth.WithPrincipal(req.Context(), admin))
	rec := httptest.NewRecorder()
	s.removeFolder(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	var n int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM departures WHERE path = ?`, filepath.Join(lib.Root, "Film (2000).mkv")).Scan(&n); err != nil || n != 1 {
		t.Fatalf("departures for the removed library's file: %d %v", n, err)
	}
}
