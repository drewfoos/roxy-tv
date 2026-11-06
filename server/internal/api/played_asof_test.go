package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// A phone finished an episode offline an hour ago; it has been rewatched part way on the
// TV since. When the phone comes back and replays its watched mark with at, the TV's
// newer position stays. Without at (a live mark) it applies as always.
func TestReplayedWatchedMarkDoesNotOverrideNewerActivity(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	u := &model.User{ID: "u1", Name: "drew"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	scope, err := st.DefaultProfileID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	lib := &model.Library{Name: "TV", Kind: model.KindSeries, Root: t.TempDir()}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	runtime := int64(1_440_000)
	it := &model.Item{ID: "e1", GUID: "g-e1", LibraryID: lib.ID, Type: model.TypeEpisode, Title: "E1", RuntimeMs: &runtime}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	mark := func(query string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/me/played/e1"+query, strings.NewReader(""))
		rc := chi.NewRouteContext()
		rc.URLParams.Add("itemId", "e1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
		req = req.WithContext(auth.WithPrincipal(req.Context(), &model.Principal{UserID: u.ID, User: u, ScopeID: scope}))
		rec := httptest.NewRecorder()
		s.played(rec, req)
		return rec.Code
	}
	state := func() (int64, bool) {
		t.Helper()
		d, err := st.GetUserItemData(ctx, scope, it.GUID)
		if err != nil || d == nil {
			t.Fatalf("user data: %v %v", d, err)
		}
		return d.PositionMs, d.Played
	}

	if err := st.SetProgress(ctx, scope, it.GUID, 600_000, nil, nil); err != nil { // the TV, just now
		t.Fatal(err)
	}
	hourAgo := time.Now().Add(-time.Hour).UnixMilli()
	if code := mark(fmt.Sprintf("?at=%d", hourAgo)); code != 200 {
		t.Fatalf("replayed mark: status %d", code)
	}
	if pos, played := state(); played || pos != 600_000 {
		t.Fatalf("old offline mark overrode the rewatch: position %d played %v", pos, played)
	}
	if code := mark("?at=nope"); code != 400 {
		t.Fatalf("bad at: status %d, want 400", code)
	}
	if code := mark(""); code != 200 {
		t.Fatalf("live mark: status %d", code)
	}
	if pos, played := state(); !played || pos != 0 {
		t.Fatalf("live mark not applied: position %d played %v", pos, played)
	}
}
