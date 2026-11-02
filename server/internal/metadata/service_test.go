package metadata

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// fakeProvider answers Match and Episodes from fixed data and counts the calls.
type fakeProvider struct {
	name     string
	result   *Result
	err      error
	seasons  map[int]*Season
	matches  int
	episodes int
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Match(ctx context.Context, it *model.Item, h Hints) (*Result, error) {
	f.matches++
	if f.err != nil {
		return nil, f.err
	}
	if f.result == nil {
		return nil, nil
	}
	r := *f.result
	return &r, nil
}
func (f *fakeProvider) Episodes(ctx context.Context, r *Result, n int) (*Season, error) {
	f.episodes++
	if r == nil || r.Provider != f.name {
		return nil, nil
	}
	return f.seasons[n], nil
}

// episodes makes a provider season list: episode i titled "<prefix><i+offset>".
func episodes(season, count, offset int, prefix string) *Season {
	se := &Season{Number: season}
	for i := 1; i <= count; i++ {
		se.Episodes = append(se.Episodes, EpisodeResult{Season: season, Episode: i, Title: fmt.Sprintf("%s%d", prefix, i+offset)})
	}
	return se
}

type svcFixture struct {
	t      *testing.T
	ctx    context.Context
	st     *store.Store
	svc    *Service
	lib    *model.Library
	series *model.Item
	tmdb   *fakeProvider
	al     *fakeProvider
}

func newFixture(t *testing.T, anime bool) *svcFixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "Lib", Kind: "series", Root: t.TempDir(), AnimeMode: anime}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	f := &svcFixture{t: t, ctx: ctx, st: st, lib: lib,
		tmdb: &fakeProvider{name: "tmdb", seasons: map[int]*Season{}},
		al:   &fakeProvider{name: "anilist", seasons: map[int]*Season{}},
	}
	f.svc = &Service{store: st, cfg: &config.Config{}, queue: make(chan job, 16), pending: map[string]bool{}, failed: map[string]time.Time{}, retries: map[string]int{}, retryDelay: time.Hour}
	f.svc.providers = []Provider{f.tmdb, f.al}
	f.svc.anilist = f.al
	f.series = &model.Item{ID: "series", GUID: "path:series:x", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"}
	f.upsert(f.series)
	return f
}

func (f *svcFixture) upsert(it *model.Item) {
	f.t.Helper()
	if err := f.st.UpsertItem(f.ctx, it); err != nil {
		f.t.Fatal(err)
	}
}

// realSeason adds a season folder holding real episodes 1..count.
func (f *svcFixture) realSeason(n, count int) {
	f.t.Helper()
	num := n
	se := &model.Item{ID: fmt.Sprintf("season%d", n), GUID: fmt.Sprintf("%s:season:%d", f.series.GUID, n), LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, Title: fmt.Sprintf("Season %d", n), IndexAired: &num}
	f.upsert(se)
	for e := 1; e <= count; e++ {
		ep, sn := e, n
		f.upsert(&model.Item{ID: fmt.Sprintf("s%de%d", n, e), GUID: fmt.Sprintf("%s:s%02de%04d", f.series.GUID, n, e), LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &se.ID, Title: fmt.Sprintf("Episode %d", e), IndexAired: &ep, SeasonAired: &sn})
	}
}

func (f *svcFixture) run(force bool) error {
	f.t.Helper()
	return f.svc.process(f.ctx, job{itemID: f.series.ID, force: force})
}

type epInfo struct {
	title   string
	virtual bool
}

// layout lists every season's episodes: season -> episode -> info.
func (f *svcFixture) layout() map[int]map[int]epInfo {
	f.t.Helper()
	out := map[int]map[int]epInfo{}
	seasons, err := f.st.Children(f.ctx, f.series.ID, model.TypeSeason)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, se := range seasons {
		m := map[int]epInfo{}
		eps, _ := f.st.Children(f.ctx, se.ID, model.TypeEpisode)
		for _, e := range eps {
			if e.IndexAired != nil {
				m[*e.IndexAired] = epInfo{e.Title, e.IsVirtual}
			}
		}
		out[*se.IndexAired] = m
	}
	return out
}

func countVirtual(m map[int]epInfo) (real, virtual int) {
	for _, e := range m {
		if e.virtual {
			virtual++
		} else {
			real++
		}
	}
	return
}

// JUJUTSU KAISEN: TMDB has one season of 59, the folders S1 (24) / S2 (23) / S3 (12).
// No placeholders may duplicate S2/S3 inside S1, the ones an earlier version made there
// go, and S2/S3 get their titles from TMDB's S1 by absolute position.
func TestSplitSeasonsMapByAbsolutePosition(t *testing.T) {
	f := newFixture(t, false)
	f.realSeason(0, 1)
	f.realSeason(1, 24)
	f.realSeason(2, 23)
	f.realSeason(3, 12)
	// Stale placeholders S1E25..59 from before the fix.
	for e := 25; e <= 59; e++ {
		ep, sn, parent := e, 1, "season1"
		if _, _, err := f.st.EnsureVirtualItem(f.ctx, &model.Item{ID: fmt.Sprintf("old%d", e), GUID: fmt.Sprintf("%s:s01e%04d", f.series.GUID, e), LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &parent, Title: "x", IndexAired: &ep, SeasonAired: &sn}); err != nil {
			t.Fatal(err)
		}
	}
	f.tmdb.result = &Result{Provider: "tmdb", ID: "95479", Title: "JUJUTSU KAISEN", ProviderIDs: map[string]string{"tmdb": "95479"},
		Seasons: []SeasonInfo{{Number: 0, EpisodeCount: 3}, {Number: 1, EpisodeCount: 59}}}
	f.tmdb.seasons[0] = episodes(0, 3, 0, "SP")
	f.tmdb.seasons[1] = episodes(1, 59, 0, "T")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	got := f.layout()
	if r, v := countVirtual(got[1]); r != 24 || v != 0 {
		t.Errorf("S1: %d real, %d placeholders; want 24 and none", r, v)
	}
	if r, v := countVirtual(got[2]); r != 23 || v != 0 {
		t.Errorf("S2: %d real, %d placeholders; want 23 and none", r, v)
	}
	if r, v := countVirtual(got[3]); r != 12 || v != 0 {
		t.Errorf("S3: %d real, %d placeholders; want 12 and none", r, v)
	}
	if got[1][24].title != "T24" || got[2][1].title != "T25" || got[2][23].title != "T47" || got[3][1].title != "T48" || got[3][12].title != "T59" {
		t.Errorf("titles by absolute position: S1E24=%q S2E1=%q S2E23=%q S3E1=%q S3E12=%q", got[1][24].title, got[2][1].title, got[2][23].title, got[3][1].title, got[3][12].title)
	}
	// Specials keep their behaviour: placeholders for what the library lacks.
	if r, v := countVirtual(got[0]); r != 1 || v != 2 || got[0][2].title != "SP2" {
		t.Errorf("specials: %d real, %d placeholders (%v); want 1 and 2", r, v, got[0])
	}
	if len(got) != 4 {
		t.Errorf("seasons = %d, want 0..3", len(got))
	}

	// TMDB adds episodes after the last real one and a second season: the last real
	// season shows the missing ones, the new season comes after it.
	f.tmdb.result.Seasons = []SeasonInfo{{Number: 0, EpisodeCount: 3}, {Number: 1, EpisodeCount: 62}, {Number: 2, EpisodeCount: 5}}
	f.tmdb.seasons[1] = episodes(1, 62, 0, "T")
	f.tmdb.seasons[2] = episodes(2, 5, 0, "N")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	got = f.layout()
	if r, v := countVirtual(got[3]); r != 12 || v != 3 || got[3][15].title != "T62" || !got[3][13].virtual {
		t.Errorf("S3 after TMDB grew: %d real, %d placeholders, E15=%q", r, v, got[3][15].title)
	}
	if r, v := countVirtual(got[4]); r != 0 || v != 5 || got[4][1].title != "N1" {
		t.Errorf("TMDB's season 2 as library S4: %d real, %d placeholders, E1=%q", r, v, got[4][1].title)
	}
	if r, v := countVirtual(got[1]); r != 24 || v != 0 {
		t.Errorf("S1 again: %d real, %d placeholders", r, v)
	}
}

// KonoSuba: the owner files the OVA as the season's last episode (S01E11), TMDB lists
// 10 per season and AniList counts the OVA in (11). That is not a split: the OVA stays a
// normal, real episode of S1 in order, nothing is renumbered, removed or duplicated.
func TestSeasonOVAFiledAsLastEpisodeStays(t *testing.T) {
	for _, anime := range []bool{false, true} {
		t.Run(fmt.Sprintf("anime=%v", anime), func(t *testing.T) {
			f := newFixture(t, anime)
			f.realSeason(1, 11)
			f.realSeason(2, 10)
			f.realSeason(3, 11)
			f.tmdb.result = &Result{Provider: "tmdb", ID: "65844", Title: "KONOSUBA", ProviderIDs: map[string]string{"tmdb": "65844"},
				Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 10}, {Number: 2, EpisodeCount: 10}, {Number: 3, EpisodeCount: 10}}}
			for n := 1; n <= 3; n++ {
				f.tmdb.seasons[n] = episodes(n, 10, 0, fmt.Sprintf("TMDB S%d E", n))
			}
			f.al.result = &Result{Provider: "anilist", ID: "21202", Title: "KONOSUBA", ProviderIDs: map[string]string{"anilist": "21202"},
				Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 11, ProviderID: "21202", MalID: "30831"}, {Number: 2, EpisodeCount: 10, ProviderID: "21699", MalID: "32937"}, {Number: 3, EpisodeCount: 11, ProviderID: "136804", MalID: "49458"}}}
			f.al.seasons[1] = episodes(1, 11, 0, "AL S1 E")
			if err := f.run(true); err != nil {
				t.Fatal(err)
			}
			got := f.layout()
			for n, want := range map[int]int{1: 11, 2: 10, 3: 11} {
				if r, v := countVirtual(got[n]); r != want || v != 0 {
					t.Errorf("S%d: %d real, %d placeholders; want %d real", n, r, v, want)
				}
			}
			if e := got[1][11]; e.virtual || e.title != "AL S1 E11" {
				t.Errorf("S01E11 (the OVA) = %+v; want a real episode titled from AniList", e)
			}
			if len(got) != 3 {
				t.Errorf("seasons = %d, want 3", len(got))
			}
			// AniSkip ids: S1's AniList entry counts 11 against TMDB's 10, one off: kept.
			if mal, _ := f.st.ProviderID(f.ctx, "season1", "mal"); mal != "30831" {
				t.Errorf("S1 mal = %q, want 30831", mal)
			}
		})
	}
}

// InuYasha: 167 + 26 on the provider, seven folders (27, 27, 28, 28, 28, 29, 26). S7 is
// the provider's second season exactly and gets that AniList entry's ids; S2 must not
// borrow the second entry's ids (it is the middle of the first).
func TestSplitSeasonIDsFollowTheLayout(t *testing.T) {
	f := newFixture(t, true)
	lens := []int{27, 27, 28, 28, 28, 29, 26}
	for i, n := range lens {
		f.realSeason(i+1, n)
	}
	f.al.result = &Result{Provider: "anilist", ID: "249", Title: "InuYasha", ProviderIDs: map[string]string{"anilist": "249"},
		Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 167, ProviderID: "249", MalID: "249"}, {Number: 2, EpisodeCount: 26, ProviderID: "6811", MalID: "6811"}}}
	f.al.seasons[2] = episodes(2, 26, 0, "FA")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	if mal, _ := f.st.ProviderID(f.ctx, "season7", "mal"); mal != "6811" {
		t.Errorf("S7 mal = %q, want the Final Act's 6811", mal)
	}
	for _, s := range []string{"season1", "season2", "season6"} {
		if mal, _ := f.st.ProviderID(f.ctx, s, "mal"); mal != "" {
			t.Errorf("%s mal = %q, want none", s, mal)
		}
	}
	got := f.layout()
	if got[7][1].title != "FA1" || got[7][26].title != "FA26" {
		t.Errorf("S7 titles = %q..%q", got[7][1].title, got[7][26].title)
	}
	if r, v := countVirtual(got[1]); r != 27 || v != 0 {
		t.Errorf("S1: %d real, %d placeholders", r, v)
	}
}

// A normal show (folders = TMDB seasons) keeps placeholders for missing episodes in
// every season, also before a later season with files.
func TestNormalSeasonsKeepMissingPlaceholders(t *testing.T) {
	f := newFixture(t, false)
	f.realSeason(1, 8)
	f.realSeason(2, 3)
	f.tmdb.result = &Result{Provider: "tmdb", ID: "1", Title: "Show", ProviderIDs: map[string]string{"tmdb": "1"},
		Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 10}, {Number: 2, EpisodeCount: 10}}}
	f.tmdb.seasons[1] = episodes(1, 10, 0, "A")
	f.tmdb.seasons[2] = episodes(2, 10, 0, "B")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	got := f.layout()
	if r, v := countVirtual(got[1]); r != 8 || v != 2 || got[1][10].title != "A10" {
		t.Errorf("S1: %d real, %d placeholders", r, v)
	}
	if r, v := countVirtual(got[2]); r != 3 || v != 7 || got[2][1].title != "B1" {
		t.Errorf("S2: %d real, %d placeholders, E1=%q", r, v, got[2][1].title)
	}
}

// AniList failing (429) for an anime must not let TMDB overwrite the title, art or
// related titles: the refresh is abandoned and re-queued.
func TestAniListErrorAbortsAnimeRefresh(t *testing.T) {
	f := newFixture(t, true)
	f.realSeason(1, 2)
	if err := f.st.UpdateItemMetadata(f.ctx, f.series.ID, store.ItemMetadata{Title: "AniList Title", ProviderIDs: map[string]string{"anilist": "1"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ReplaceRelations(f.ctx, f.series.ID, []store.ItemRelation{{Provider: "anilist", ProviderID: "2", Relation: "movie", Title: "The Movie"}}); err != nil {
		t.Fatal(err)
	}
	f.al.err = &httpError{Status: 429, Body: "Too Many Requests"}
	f.tmdb.result = &Result{Provider: "tmdb", ID: "9", Title: "TMDB Title", ProviderIDs: map[string]string{"tmdb": "9"}, Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 5}}}
	err := f.run(true)
	var down *errProviderDown
	if !errors.As(err, &down) || down.provider != "anilist" {
		t.Fatalf("err = %v, want anilist did not answer", err)
	}
	it, _ := f.st.GetItem(f.ctx, f.series.ID)
	if it.Title != "AniList Title" {
		t.Errorf("title = %q, overwritten", it.Title)
	}
	if rels, _ := f.st.RelationsFor(f.ctx, f.series.ID); len(rels) != 1 {
		t.Errorf("relations = %v, want the saved one kept", rels)
	}
	if r, v := countVirtual(f.layout()[1]); r != 2 || v != 0 {
		t.Errorf("placeholders made from TMDB after the error: %d real, %d virtual", r, v)
	}
	if f.svc.retries[f.series.ID] != 1 {
		t.Errorf("retries = %d, want a re-queue scheduled", f.svc.retries[f.series.ID])
	}
	if at, _ := f.st.MetadataAttemptedAt(f.ctx, f.series.ID); at != nil {
		t.Errorf("a failed attempt must not count as tried: %v", at)
	}
}

// Outside anime, a title AniList never matched falls back to TMDB on an AniList error,
// but the related titles (AniList's) are not wiped.
func TestAniListErrorKeepsRelationsOutsideAnime(t *testing.T) {
	f := newFixture(t, false)
	if err := f.st.ReplaceRelations(f.ctx, f.series.ID, []store.ItemRelation{{Provider: "anilist", ProviderID: "2", Relation: "movie", Title: "The Movie"}}); err != nil {
		t.Fatal(err)
	}
	f.al.err = errors.New("connection reset")
	f.tmdb.result = &Result{Provider: "tmdb", ID: "9", Title: "TMDB Title", ProviderIDs: map[string]string{"tmdb": "9"}}
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	it, _ := f.st.GetItem(f.ctx, f.series.ID)
	if it.Title != "TMDB Title" {
		t.Errorf("title = %q", it.Title)
	}
	if rels, _ := f.st.RelationsFor(f.ctx, f.series.ID); len(rels) != 1 {
		t.Errorf("relations = %v, want kept", rels)
	}
}

// A restart must not re-match a series whose only unfilled episodes are ones no
// provider could fill at the last attempt (an episode TMDB has no data for).
func TestRestartDoesNotRematchUnfillableEpisodes(t *testing.T) {
	f := newFixture(t, false)
	f.realSeason(1, 3)
	f.tmdb.result = &Result{Provider: "tmdb", ID: "9", Title: "Show", ProviderIDs: map[string]string{"tmdb": "9"}, Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 2}}}
	f.tmdb.seasons[1] = episodes(1, 2, 0, "T") // nothing for E3
	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	if f.tmdb.matches != 1 {
		t.Fatalf("first run matched %d times", f.tmdb.matches)
	}
	// A restart: a new service, empty in-memory backoff.
	f.svc.failed = map[string]time.Time{}
	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	if f.tmdb.matches != 1 {
		t.Errorf("restart re-matched (%d matches) for an episode nobody can fill", f.tmdb.matches)
	}
	// A new episode file does bring it back.
	time.Sleep(5 * time.Millisecond)
	ep, sn, parent := 4, 1, "season1"
	f.upsert(&model.Item{ID: "s1e4", GUID: f.series.GUID + ":s01e0004", LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &parent, Title: "Episode 4", IndexAired: &ep, SeasonAired: &sn})
	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	if f.tmdb.matches != 2 {
		t.Errorf("a new episode should trigger a lookup, matches = %d", f.tmdb.matches)
	}
}

// One Pace: "Wano 01.mp4" carries no number the scanner reads; its .nfo's <episode>
// orders it inside the season.nfo's season.
func TestLocalEpisodeNumberFromNFO(t *testing.T) {
	f := newFixture(t, true)
	show := filepath.Join(f.lib.Root, "One Pace")
	arc := filepath.Join(show, "[One Pace][909-1057] Wano")
	if err := os.MkdirAll(arc, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(show, "tvshow.nfo"), `<tvshow><title>One Pace</title></tvshow>`)
	writeFile(t, filepath.Join(arc, "season.nfo"), `<season><title>Wano</title><seasonnumber>35</seasonnumber></season>`)
	num := 1
	loose := &model.Item{ID: "loose", GUID: f.series.GUID + ":season:1", LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, Title: "Season 1", IndexAired: &num}
	f.upsert(loose)
	for i := 1; i <= 2; i++ {
		p := filepath.Join(arc, fmt.Sprintf("Wano %02d.mp4", i))
		writeFile(t, p, "x")
		writeFile(t, filepath.Join(arc, fmt.Sprintf("Wano %02d.nfo", i)), fmt.Sprintf(`<episodedetails><title>Wano part %d</title><episode>%d</episode></episodedetails>`, i, i))
		id := fmt.Sprintf("wano%d", i)
		sn := 1
		f.upsert(&model.Item{ID: id, GUID: "file:" + id, LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &loose.ID, Title: "Wano", SeasonAired: &sn})
		src := &model.MediaSource{ID: "src" + id, ItemID: id, IsPrimary: true, Parts: []model.MediaPart{{ID: "part" + id, Index: 0, Path: p, ModTime: time.Now()}}}
		if err := f.st.ReplaceSource(f.ctx, src, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		it, _ := f.st.GetItem(f.ctx, fmt.Sprintf("wano%d", i))
		if it.IndexAired == nil || *it.IndexAired != i || it.SeasonAired == nil || *it.SeasonAired != 35 || it.Title != fmt.Sprintf("Wano part %d", i) {
			t.Errorf("wano%d: index %v season %v title %q", i, it.IndexAired, it.SeasonAired, it.Title)
		}
	}
	if f.tmdb.matches+f.al.matches != 0 {
		t.Errorf("a show described by its .nfo must not be looked up")
	}
}

// Placeholders follow their series into its library (bug: TV placeholders kept the
// Movies library id).
func TestPlaceholdersTakeTheSeriesLibrary(t *testing.T) {
	f := newFixture(t, false)
	other := &model.Library{Name: "Movies", Kind: "movies", Root: t.TempDir()}
	if err := f.st.CreateLibrary(f.ctx, other); err != nil {
		t.Fatal(err)
	}
	f.realSeason(1, 1)
	// A placeholder season + episode under the wrong library, as the live data has.
	n2, e1 := 2, 1
	vs := &model.Item{ID: "vseason", GUID: f.series.GUID + ":season:2", LibraryID: other.ID, Type: model.TypeSeason, ParentID: &f.series.ID, Title: "Season 2", IndexAired: &n2}
	if _, _, err := f.st.EnsureVirtualItem(f.ctx, vs); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.EnsureVirtualItem(f.ctx, &model.Item{ID: "vep", GUID: f.series.GUID + ":s02e0001", LibraryID: other.ID, Type: model.TypeEpisode, ParentID: &vs.ID, Title: "x", IndexAired: &e1, SeasonAired: &n2}); err != nil {
		t.Fatal(err)
	}
	f.tmdb.result = &Result{Provider: "tmdb", ID: "9", Title: "Show", ProviderIDs: map[string]string{"tmdb": "9"}, Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 1}, {Number: 2, EpisodeCount: 2}}}
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"vseason", "vep"} {
		it, _ := f.st.GetItem(f.ctx, id)
		if it == nil || it.LibraryID != f.lib.ID {
			t.Errorf("%s library = %v, want %d", id, it, f.lib.ID)
		}
	}
	got := f.layout()
	if r, v := countVirtual(got[2]); r != 0 || v != 2 {
		t.Errorf("S2: %d real, %d virtual", r, v)
	}
}
