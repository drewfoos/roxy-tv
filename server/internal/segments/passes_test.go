package segments

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// seasonFixture is a season of count 24-minute episodes, each with a file on record.
func seasonFixture(t *testing.T, count int) (*Service, *store.Store, string) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	lib := &model.Library{Name: "Anime", Kind: "series", Root: t.TempDir()}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	series := &model.Item{ID: "series", GUID: "g:series", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"}
	one := 1
	season := &model.Item{ID: "season", GUID: "g:season", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series.ID, Title: "Season 1", IndexAired: &one}
	for _, it := range []*model.Item{series, season} {
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for e := 1; e <= count; e++ {
		n := e
		ep := &model.Item{ID: fmt.Sprintf("e%d", e), GUID: fmt.Sprintf("g:e%d", e), LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID, Title: fmt.Sprintf("Episode %d", e), IndexAired: &n, SeasonAired: &one}
		if err := st.UpsertItem(ctx, ep); err != nil {
			t.Fatal(err)
		}
		if err := st.ReplaceSource(ctx, episodeFile(lib.Root, e, 1000), "{}"); err != nil {
			t.Fatal(err)
		}
	}
	return &Service{store: st, pending: map[string]pendingSeason{}, wake: make(chan struct{}, 1)}, st, season.ID
}

// episodeFile is episode e's file as the scanner records it; another size is another file
// at the same path.
func episodeFile(root string, e int, size int64) *model.MediaSource {
	return &model.MediaSource{ID: fmt.Sprintf("src%d", e), ItemID: fmt.Sprintf("e%d", e), IsPrimary: true, DurationMs: 1440000, Size: size,
		Parts: []model.MediaPart{{ID: fmt.Sprintf("part%d", e), Path: filepath.Join(root, fmt.Sprintf("Show - S01E%02d.mkv", e)), Size: size, ModTime: time.Unix(1700000000+size, 0)}}}
}

// An episode whose audio can never be fingerprinted (no sound, a file cut short) used to
// stay "never worked on" for good, so every sweep ran its whole season again. It gets a
// few passes, then it is left alone until the file changes.
func TestUnfingerprintableEpisodeIsGivenUpOn(t *testing.T) {
	ctx := context.Background()
	s, st, season := seasonFixture(t, 3)
	fp := []uint32{1, 2, 3}
	heads := [][]uint32{fp, nil, fp} // episode 2: no fingerprint
	tails := [][]uint32{fp, fp, fp}
	pass := func() bool {
		t.Helper()
		eps, err := st.SeasonEpisodes(ctx, season)
		if err != nil || len(eps) != 3 {
			t.Fatalf("episodes: %d, %v", len(eps), err)
		}
		s.markDetected(ctx, season, eps, heads, tails)
		if eps, err = st.SeasonEpisodes(ctx, season); err != nil {
			t.Fatal(err)
		}
		return needsFingerprints(eps)
	}
	for n := 1; n < maxFailedPasses; n++ {
		if !pass() {
			t.Fatalf("after %d failed pass(es) the episode is not tried again", n)
		}
	}
	if pass() {
		t.Fatalf("after %d failed passes the season still comes back on every sweep", maxFailedPasses)
	}

	// A pass cut short by a shutdown says nothing about the file.
	if err := st.ReplaceSource(ctx, episodeFile(filepath.Dir(mustPath(t, st, season, 1)), 2, 2000), "{}"); err != nil {
		t.Fatal(err)
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	eps, _ := st.SeasonEpisodes(ctx, season)
	if eps[1].FailedPasses != 0 || eps[1].DetectVersion != 0 {
		t.Fatalf("a new file at the path starts over; got %d failed passes, version %d", eps[1].FailedPasses, eps[1].DetectVersion)
	}
	s.markDetected(stopped, season, eps, heads, tails)
	if eps, _ = st.SeasonEpisodes(ctx, season); eps[1].FailedPasses != 0 {
		t.Errorf("an interrupted pass counted as a failure of the file: %d", eps[1].FailedPasses)
	}
}

func mustPath(t *testing.T, st *store.Store, season string, i int) string {
	t.Helper()
	eps, err := st.SeasonEpisodes(context.Background(), season)
	if err != nil || len(eps) <= i {
		t.Fatalf("episodes: %d, %v", len(eps), err)
	}
	return eps[i].Path
}

// The episodes two away are compared only when neither direct neighbour shares anything.
// For the first episode (no episode before it) they were compared anyway, and a longer
// run shared with episode 3 (a recap, a long cold open) replaced the opening it shares
// with episode 2.
func TestBestNeighbourPrefersDirectNeighbours(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	random := func(sec float64) []uint32 {
		p := make([]uint32, int(sec/SampleSec))
		for i := range p {
			p[i] = rng.Uint32()
		}
		return p
	}
	join := func(parts ...[]uint32) []uint32 {
		var out []uint32
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	op, long := random(90), random(120)
	ep1 := join(random(20), op, random(30), long, random(100))
	ep2 := join(random(40), op, random(300))
	ep3 := join(random(60), long, random(300))
	fps := [][]uint32{ep1, ep2, ep3}

	m, with, ok := bestNeighbour(fps, 0, MinIntroSec, MaxIntroSec)
	if !ok || with != 1 {
		t.Fatalf("first episode matched with index %d (ok %v), want its direct neighbour 1", with, ok)
	}
	if m.length() < 85 || m.length() > 95 {
		t.Errorf("shared run %.1f s, want the 90 s opening", m.length())
	}
	// The last episode, the same way round.
	if _, with, ok := bestNeighbour([][]uint32{ep3, ep2, ep1}, 2, MinIntroSec, MaxIntroSec); !ok || with != 1 {
		t.Errorf("last episode matched with index %d (ok %v), want 1", with, ok)
	}
	// Nothing shared with a direct neighbour: the one two away is still used.
	if _, with, ok := bestNeighbour([][]uint32{ep1, random(400), ep3}, 0, MinIntroSec, MaxIntroSec); !ok || with != 2 {
		t.Errorf("no direct match: matched with index %d (ok %v), want 2", with, ok)
	}
}

// One episode AniSkip cannot answer for (a 500 for that entry) must not keep the rest of
// the season from being asked about: the pass used to stop at the first error, on every
// pass, so the episodes after it never got their intro.
func TestAniSkipCarriesOnPastOneFailedEpisode(t *testing.T) {
	ctx := context.Background()
	s, st, season := seasonFixture(t, 3)
	if err := st.SetProviderIDs(ctx, season, map[string]string{"mal": "42"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/skip-times/42/2" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"found":true,"results":[{"interval":{"startTime":60,"endTime":150},"skipType":"op","episodeLength":1440}],"statusCode":200}`))
	}))
	defer srv.Close()
	s.aniskip = NewAniSkip()
	s.aniskip.base = srv.URL

	s.fillFromAniSkip(ctx, season, false, &Report{SeasonID: season})
	eps, err := st.SeasonEpisodes(ctx, season)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{true, false, true} {
		if got := hasType(eps[i].Segments, "intro"); got != want {
			t.Errorf("episode %d: intro from AniSkip = %v, want %v", i+1, got, want)
		}
	}
	if eps[1].AniSkipAsked {
		t.Error("the episode whose lookup failed is recorded as asked: it would never be tried again")
	}
	if !eps[2].AniSkipAsked {
		t.Error("the episode after the failed one was not asked about")
	}
}
