package metadata

import (
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// KonoSuba's two OVAs are TheTVDB's specials 3 and 4 (so Sonarr's file names) and TMDB's
// 1 and 2, titled one letter apart ("Gods Blessings On This Wonderful Choker!" against
// "God's Blessing on This Wonderful Choker!"). The titles did not match, so the files
// kept their name only and TMDB's 1 and 2 were listed as missing specials beside them.
// They match now: the files get TMDB's data and no placeholder repeats them.
func TestSpecialTitlesMatchAcrossPluralAndApostrophe(t *testing.T) {
	f := newFixture(t, true)
	f.realSeason(1, 1)
	s0 := 0
	f.upsert(&model.Item{ID: "season0", GUID: f.series.GUID + ":season:0", LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, Title: "Specials", IndexAired: &s0})
	sp := "season0"
	for _, x := range []struct {
		id, name string
		n        int
	}{{"e3", "KonoSuba - S00E03 - Gods Blessings On This Wonderful Choker!.mkv", 3}, {"e4", "KonoSuba - S00E04 - Gods Blessings on These Wonderful Works of Art!.mkv", 4}} {
		n := x.n
		f.upsert(&model.Item{ID: x.id, GUID: f.series.GUID + ":s00e000" + x.id[1:], LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &sp, Title: "x", IndexAired: &n, SeasonAired: &s0})
		if err := f.st.ReplaceSource(f.ctx, &model.MediaSource{ID: "src-" + x.id, ItemID: x.id, Parts: []model.MediaPart{{ID: "p-" + x.id, Path: f.lib.Root + "/K/Specials/" + x.name}}}, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	f.tmdb.result = &Result{Provider: "tmdb", ID: "65844", Title: "KonoSuba", ProviderIDs: map[string]string{"tmdb": "65844"},
		Seasons: []SeasonInfo{{Number: 0, EpisodeCount: 2}, {Number: 1, EpisodeCount: 1}}}
	f.tmdb.seasons[0] = &Season{Number: 0, Episodes: []EpisodeResult{
		{Season: 0, Episode: 1, Title: "God's Blessing on This Wonderful Choker!", AirDate: "2016-06-24"},
		{Season: 0, Episode: 2, Title: "God's Blessing on These Wonderful Works of Art!", AirDate: "2017-07-24"},
	}}
	f.tmdb.seasons[1] = episodes(1, 1, 0, "E")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	got := f.layout()[0]
	if r, v := countVirtual(got); r != 2 || v != 0 {
		t.Fatalf("specials: %d real, %d placeholders (%v); want the two files and no placeholder", r, v, got)
	}
	if got[3].title != "God's Blessing on This Wonderful Choker!" || got[4].title != "God's Blessing on These Wonderful Works of Art!" {
		t.Fatalf("titles: %q %q", got[3].title, got[4].title)
	}
}

// The plural/apostrophe allowance does not make different titles equal: short titles
// (one or two words, where a plural can be another episode) must match as before, other
// words, numbers or word order still differ, and an exact title beats a folded one.
func TestSameTitleStaysStrict(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"Gods Blessings On This Wonderful Choker!", "God's Blessing on This Wonderful Choker!", true},
		{"The Party of Heroes", "The Party Of Heroes!", true},
		{"Steamy Grayfia", "Rossweisse's True Teaching Story!", false},
		{"Red Stream Explosion!", "Watch Out for Impostors!", false},
		{"Partner", "Partners", false},                            // one word: could be two episodes
		{"The Boy", "The Boys", false},                            // two words: same
		{"Its Fate", "It's Fate", true},                           // equal once compacted (as before)
		{"Part 1 of the Journey", "Part 2 of the Journey", false}, // numbers are kept
		{"A Cat for the Witch", "A Witch for the Cat", false},     // word order is kept
		{"Return of the Kings", "Return of the King", true},
		{"Return of the Kings", "Return of the Queen", false},
		{"Ms Marvel Saves the Day", "Mr Marvel Saves the Day", false},
		{"Episode", "Episodes 2", false},
		{"", "", false},
	} {
		if got := sameTitle(c.a, c.b); got != c.want {
			t.Errorf("sameTitle(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// Where a season has both, the file's exact title wins over a folded match.
func TestExactTitleBeatsFoldedTitle(t *testing.T) {
	eps := []EpisodeResult{
		{Episode: 3, Title: "The Gift of the Gods"},
		{Episode: 4, Title: "The Gifts of the Gods"},
	}
	if k := matchEpisodeTitle(eps, "The Gifts of the Gods", 3); k != 1 {
		t.Fatalf("matched %d, want the exact title (index 1)", k)
	}
	if k := matchEpisodeTitle(eps, "The Gift of the Gods", 4); k != 0 {
		t.Fatalf("matched %d, want the exact title (index 0)", k)
	}
}
