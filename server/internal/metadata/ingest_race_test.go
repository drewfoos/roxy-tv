package metadata

import (
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Specials copied in together: the scanner makes each episode before it has probed the
// file, and the previous file's ingest starts a metadata pass. High School DxD's S00E17
// ("Rossweisse's True Teaching Story!") was still unprobed then, so it had no name to
// check TMDB's special 17 (Steamy Grayfia) against, took that title and date by number,
// and was marked done, so its own ingest changed nothing. A special with no file yet is
// left for that later pass, which keeps its own title.
func TestSpecialStillBeingIngestedWaitsForItsFile(t *testing.T) {
	f := newFixture(t, false)
	f.realSeason(1, 1)
	s0 := 0
	f.upsert(&model.Item{ID: "season0", GUID: f.series.GUID + ":season:0", LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, Title: "Specials", IndexAired: &s0})
	sp := "season0"
	special := func(id string, n int) {
		f.upsert(&model.Item{ID: id, GUID: f.series.GUID + ":sp:" + id, LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &sp, Title: "x", IndexAired: &n, SeasonAired: &s0})
	}
	file := func(id, name string) {
		if err := f.st.ReplaceSource(f.ctx, &model.MediaSource{ID: "src-" + id, ItemID: id, Parts: []model.MediaPart{{ID: "p-" + id, Path: f.lib.Root + "/Show/Specials/" + name}}}, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	special("e16", 16)
	file("e16", "Show S00E16 - Steamy Grayfia.mkv")
	special("e17", 17) // made, not probed yet
	f.tmdb.result = &Result{Provider: "tmdb", ID: "1", Title: "Show", ProviderIDs: map[string]string{"tmdb": "1"},
		Seasons: []SeasonInfo{{Number: 0, EpisodeCount: 18}, {Number: 1, EpisodeCount: 1}}}
	f.tmdb.seasons[0] = &Season{Number: 0, Episodes: []EpisodeResult{
		{Season: 0, Episode: 16, Title: "Don't Revive the Phoenix", AirDate: "2015-12-09"},
		{Season: 0, Episode: 17, Title: "Steamy Grayfia", AirDate: "2015-12-25"},
		{Season: 0, Episode: 18, Title: "Rossweisse-sensei", AirDate: "2016-01-27"},
	}}
	f.tmdb.seasons[1] = episodes(1, 1, 0, "E")
	if err := f.run(false); err != nil { // the pass e16's ingest started
		t.Fatal(err)
	}
	e17, _ := f.st.GetItem(f.ctx, "e17")
	if e17.Title == "Steamy Grayfia" || e17.MetadataRefreshedAt != nil {
		t.Fatalf("unprobed special: title %q refreshed %v; want it left for its own pass", e17.Title, e17.MetadataRefreshedAt)
	}
	file("e17", "Show S00E17 - Rossweisse's True Teaching Story!.mkv")
	if err := f.run(false); err != nil { // e17's own ingest
		t.Fatal(err)
	}
	e16, _ := f.st.GetItem(f.ctx, "e16")
	e17, _ = f.st.GetItem(f.ctx, "e17")
	if e16.Title != "Steamy Grayfia" || e17.Title != "Rossweisse's True Teaching Story!" || e17.Premiered != "" {
		t.Fatalf("e16 %q, e17 %q (%q); want Steamy Grayfia and its own title, no TMDB date", e16.Title, e17.Title, e17.Premiered)
	}
}
