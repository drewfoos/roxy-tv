package metadata

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// fileEpisode adds an episode backed by a real file in the show folder.
func (f *svcFixture) nfoEpisode(dir, name, id string, season, episode int) string {
	f.t.Helper()
	sid := fmt.Sprintf("season%d", season)
	sn, en := season, episode
	f.upsert(&model.Item{ID: sid, GUID: fmt.Sprintf("%s:season:%d", f.series.GUID, season), LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, Title: "S", IndexAired: &sn})
	f.upsert(&model.Item{ID: id, GUID: f.series.GUID + ":" + id, LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &sid, IndexAired: &en, SeasonAired: &sn, Title: name})
	path := filepath.Join(dir, name)
	writeFile(f.t, path, "x")
	if err := f.st.ReplaceSource(f.ctx, &model.MediaSource{ID: "src-" + id, ItemID: id, IsPrimary: true,
		Parts: []model.MediaPart{{ID: "part-" + id, Path: path}}}, "{}"); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	img.Set(0, 0, color.White)
	fh, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	if err := png.Encode(fh, img); err != nil {
		t.Fatal(err)
	}
}

// A show whose tvshow.nfo names a provider id: the lookup runs, and then the .nfo's own
// fields, season names, every episode's .nfo (placing a special in season 0, as the
// SpongeBob fix needs) and the local poster win. Episode .nfo files used to be read only
// for shows whose .nfo had no ids at all.
func TestNFOWithIDsOverlaysTheLookup(t *testing.T) {
	f := newFixture(t, false)
	data := t.TempDir()
	safefs.Init(data)
	f.svc.cfg = &config.Config{DataDir: data}
	show := filepath.Join(f.lib.Root, "SpongeBob")
	if err := os.MkdirAll(show, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(show, "tvshow.nfo"), `<tvshow><title>SpongeBob SquarePants</title><sorttitle>Sponge</sorttitle>
		<uniqueid type="tvdb">75886</uniqueid><namedseason number="1">Early Days</namedseason></tvshow>`)
	writePNG(t, filepath.Join(show, "Poster.PNG")) // any case
	special := f.nfoEpisode(show, "SpongeBob - S02E08.mkv", "sp", 2, 8)
	writeFile(t, filepath.Join(show, "SpongeBob - S02E08.nfo"), `<episodedetails><title>Christmas Who?</title><season>0</season><episode>1</episode>
		<plot>The Christmas special.</plot><uniqueid type="tvdb">369</uniqueid></episodedetails>`)
	f.nfoEpisode(show, "SpongeBob - S01E01.mkv", "e1", 1, 1)

	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	ser, _ := f.st.GetItem(f.ctx, "series")
	if ser.SortTitle != "Sponge" || ser.ProviderIDs["tvdb"] != "75886" {
		t.Fatalf("series: sort %q ids %v", ser.SortTitle, ser.ProviderIDs)
	}
	ep, _ := f.st.GetItem(f.ctx, "sp")
	if ep.Title != "Christmas Who?" || ep.IndexAired == nil || *ep.IndexAired != 1 || ep.SeasonAired == nil || *ep.SeasonAired != 0 {
		t.Fatalf("special: %q S%v E%v", ep.Title, ep.SeasonAired, ep.IndexAired)
	}
	if ep.ProviderIDs["tvdb"] != "369" {
		t.Fatalf("episode ids: %v", ep.ProviderIDs)
	}
	parent, _ := f.st.GetItem(f.ctx, *ep.ParentID)
	if parent.IndexAired == nil || *parent.IndexAired != 0 || parent.Title != "Specials" {
		t.Fatalf("special's season: %+v", parent)
	}
	se1, _ := f.st.GetItemByGUID(f.ctx, f.series.GUID+":season:1")
	if se1 == nil || se1.Title != "Early Days" {
		t.Fatalf("named season: %+v", se1)
	}
	if img, err := f.st.GetItemImage(f.ctx, "series", "poster"); err != nil || img == nil {
		t.Fatalf("local poster not used: %v %v", img, err)
	}
	_ = special
}

// A multi-episode file's .nfo holds two <episodedetails>: the episode spans both and is
// titled after both. <episodenumberend> does the same in one block.
func TestNFOMultiEpisode(t *testing.T) {
	f := newFixture(t, false)
	show := filepath.Join(f.lib.Root, "Show")
	if err := os.MkdirAll(show, 0o755); err != nil {
		t.Fatal(err)
	}
	f.nfoEpisode(show, "Show - S01E01-E02.mkv", "a", 1, 1)
	writeFile(t, filepath.Join(show, "Show - S01E01-E02.nfo"), `<episodedetails><title>Part One</title><season>1</season><episode>1</episode></episodedetails>
<episodedetails><title>Part Two</title><season>1</season><episode>2</episode></episodedetails>`)
	f.nfoEpisode(show, "Show - S01E03.mkv", "b", 1, 3)
	writeFile(t, filepath.Join(show, "Show - S01E03.nfo"), `<episodedetails><title>Three to Five</title><season>1</season><episode>3</episode><episodenumberend>5</episodenumberend></episodedetails>`)
	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	a, _ := f.st.GetItem(f.ctx, "a")
	if a.Title != "Part One / Part Two" || a.IndexAiredEnd == nil || *a.IndexAiredEnd != 2 {
		t.Fatalf("two blocks: %q end %v", a.Title, a.IndexAiredEnd)
	}
	b, _ := f.st.GetItem(f.ctx, "b")
	if b.IndexAiredEnd == nil || *b.IndexAiredEnd != 5 {
		t.Fatalf("episodenumberend: end %v", b.IndexAiredEnd)
	}
}

// <lockdata>true</lockdata>: the .nfo is the metadata even though it names an id; no
// provider is asked.
func TestNFOLockData(t *testing.T) {
	f := newFixture(t, false)
	show := filepath.Join(f.lib.Root, "Show")
	if err := os.MkdirAll(show, 0o755); err != nil {
		t.Fatal(err)
	}
	f.nfoEpisode(show, "Show - S01E01.mkv", "a", 1, 1)
	writeFile(t, filepath.Join(show, "tvshow.nfo"), `<tvshow><title>My Cut</title><uniqueid type="tmdb">1</uniqueid><lockdata>true</lockdata></tvshow>`)
	f.tmdb.result = &Result{Provider: "tmdb", ProviderIDs: map[string]string{"tmdb": "1"}, Title: "Provider Title"}
	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	if f.tmdb.matches != 0 || f.al.matches != 0 {
		t.Fatalf("a provider was asked: tmdb %d anilist %d", f.tmdb.matches, f.al.matches)
	}
	if ser, _ := f.st.GetItem(f.ctx, "series"); ser.Title != "My Cut" {
		t.Fatalf("title %q", ser.Title)
	}
}

func TestFirstFileAnyCaseAndExtension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "FANART.WebP"), "x")
	if got := firstFile(dir, withExts("poster")...); got != "" {
		t.Fatalf("no poster here: %q", got)
	}
	if got := firstFile(dir, withExts("fanart")...); got == "" || filepath.Base(got[len("file://"):len(got)-len("#")-10]) != "FANART.WebP" {
		t.Fatalf("fanart: %q", got)
	}
}

// A special's .nfo places it in story order (KonoSuba's film: after season 2, before
// season 3 episode 1); Kodi's displayseason/displayepisode work too.
func TestNFOSpecialPlacement(t *testing.T) {
	f := newFixture(t, false)
	show := filepath.Join(f.lib.Root, "KonoSuba")
	if err := os.MkdirAll(filepath.Join(show, "Specials"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.nfoEpisode(filepath.Join(show, "Specials"), "KonoSuba - S00E05 - Movie.mkv", "film", 0, 5)
	writeFile(t, filepath.Join(show, "Specials", "KonoSuba - S00E05 - Movie.nfo"), `<episodedetails><title>Movie: Legend of Crimson</title><season>0</season><episode>5</episode>
		<airsafter_season>2</airsafter_season><airsbefore_season>3</airsbefore_season><airsbefore_episode>1</airsbefore_episode></episodedetails>`)
	f.nfoEpisode(filepath.Join(show, "Specials"), "KonoSuba - S00E06 - OVA.mkv", "ova", 0, 6)
	writeFile(t, filepath.Join(show, "Specials", "KonoSuba - S00E06 - OVA.nfo"), `<episodedetails><season>0</season><episode>6</episode><displayseason>1</displayseason><displayepisode>4096</displayepisode></episodedetails>`)
	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	film, _ := f.st.GetItem(f.ctx, "film")
	if film.AirsBeforeSeason == nil || *film.AirsBeforeSeason != 3 || film.AirsBeforeEpisode == nil || *film.AirsBeforeEpisode != 1 || film.AirsAfterSeason != nil {
		t.Fatalf("film placement: %v %v %v", film.AirsBeforeSeason, film.AirsBeforeEpisode, film.AirsAfterSeason)
	}
	ova, _ := f.st.GetItem(f.ctx, "ova")
	if ova.AirsAfterSeason == nil || *ova.AirsAfterSeason != 1 || ova.AirsBeforeSeason != nil {
		t.Fatalf("ova placement: %v %v", ova.AirsBeforeSeason, ova.AirsAfterSeason)
	}
}
