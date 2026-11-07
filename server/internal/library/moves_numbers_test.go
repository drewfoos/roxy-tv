package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Found by an audit: an episode file deleted for good whose stem ("Episode 01") repeats in another
// season of the same show: rule 2 (same stem, same top title, duration within 2s) carried
// S1E01's history to S2E01. An episode now needs the same season and episode number.
func TestDeletedEpisodeHistoryStaysOffOtherSeasons(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindSeries)
	s1 := filepath.Join(root, "Show", "Season 01", "Episode 01.mkv")
	s2 := filepath.Join(root, "Show", "Season 02", "Episode 01.mkv")
	durations["Episode 01.mkv"] = 1_420_000
	t.Cleanup(func() { delete(durations, "Episode 01.mkv") })
	writeSized(t, s1, 800)
	writeSized(t, s2, 900)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	e1, e2 := itemAt(t, st, s1), itemAt(t, st, s2)
	if e1.GUID == e2.GUID {
		t.Fatalf("same guid")
	}
	if err := st.MarkPlayed(ctx, scope, e1.GUID, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s1); err != nil { // deleted for good, not moved
		t.Fatal(err)
	}
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if d, _ := st.GetUserItemData(ctx, scope, e2.GUID); d != nil && d.Played {
		t.Fatalf("S2E01 (never watched) is now watched: S1E01's history was carried to it: %+v", d)
	}
}

// Found by an audit: an episode replaced, inside the same series, by a release whose name gives it a
// different identity (absolute "- 01" -> "S01E01"): rule 3 skips departures from the same
// series, so even after the provider match the history never followed. Rule 2b carries it to
// the same series' episode with the same numbers.
func TestReplacementInSameSeriesCarriesHistory(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindSeries)
	old := filepath.Join(root, "Show", "Season 01", "Show - 01.mkv")
	writeSized(t, old, 800)
	writeSized(t, filepath.Join(root, "Show", "Season 01", "Show - 02.mkv"), 801)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	ep := itemAt(t, st, old)
	t.Logf("old guid %s", ep.GUID)
	if err := st.MarkPlayed(ctx, scope, ep.GUID, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	nw := filepath.Join(root, "Show", "Season 01", "Show - S01E01 - Pilot [1080p].mkv")
	writeSized(t, nw, 5000)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	ne := itemAt(t, st, nw)
	t.Logf("new guid %s", ne.GUID)
	series := mustParent(t, st, mustParent(t, st, ne))
	if err := st.SetProviderIDs(ctx, series.ID, map[string]string{"tvdb": "999"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SettleByProvider(ctx, series.ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := st.GetUserItemData(ctx, scope, ne.GUID); d == nil || !d.Played {
		t.Fatalf("history of S01E01 did not follow its replacement in the same series: %+v", d)
	}
}
