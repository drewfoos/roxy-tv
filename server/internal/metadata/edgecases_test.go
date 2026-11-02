package metadata

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// fileEpisode adds a real episode backed by a media file with the given name.
func (f *svcFixture) fileEpisode(season, ep int, name string, abs *int) *model.Item {
	f.t.Helper()
	num := season
	seasonID := fmt.Sprintf("season%d", season)
	f.upsert(&model.Item{ID: seasonID, GUID: fmt.Sprintf("%s:season:%d", f.series.GUID, season), LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, Title: fmt.Sprintf("Season %d", season), IndexAired: &num})
	e, sn := ep, season
	it := &model.Item{ID: fmt.Sprintf("s%de%d", season, ep), GUID: fmt.Sprintf("%s:s%02de%04d", f.series.GUID, season, ep), LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &seasonID, Title: name, IndexAired: &e, SeasonAired: &sn, IndexAbsolute: abs}
	f.upsert(it)
	p := filepath.Join(f.lib.Root, "Show", fmt.Sprintf("Season %02d", season), name)
	src := &model.MediaSource{ID: "src" + it.ID, ItemID: it.ID, IsPrimary: true, Parts: []model.MediaPart{{ID: "part" + it.ID, Index: 0, Path: p, ModTime: time.Now()}}}
	if err := f.st.ReplaceSource(f.ctx, src, ""); err != nil {
		f.t.Fatal(err)
	}
	return it
}

func (f *svcFixture) placeholder(season, ep int, title string) {
	f.t.Helper()
	e, sn := ep, season
	parent := fmt.Sprintf("season%d", season)
	if _, _, err := f.st.EnsureVirtualItem(f.ctx, &model.Item{ID: fmt.Sprintf("ph-s%de%d", season, ep), GUID: fmt.Sprintf("%s:s%02de%04d", f.series.GUID, season, ep), LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &parent, Title: title, IndexAired: &e, SeasonAired: &sn}); err != nil {
		f.t.Fatal(err)
	}
}

func titled(season int, titles ...string) *Season {
	se := &Season{Number: season}
	for i, t := range titles {
		se.Episodes = append(se.Episodes, EpisodeResult{Season: season, Episode: i + 1, Title: t})
	}
	return se
}

// Mushoku Tensei: AniList's first-season entry carries the second season's 25 titles.
// TMDB's titles win, and the placeholder AniList's longer list made (S01E24) goes.
func TestTMDBTitlesWinAndAniListCountDoesNotMakePlaceholders(t *testing.T) {
	f := newFixture(t, true)
	f.realSeason(1, 23)
	f.placeholder(1, 24, "Succession")
	f.tmdb.result = &Result{Provider: "tmdb", ID: "94664", Title: "Mushoku Tensei", ProviderIDs: map[string]string{"tmdb": "94664"}, Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 23}}}
	f.tmdb.seasons[1] = episodes(1, 23, 0, "TMDB ")
	f.al.result = &Result{Provider: "anilist", ID: "108465", Title: "Mushoku Tensei", ProviderIDs: map[string]string{"anilist": "108465"}, Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 25, ProviderID: "108465"}}}
	f.al.seasons[1] = episodes(1, 25, 0, "S2 title ")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	got := f.layout()
	if r, v := countVirtual(got[1]); r != 23 || v != 0 {
		t.Errorf("S1: %d real, %d placeholders; want 23 and none", r, v)
	}
	if got[1][1].title != "TMDB 1" || got[1][23].title != "TMDB 23" {
		t.Errorf("titles %q..%q, want TMDB's", got[1][1].title, got[1][23].title)
	}
}

// Yu-Gi-Oh!: AniList's sequel chain (Duel Monsters, GX, 5D's) is not the show's seasons;
// TMDB lists one season of 224. Placeholders follow TMDB, and the fake seasons go.
func TestPlaceholdersFollowTMDBNotTheAniListChain(t *testing.T) {
	f := newFixture(t, true)
	f.realSeason(1, 3)
	n2 := 2
	f.upsert(&model.Item{ID: "season2", GUID: f.series.GUID + ":season:2", LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, Title: "Season 2", IndexAired: &n2, IsVirtual: true})
	f.placeholder(2, 1, "GX 1")
	f.tmdb.result = &Result{Provider: "tmdb", ID: "902", Title: "Yu-Gi-Oh!", ProviderIDs: map[string]string{"tmdb": "902"}, Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 224}}}
	f.tmdb.seasons[1] = episodes(1, 224, 0, "DM ")
	f.al.result = &Result{Provider: "anilist", ID: "481", Title: "Yu-Gi-Oh! Duel Monsters", ProviderIDs: map[string]string{"anilist": "481"},
		Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 224, ProviderID: "481"}, {Number: 2, EpisodeCount: 180, ProviderID: "482"}, {Number: 3, EpisodeCount: 154, ProviderID: "483"}}}
	f.al.seasons[2] = episodes(2, 180, 0, "GX ")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	got := f.layout()
	if len(got) != 1 {
		t.Errorf("seasons %d, want only season 1", len(got))
	}
	if r, v := countVirtual(got[1]); r != 3 || v != 221 {
		t.Errorf("S1: %d real, %d placeholders; want 3 and 221", r, v)
	}
}

// KonoSuba's OVA filed as S01E11 arrives where a placeholder made from a wrong list
// (S3's finale and still) stood: the file's own title shows, not the placeholder's, and
// no later lookup brings the wrong one back.
func TestPlaceholderBecomingRealDropsItsGuess(t *testing.T) {
	f := newFixture(t, true)
	f.realSeason(1, 10)
	f.placeholder(1, 11, "God's Blessing for These Unchanging Days!")
	if err := f.st.UpdateItemMetadata(f.ctx, "ph-s1e11", store.ItemMetadata{Title: "God's Blessing for These Unchanging Days!", Overview: "S3 finale"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetItemImage(f.ctx, store.ItemImage{ItemID: "ph-s1e11", Type: "thumb", Path: "x.jpg", SourceURL: "https://img1.ak.crunchyroll.com/s3e11.jpg"}); err != nil {
		t.Fatal(err)
	}
	// The scanner finds the file under the placeholder's GUID.
	e, sn, parent := 11, 1, "season1"
	file := &model.Item{ID: "new", GUID: f.series.GUID + ":s01e0011", LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &parent, Title: "Gods Blessings On This Wonderful Choker!", IndexAired: &e, SeasonAired: &sn}
	f.upsert(file)
	it, _ := f.st.GetItem(f.ctx, "ph-s1e11")
	if it.IsVirtual || it.Title != "Gods Blessings On This Wonderful Choker!" || it.Overview != "" || it.MetadataRefreshedAt != nil {
		t.Fatalf("flipped item: virtual=%v title=%q overview=%q refreshed=%v", it.IsVirtual, it.Title, it.Overview, it.MetadataRefreshedAt)
	}
	if img, _ := f.st.GetItemImage(f.ctx, it.ID, "thumb"); img != nil {
		t.Errorf("the placeholder's still was kept: %+v", img)
	}
	if time.Since(it.DateCreated) > time.Minute {
		t.Errorf("a file that just arrived is newly added, created %v", it.DateCreated)
	}
	// TMDB has 10 episodes; AniList's S1 list was another season's (no data).
	f.tmdb.result = &Result{Provider: "tmdb", ID: "65844", Title: "KONOSUBA", ProviderIDs: map[string]string{"tmdb": "65844"}, Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 10}}}
	f.tmdb.seasons[1] = episodes(1, 10, 0, "T")
	f.al.result = &Result{Provider: "anilist", ID: "21202", Title: "KONOSUBA", ProviderIDs: map[string]string{"anilist": "21202"}, Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 10, ProviderID: "21202", NoEpisodeData: true}}}
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	got := f.layout()
	if e := got[1][11]; e.virtual || e.title != "Gods Blessings On This Wonderful Choker!" {
		t.Errorf("S01E11 = %+v", e)
	}
	if got[1][1].title != "T1" {
		t.Errorf("S01E01 = %q", got[1][1].title)
	}
}

// Sonarr numbers specials (and some episodes) the TVDB way; TMDB differently. A file
// whose name carries the episode title gets that episode's data, a special TMDB does not
// have keeps its own name, and the TMDB episode a file claimed by title shows no
// "missing" placeholder.
func TestEpisodeMatchedByFileTitle(t *testing.T) {
	f := newFixture(t, false)
	f.fileEpisode(0, 1, "JUJUTSU KAISEN - S00E01 - Jujutsu Kaisen 0 [Bluray-1080p].mkv", nil)
	f.fileEpisode(0, 3, "KonoSuba - S00E03 - Gods Blessings On This Wonderful Choker! [Bluray-1080p][Opus 2.0][x265]-Group.mkv", nil)
	f.fileEpisode(0, 4, "KonoSuba - S00E04 - Gods Blessings on These Wonderful Works of Art!.mkv", nil)
	f.fileEpisode(0, 5, "Mushoku Tensei - S00E05 - S1 sp 17.5.mkv", nil) // a label, not a name: the number decides
	f.fileEpisode(1, 1, "SpongeBob - S01E01 - Help Wanted.mkv", nil)
	f.fileEpisode(1, 2, "SpongeBob - S01E02 - Reef Blower.mkv", nil)
	f.fileEpisode(1, 3, "SpongeBob - S01E03 - Something TMDB Spells Differently.mkv", nil)
	// An earlier lookup already wrote the wrong special's data.
	if err := f.st.UpdateItemMetadata(f.ctx, "s0e1", store.ItemMetadata{Title: "New Year's Special", Overview: "wrong"}); err != nil {
		t.Fatal(err)
	}
	f.tmdb.result = &Result{Provider: "tmdb", ID: "1", Title: "Show", ProviderIDs: map[string]string{"tmdb": "1"}, Seasons: []SeasonInfo{{Number: 0, EpisodeCount: 5}, {Number: 1, EpisodeCount: 3}}}
	f.tmdb.seasons[0] = titled(0, "New Year's Special", "God's Blessings on This Wonderful Choker!", "Red Stream Explosion", "God's Blessings on These Wonderful Works of Art!", "Eris the Goblin Slayer")
	f.tmdb.seasons[1] = titled(1, "Help Wanted", "Tea at the Treedome", "Reef Blower")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	got := f.layout()
	want := map[int]map[int]string{
		0: {1: "Jujutsu Kaisen 0", 3: "God's Blessings on This Wonderful Choker!", 4: "God's Blessings on These Wonderful Works of Art!", 5: "Eris the Goblin Slayer"},
		1: {1: "Help Wanted", 2: "Reef Blower", 3: "Reef Blower"},
	}
	for sn, eps := range want {
		for en, title := range eps {
			if g := got[sn][en]; g.virtual || g.title != title {
				t.Errorf("S%02dE%02d = %+v, want %q", sn, en, g, title)
			}
		}
	}
	jjk, _ := f.st.GetItem(f.ctx, "s0e1")
	if jjk.Overview != "" {
		t.Errorf("JJK 0 kept the wrong special's overview %q", jjk.Overview)
	}
	// TMDB's S00E02 (Choker) is the S00E03 file: no placeholder for it.
	if r, v := countVirtual(got[0]); r != 4 || v != 0 {
		t.Errorf("specials: %d real, %d placeholders %v; want 4 and none", r, v, got[0])
	}
}

// A file holding several episodes (S01E01-E02) covers all of them: no "missing"
// placeholder for E2, also for a file stored before the range was recorded.
func TestMultiEpisodeFileCoversItsRange(t *testing.T) {
	f := newFixture(t, false)
	f.fileEpisode(1, 1, "SpongeBob - S01E01-E02 - Help Wanted + Reef Blower.mkv", nil)
	f.placeholder(1, 2, "Reef Blower") // made by an earlier version
	f.tmdb.result = &Result{Provider: "tmdb", ID: "1", Title: "Show", ProviderIDs: map[string]string{"tmdb": "1"}, Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 4}}}
	f.tmdb.seasons[1] = titled(1, "Help Wanted", "Reef Blower", "Tea at the Treedome", "Bubblestand")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	got := f.layout()
	if r, v := countVirtual(got[1]); r != 1 || v != 2 || !got[1][3].virtual || !got[1][4].virtual {
		t.Errorf("S1: %d real, %d placeholders %v; want the file and E3, E4", r, v, got[1])
	}
	if got[1][1].title != "Help Wanted" {
		t.Errorf("E1 = %q", got[1][1].title)
	}
	it, _ := f.st.GetItem(f.ctx, "s1e1")
	if it.IndexAiredEnd == nil || *it.IndexAiredEnd != 2 {
		t.Errorf("end = %v, want 2", it.IndexAiredEnd)
	}
}

// InuYasha's "The Final Act" folder (S7) holds files named "- 01".."- 26" (absolute
// numbers to the scanner) next to S1..S6 named S01E01..: they are S7's own episodes (the
// provider's second season), not episodes 1..26 of the show again.
func TestAbsoluteNumbersRestartingInALaterSeasonFolder(t *testing.T) {
	f := newFixture(t, true)
	lens := []int{27, 27, 28, 28, 28, 29}
	for i, n := range lens {
		f.realSeason(i+1, n)
	}
	num := 7
	f.upsert(&model.Item{ID: "season7", GUID: f.series.GUID + ":season:7", LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, Title: "Season 7", IndexAired: &num})
	for e := 1; e <= 26; e++ {
		ep, abs, sn, parent := e, e, 7, "season7"
		f.upsert(&model.Item{ID: fmt.Sprintf("s7e%d", e), GUID: fmt.Sprintf("%s:abs%04d", f.series.GUID, e), LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &parent, Title: "x", IndexAired: &ep, SeasonAired: &sn, IndexAbsolute: &abs})
	}
	f.al.result = &Result{Provider: "anilist", ID: "249", Title: "InuYasha", ProviderIDs: map[string]string{"anilist": "249"},
		Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 167, ProviderID: "249"}, {Number: 2, EpisodeCount: 26, ProviderID: "6811"}}}
	f.al.seasons[1] = episodes(1, 167, 0, "IY")
	f.al.seasons[2] = episodes(2, 26, 0, "FA")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	got := f.layout()
	if got[7][1].title != "FA1" || got[7][26].title != "FA26" {
		t.Errorf("S7 = %q..%q, want the Final Act's titles", got[7][1].title, got[7][26].title)
	}
	if got[1][1].title != "IY1" || got[2][1].title != "IY28" {
		t.Errorf("S1E1 = %q, S2E1 = %q", got[1][1].title, got[2][1].title)
	}
	if r, v := countVirtual(got[7]); r != 26 || v != 0 {
		t.Errorf("S7: %d real, %d placeholders", r, v)
	}
}

// An absolute-numbered season folder that continues the count (S2 holding 13..24) still
// maps by absolute number.
func TestAbsoluteNumbersContinuingInALaterSeasonFolder(t *testing.T) {
	f := newFixture(t, false)
	for season, rng := range map[int][2]int{1: {1, 12}, 2: {13, 24}} {
		num := season
		f.upsert(&model.Item{ID: fmt.Sprintf("season%d", season), GUID: fmt.Sprintf("%s:season:%d", f.series.GUID, season), LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, Title: "S", IndexAired: &num})
		for a := rng[0]; a <= rng[1]; a++ {
			ep, abs, sn, parent := a, a, season, fmt.Sprintf("season%d", season)
			f.upsert(&model.Item{ID: fmt.Sprintf("a%d", a), GUID: fmt.Sprintf("%s:s%02dabs%04d", f.series.GUID, season, a), LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &parent, Title: "x", IndexAired: &ep, SeasonAired: &sn, IndexAbsolute: &abs})
		}
	}
	f.tmdb.result = &Result{Provider: "tmdb", ID: "1", Title: "Show", ProviderIDs: map[string]string{"tmdb": "1"}, Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 12}, {Number: 2, EpisodeCount: 12}}}
	f.tmdb.seasons[1] = episodes(1, 12, 0, "A")
	f.tmdb.seasons[2] = episodes(2, 12, 0, "B")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	if it, _ := f.st.GetItem(f.ctx, "a13"); it.Title != "B1" {
		t.Errorf("absolute 13 = %q, want B1", it.Title)
	}
}
