package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Jellyfin's play-state rules, adopted: a minimum resume point (5%, at most 2 min, at
// least 30 s), short items (under 5 min) keep no resume point, the last second counts as
// the end. Unknown length is never "watched" by position (Jellyfin's rule would mark
// everything at the first report).
func TestPlayState(t *testing.T) {
	for _, c := range []struct {
		pos, runtime int64
		played       bool
		min          int64
	}{
		{60_000, 1_440_000, false, 72_000},        // 24-min episode: 5% = 72 s
		{1_300_000, 1_440_000, true, 72_000},      // 90%
		{1_439_500, 1_440_000, true, 72_000},      // the last second
		{300_000, 9_000_000, false, 120_000},      // a 2.5 h film: capped at 2 min
		{200_000, 240_000, true, 30_000},          // a 4-min short past its minimum: watched
		{20_000, 240_000, false, 30_000},          // ... before it: a peek
		{5_000_000, 0, false, store.RewatchMinMs}, // unknown length: never watched by position
	} {
		played, min := playState(c.pos, c.runtime)
		if played != c.played || min != c.min {
			t.Errorf("playState(%d, %d) = %v, %d; want %v, %d", c.pos, c.runtime, played, min, c.played, c.min)
		}
	}
}

// End to end through POST /v1/me/progress: a peek leaves no resume point and does not
// move the item in history; the playing version's own length decides "watched".
func TestProgressPeekAndVersion(t *testing.T) {
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
	lib := &model.Library{Name: "M", Kind: model.KindMovies, Root: t.TempDir()}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	// Two versions: the item's runtime is the extended cut's (scanned last).
	runtime := int64(10_800_000) // 3 h extended
	it := &model.Item{ID: "f", GUID: "g-f", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film", RuntimeMs: &runtime}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "theatrical", ItemID: "f", DurationMs: 9_000_000, Parts: []model.MediaPart{{ID: "p1", Path: "/m/f/t.mkv", Size: 1}}}, "{}"); err != nil {
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
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	get := func() *store.UserItemData {
		d, _ := st.GetUserItemData(ctx, scope, "g-f")
		return d
	}

	// A peek (40 s of a 2.5 h film): no resume point, not in history.
	post(`{"itemId":"f","positionMs":40000,"sourceId":"theatrical"}`)
	if d := get(); d != nil && (d.PositionMs != 0 || !d.LastPlayedAt.IsZero()) {
		t.Fatalf("a peek left a resume point or a history entry: %+v", d)
	}
	// Past the minimum: a resume point.
	post(`{"itemId":"f","positionMs":600000,"sourceId":"theatrical"}`)
	if d := get(); d == nil || d.PositionMs != 600000 {
		t.Fatalf("no resume point at 10 min: %+v", d)
	}
	// 2 h 20 m of the theatrical cut (93%): watched, though only 78% of the item's runtime.
	post(`{"itemId":"f","positionMs":8400000,"sourceId":"theatrical"}`)
	if d := get(); d == nil || !d.Played {
		t.Fatalf("the theatrical cut watched to 93%% is not watched: %+v", d)
	}
}
