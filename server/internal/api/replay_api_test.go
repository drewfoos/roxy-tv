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

// Replays through the HTTP API, as the phone sends them: a report it had sent but whose
// removal from its queue never reached disk goes again after a restart, with its age
// measured again (larger by the time it waited: the same moment). Request parsing and the
// age-to-time reconstruction must keep the store's guarantees: a watched mark or a finish
// sent twice is one play, a position twice is the same position, an old mark replayed after
// a later rewatch is dropped.
func TestReplayedRequestsAreHarmless(t *testing.T) {
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
	scope, _ := st.DefaultProfileID(ctx, u.ID)
	lib := &model.Library{Name: "TV", Kind: model.KindSeries, Root: t.TempDir()}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	runtime := int64(1_440_000)
	for _, id := range []string{"mark", "finish", "pos", "re"} {
		if err := st.UpsertItem(ctx, &model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeMovie, Title: id, RuntimeMs: &runtime}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{store: st}
	as := func(r *http.Request) *http.Request {
		return r.WithContext(auth.WithPrincipal(r.Context(), &model.Principal{UserID: u.ID, User: u, ScopeID: scope}))
	}
	progress := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.progress(rec, as(httptest.NewRequest(http.MethodPost, "/v1/me/progress", strings.NewReader(body))))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	mark := func(id string, ageMs int64) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/me/played/"+id+"?age="+strconv.FormatInt(ageMs, 10), nil)
		rc := chi.NewRouteContext()
		rc.URLParams.Add("itemId", id)
		req = as(req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc)))
		rec := httptest.NewRecorder()
		s.played(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("played %s: %d %s", id, rec.Code, rec.Body.String())
		}
	}
	state := func(id string) *store.UserItemData {
		t.Helper()
		d, err := st.GetUserItemData(ctx, scope, "g-"+id)
		if err != nil || d == nil {
			t.Fatalf("no data for %s: %v", id, err)
		}
		return d
	}
	// The event happened a minute ago; the replay goes 3 s later, its age 3 s larger.
	const age = int64(60_000)
	wait := func() { time.Sleep(30 * time.Millisecond) }

	mark("mark", age)
	wait()
	mark("mark", age+30)
	if d := state("mark"); !d.Played || d.PlayCount != 1 {
		t.Fatalf("a replayed watched mark counted twice: %+v", d)
	}

	progress(`{"itemId":"finish","positionMs":1400000,"ageMs":60000}`)
	wait()
	progress(`{"itemId":"finish","positionMs":1400000,"ageMs":60030}`)
	if d := state("finish"); !d.Played || d.PlayCount != 1 {
		t.Fatalf("a replayed finish counted twice: %+v", d)
	}

	progress(`{"itemId":"pos","positionMs":600000,"ageMs":60000}`)
	wait()
	progress(`{"itemId":"pos","positionMs":600000,"ageMs":60030}`)
	if d := state("pos"); d.Played || d.PositionMs != 600000 {
		t.Fatalf("a replayed position changed it: %+v", d)
	}

	// Watched a minute ago, rewatched part-way 30 s ago; the old mark replayed now is dropped.
	mark("re", age)
	progress(`{"itemId":"re","positionMs":600000,"ageMs":30000}`)
	wait()
	mark("re", age+30)
	if d := state("re"); d.Played || d.PositionMs != 600000 || d.PlayCount != 1 {
		t.Fatalf("an old mark replayed after a rewatch undid it: %+v", d)
	}
}
