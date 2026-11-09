package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// An offline report from the shorter of two versions, sent after the item was removed and
// recreated (a new item id, redirected from the old; new source ids): the source it names
// is gone. The version is then found by the length the player reported, so the 90% rule
// still uses the cut that was watched. Without a matching length the item's runtime
// decides, as before.
func TestOfflineReportAfterRecreateKeepsTheVersion(t *testing.T) {
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
	lib := &model.Library{Name: "M", Kind: model.KindMovies, Root: "/m"}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	const theatrical, extended = int64(5_400_000), int64(7_200_000) // 90 and 120 min
	versions := func(itemID, prefix string) {
		t.Helper()
		runtime := extended // the item's runtime: the version scanned last
		if err := st.UpsertItem(ctx, &model.Item{ID: itemID, GUID: "g-film", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film", RuntimeMs: &runtime}); err != nil {
			t.Fatal(err)
		}
		for _, v := range []struct {
			id   string
			dur  int64
			path string
		}{{prefix + "-theatrical", theatrical, "/m/Film/Film - Theatrical.mkv"}, {prefix + "-extended", extended, "/m/Film/Film - Extended.mkv"}} {
			if err := st.ReplaceSource(ctx, &model.MediaSource{ID: v.id, ItemID: itemID, DurationMs: v.dur, Parts: []model.MediaPart{{ID: v.id + "-p", Path: v.path, Size: v.dur / 1000, DurationMs: v.dur}}}, "{}"); err != nil {
				t.Fatal(err)
			}
		}
	}
	versions("old", "s")

	// Removed (files gone, item pruned) and back: the same identity, new ids everywhere.
	for _, p := range []string{"/m/Film/Film - Theatrical.mkv", "/m/Film/Film - Extended.mkv"} {
		if err := st.RecordDeparture(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"s-theatrical", "s-extended"} {
		if err := st.DeleteSource(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.DeleteItem(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond) // back after it left
	versions("new", "n")
	if _, err := st.SettleDepartures(ctx); err != nil {
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
	played := func() bool {
		d, _ := st.GetUserItemData(ctx, scope, "g-film")
		return d != nil && d.Played
	}

	// Without the played length (an older app): the item's runtime decides, 85 of 120 min
	// is 71%, not watched. This is the case the length is for.
	post(`{"itemId":"old","positionMs":5100000,"sourceId":"s-theatrical","ageMs":1000}`)
	if played() {
		t.Fatal("without a length the item's runtime should decide (71%: not watched)")
	}
	// The queued report with its length: the theatrical cut is found, 85 of 90 min (94%).
	post(`{"itemId":"old","positionMs":5100000,"sourceId":"s-theatrical","durationMs":5400000,"ageMs":500}`)
	if !played() {
		t.Fatal("an offline report from the theatrical cut, sent after the item was recreated, should mark it watched")
	}
}

// A reported length that matches no version is not believed: the item's runtime decides.
func TestReportedLengthMustMatchAVersion(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "M", Kind: model.KindMovies, Root: "/m"}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	runtime := int64(7_200_000)
	st.UpsertItem(ctx, &model.Item{ID: "f", GUID: "g", LibraryID: lib.ID, Type: model.TypeMovie, Title: "F", RuntimeMs: &runtime})
	st.ReplaceSource(ctx, &model.MediaSource{ID: "a", ItemID: "f", DurationMs: 7_200_000, Parts: []model.MediaPart{{ID: "ap", Path: "/m/F/F.mkv", Size: 1}}}, "{}")
	s := &Server{store: st}
	if _, ok := s.playedLength(ctx, "f", "gone", 1_000_000); ok {
		t.Fatal("a length matching no version was used")
	}
	if d, ok := s.playedLength(ctx, "f", "gone", 7_100_000); !ok || d != 7_200_000 {
		t.Fatalf("a length within 5%% of a version should pick it: %d %v", d, ok)
	}
	if d, ok := s.playedLength(ctx, "f", "a", 0); !ok || d != 7_200_000 {
		t.Fatalf("a valid source decides: %d %v", d, ok)
	}
}
