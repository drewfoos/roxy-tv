package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Found by an audit: a TV left paused at 50% sent its stop report after the episode was
// finished on the phone, and the report (live, so never checked for age) marked it
// unwatched again at 50%. Reports now say how long ago their position was reached
// (ageMs, by the sender's own clock): that one is older than the finish and is dropped.
// A real rewatch (a fresh position) still unmarks, and a phone with a wrong clock is no
// longer believed about order.
func TestProgressAge(t *testing.T) {
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
	runtime := int64(1_400_000)
	it := &model.Item{ID: "e1", GUID: "g-e1", LibraryID: lib.ID, Type: model.TypeMovie, Title: "E1", RuntimeMs: &runtime}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	as := func(r *http.Request) *http.Request {
		return r.WithContext(auth.WithPrincipal(r.Context(), &model.Principal{UserID: u.ID, User: u, ScopeID: scope}))
	}
	post := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.progress(rec, as(httptest.NewRequest(http.MethodPost, "/v1/me/progress", strings.NewReader(body))))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: status %d (%s)", body, rec.Code, rec.Body.String())
		}
	}
	state := func() (bool, int64) {
		t.Helper()
		d, err := st.GetUserItemData(ctx, scope, it.GUID)
		if err != nil || d == nil {
			t.Fatalf("no data: %v", err)
		}
		return d.Played, d.PositionMs
	}

	// The TV pauses at 50% (reported then), the phone finishes the episode a little later.
	post(`{"itemId":"e1","positionMs":700000,"ageMs":0}`)
	time.Sleep(20 * time.Millisecond)
	post(`{"itemId":"e1","positionMs":1380000,"ageMs":0}`)
	if played, _ := state(); !played {
		t.Fatal("the finish should mark it watched")
	}
	// The TV's stop report: still at 50%, reached 5 s ago (before the finish).
	post(`{"itemId":"e1","positionMs":700000,"ageMs":5000}`)
	if played, pos := state(); !played {
		t.Fatalf("a stale report unmarked the finished episode (position %d)", pos)
	}
	// A real rewatch: a fresh position past the minimum resume point (5% = 70 s) unmarks.
	post(`{"itemId":"e1","positionMs":80000,"ageMs":0}`)
	if played, pos := state(); played || pos != 80000 {
		t.Fatalf("a rewatch should unmark: played=%v pos=%d", played, pos)
	}
	// Older clients (no age, no at) are applied as before.
	post(`{"itemId":"e1","positionMs":90000}`)
	if _, pos := state(); pos != 90000 {
		t.Fatalf("a report without age should apply: %d", pos)
	}
	// A phone clock 10 minutes fast cannot make an old offline report win: age is elapsed
	// time by its own clock, not a time of day.
	post(`{"itemId":"e1","positionMs":120000,"ageMs":0}`)
	post(`{"itemId":"e1","positionMs":30000,"ageMs":600000,"at":` + itoa(time.Now().Add(10*time.Minute).UnixMilli()) + `}`)
	if _, pos := state(); pos != 120000 {
		t.Fatalf("an old offline report with a fast clock won: %d", pos)
	}

	// Marks: an offline "watched" made 10 minutes ago, after a rewatch now, is dropped.
	rec := httptest.NewRecorder()
	req := as(httptest.NewRequest(http.MethodPost, "/v1/me/played/e1?age=600000", nil))
	rctx := chiCtx(req, "itemId", "e1")
	s.played(rec, rctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("played: %d %s", rec.Code, rec.Body.String())
	}
	if played, _ := state(); played {
		t.Fatal("an old offline watched mark undid the newer rewatch")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func chiCtx(r *http.Request, key, val string) *http.Request {
	rc := chi.NewRouteContext()
	rc.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
}
