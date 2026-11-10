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

// Alya S1E9 is 25:11 (1510968 ms) with its ED (outro chapter) at 23:40. 90% is 22:40:
// stopped at 22:43, in the last scene, it counted as watched and reopened from the start.
// With the ending known, it is watched when the ending starts.
const alyaMs, alyaEDMs = int64(1_510_968), int64(1_420_000)

func TestEndingStart(t *testing.T) {
	outro := func(start int64) model.MediaSegment {
		return model.MediaSegment{Type: "outro", StartMs: start, EndMs: start + 90_000}
	}
	cases := []struct {
		name string
		segs []model.MediaSegment
		want int64
	}{
		{"none", nil, 0},
		{"the ED", []model.MediaSegment{{Type: "intro", StartMs: 135_000, EndMs: 225_000}, outro(alyaEDMs)}, alyaEDMs},
		{"two sources agree roughly: the earlier", []model.MediaSegment{outro(alyaEDMs + 4_000), outro(alyaEDMs)}, alyaEDMs},
		{"an insert song early on is not the ending", []model.MediaSegment{outro(600_000)}, 0},
		{"past the end is not the ending", []model.MediaSegment{outro(alyaMs + 1)}, 0},
		{"only outros count", []model.MediaSegment{{Type: "preview", StartMs: 1_480_000, EndMs: alyaMs}}, 0},
	}
	for _, c := range cases {
		if got := endingStart(alyaMs, c.segs); got != c.want {
			t.Errorf("%s: endingStart = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestPlayStateEnding(t *testing.T) {
	cases := []struct {
		name         string
		pos, rt, end int64
		played       bool
	}{
		{"in the last scene, past 90%: not yet", 1_363_703, alyaMs, alyaEDMs, false},
		{"the ending starts: watched", alyaEDMs, alyaMs, alyaEDMs, true},
		{"in the ending", alyaEDMs + 30_000, alyaMs, alyaEDMs, true},
		{"no ending known: 90% as before", 1_363_703, alyaMs, 0, true},
		{"the last second always counts", alyaMs - 500, alyaMs, alyaMs - 100, true},
		{"credits early in a film: watched at the credits, before 90%", 6_200_000, 7_200_000, 6_100_000, true},
		{"a short item keeps its own rule", 60_000, 240_000, 230_000, true},
	}
	for _, c := range cases {
		if got, _ := playStateEnding(c.pos, c.rt, c.end); got != c.played {
			t.Errorf("%s: played = %v, want %v", c.name, got, c.played)
		}
	}
}

// Through the progress endpoint, with the ED stored as an outro segment of the version.
func TestProgressWatchedAtTheEnding(t *testing.T) {
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
	lib := &model.Library{Name: "A", Kind: model.KindSeries, Root: "/a"}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	rt := alyaMs
	if err := st.UpsertItem(ctx, &model.Item{ID: "e9", GUID: "g-e9", LibraryID: lib.ID, Type: model.TypeEpisode, Title: "Rom-commy", RuntimeMs: &rt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "s9", ItemID: "e9", DurationMs: alyaMs, Parts: []model.MediaPart{{ID: "s9-p", Path: "/a/Alya/S01E09.mkv", Size: 1, DurationMs: alyaMs}}}, "{}"); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSegmentsByOrigin(ctx, "s9", "chapters", []model.MediaSegment{{Type: "outro", StartMs: alyaEDMs, EndMs: 1_511_000, Origin: "chapters"}}); err != nil {
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
			t.Fatalf("progress %s: %d %s", body, rec.Code, rec.Body)
		}
	}
	get := func() *store.UserItemData {
		t.Helper()
		d, err := st.GetUserItemData(ctx, scope, "g-e9")
		if err != nil || d == nil {
			t.Fatalf("user data: %v %v", d, err)
		}
		return d
	}

	post(`{"itemId":"e9","positionMs":1363703,"sourceId":"s9"}`)
	if d := get(); d.Played || d.PositionMs != 1_363_703 {
		t.Fatalf("stopped in the last scene (22:43): played=%v pos=%d, want a resume point", d.Played, d.PositionMs)
	}
	post(`{"itemId":"e9","positionMs":1425000,"sourceId":"s9"}`)
	if d := get(); !d.Played {
		t.Fatalf("in the ED: not watched (pos %d)", d.PositionMs)
	}
}
