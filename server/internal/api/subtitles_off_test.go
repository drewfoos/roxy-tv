package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// subtitleIndex -1 means "subtitles off": it is stored, replaces a saved track, survives
// a report that omits the field, and comes back as userData.subtitleIndex = -1. Other
// negatives are read as off too.
func TestProgressSubtitlesOffIsSaved(t *testing.T) {
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
	runtime := int64(3_600_000)
	it := &model.Item{ID: "e1", GUID: "g-e1", LibraryID: lib.ID, Type: model.TypeMovie, Title: "E1", RuntimeMs: &runtime}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	post := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/me/progress", strings.NewReader(body))
		req = req.WithContext(auth.WithPrincipal(req.Context(), &model.Principal{UserID: u.ID, User: u, ScopeID: scope}))
		rec := httptest.NewRecorder()
		s.progress(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: status %d (%s)", body, rec.Code, rec.Body.String())
		}
	}
	sub := func() string {
		t.Helper()
		d, err := st.GetUserItemData(ctx, scope, it.GUID)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(d.Native())
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		v, ok := m["subtitleIndex"]
		if !ok {
			return "absent"
		}
		b, _ = json.Marshal(v)
		return string(b)
	}
	post(`{"itemId":"e1","positionMs":60000,"subtitleIndex":3}`)
	if got := sub(); got != "3" {
		t.Fatalf("after choosing track 3: %s", got)
	}
	post(`{"itemId":"e1","positionMs":70000,"subtitleIndex":-1}`)
	if got := sub(); got != "-1" {
		t.Fatalf("after turning subtitles off: subtitleIndex %s, want -1", got)
	}
	post(`{"itemId":"e1","positionMs":80000}`)
	if got := sub(); got != "-1" {
		t.Fatalf("a report without the field changed it: %s, want -1", got)
	}
	post(`{"itemId":"e1","positionMs":90000,"subtitleIndex":2}`)
	post(`{"itemId":"e1","positionMs":95000,"subtitleIndex":-7}`)
	if got := sub(); got != "-1" {
		t.Fatalf("subtitleIndex -7: stored %s, want -1", got)
	}
}
