package metadata

import (
	"context"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Specials Sonarr named in TheTVDB's numbering get no date from TMDB (its specials of the
// same number are other videos), which left them nowhere to go in release order. They get
// Sonarr's date: only real episodes with none, never replacing one, never placeholders;
// Sonarr is asked only when something is missing, and not again the same day.
func TestFillAirDatesFromSonarr(t *testing.T) {
	f := newFixture(t, true)
	f.series.ProviderIDs = map[string]string{"tvdb": "254653"}
	f.upsert(f.series)
	s0, s1 := 0, 1
	f.upsert(&model.Item{ID: "season0", GUID: "series:season:0", LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, IndexAired: &s0})
	f.upsert(&model.Item{ID: "season1", GUID: "series:season:1", LibraryID: f.lib.ID, Type: model.TypeSeason, ParentID: &f.series.ID, IndexAired: &s1})
	ep := func(id string, season, n int, date string, virtual bool) {
		sid := "season0"
		if season == 1 {
			sid = "season1"
		}
		e := &model.Item{ID: id, GUID: "g-" + id, LibraryID: f.lib.ID, Type: model.TypeEpisode, ParentID: &sid, SeasonAired: &season, IndexAired: &n, IsVirtual: virtual}
		f.upsert(e)
		if date != "" {
			if err := f.st.SetAirDateIfUnset(f.ctx, id, date); err != nil {
				t.Fatal(err)
			}
		}
	}
	ep("ova1", 0, 1, "", false)
	ep("ova2", 0, 2, "", false)
	ep("ep1", 1, 1, "2012-01-06", false)
	ep("missing", 0, 6, "", true) // a placeholder: not filled
	calls := 0
	sonarr := map[[2]int]string{{0, 1}: "2012-04-11", {0, 2}: "2012-07-25", {1, 1}: "1999-01-01", {0, 6}: "2012-07-25"}
	f.svc.UseEpisodeAirDates(func(_ context.Context, tvdb int) (map[[2]int]string, error) {
		calls++
		if tvdb != 254653 {
			t.Errorf("asked for tvdb %d", tvdb)
		}
		return sonarr, nil
	})
	date := func(id string) string {
		it, err := f.st.GetItem(f.ctx, id)
		if err != nil || it == nil {
			t.Fatal(it, err)
		}
		if len(it.Premiered) >= 10 {
			return it.Premiered[:10]
		}
		return it.Premiered
	}
	series, _ := f.st.GetItem(f.ctx, "series")
	f.svc.fillAirDates(f.ctx, series)
	if date("ova1") != "2012-04-11" || date("ova2") != "2012-07-25" {
		t.Errorf("specials: %q %q", date("ova1"), date("ova2"))
	}
	if date("ep1") != "2012-01-06" {
		t.Errorf("a date the provider gave was replaced: %q", date("ep1"))
	}
	if date("missing") != "" {
		t.Errorf("a placeholder was dated: %q", date("missing"))
	}
	// Nothing missing any more: Sonarr is not asked again; nor the same day for a series
	// whose undated episode Sonarr could not date.
	f.svc.fillAirDates(f.ctx, series)
	if calls != 1 {
		t.Fatalf("Sonarr asked %d times, want 1", calls)
	}
	ep("ova3", 0, 3, "", false) // Sonarr has no date for it
	f.svc.fillAirDates(f.ctx, series)
	if calls != 1 {
		t.Fatalf("asked again the same day: %d calls", calls)
	}
}
