package metadata

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// weekly makes a provider season of count episodes titled prefix+number, a week apart
// from first.
func weekly(season, count int, first, prefix string) *Season {
	d, _ := time.Parse("2006-01-02", first)
	se := &Season{Number: season}
	for i := 1; i <= count; i++ {
		se.Episodes = append(se.Episodes, EpisodeResult{Season: season, Episode: i, Title: fmt.Sprintf("%s%d", prefix, i), AirDate: d.AddDate(0, 0, 7*(i-1)).Format("2006-01-02")})
	}
	return se
}

// High School DxD Hero: TMDB counts the recap "Episode 0" as Season 4's E1, TheTVDB (and
// so Sonarr's file names) starts at the first real episode. Every file showed the title,
// synopsis and still of the episode before it, and the finale was listed as missing. By
// Sonarr's air dates the season lines up one later: E1 is TMDB's E2, E12 TMDB's E13, no
// placeholder for a 13th. A season the two agree on stays as it is, and a refresh while
// Sonarr is unreachable keeps the offset.
func TestEpisodeNumbersFollowSonarrDates(t *testing.T) {
	f := newFixture(t, false)
	f.series.ProviderIDs = map[string]string{"tvdb": "254653"}
	f.upsert(f.series)
	f.realSeason(1, 3)
	f.realSeason(2, 12)
	f.tmdb.result = &Result{Provider: "tmdb", ID: "45950", Title: "Show", ProviderIDs: map[string]string{"tmdb": "45950"},
		Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 3}, {Number: 2, EpisodeCount: 13}}}
	f.tmdb.seasons[1] = weekly(1, 3, "2012-01-06", "A")
	f.tmdb.seasons[2] = weekly(2, 13, "2018-04-10", "H") // H1 is "Episode 0"
	sonarr := map[[2]int]string{}
	for i, e := range weekly(1, 3, "2012-01-06", "").Episodes {
		sonarr[[2]int{1, i + 1}] = e.AirDate
	}
	for i, e := range weekly(2, 12, "2018-04-17", "").Episodes {
		sonarr[[2]int{2, i + 1}] = e.AirDate
	}
	var sonarrErr error
	f.svc.UseEpisodeAirDates(func(context.Context, int) (map[[2]int]string, error) {
		if sonarrErr != nil {
			return nil, sonarrErr
		}
		return sonarr, nil
	})
	check := func(when string) {
		t.Helper()
		got := f.layout()
		if got[1][1].title != "A1" || got[1][3].title != "A3" {
			t.Errorf("%s: S1 (agrees with Sonarr) changed: %q %q", when, got[1][1].title, got[1][3].title)
		}
		if got[2][1].title != "H2" || got[2][12].title != "H13" {
			t.Errorf("%s: S2E1=%q S2E12=%q, want H2 and H13", when, got[2][1].title, got[2][12].title)
		}
		if r, v := countVirtual(got[2]); r != 12 || v != 0 {
			t.Errorf("%s: S2 has %d real, %d placeholders (%v); want 12 and none", when, r, v, got[2])
		}
	}
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	check("first refresh")
	sonarrErr = errors.New("connection refused")
	if err := f.run(true); err != nil {
		t.Fatal(err)
	}
	check("refresh with Sonarr down")
}

// Only exact, consistent date matches move a season: dates a day apart throughout (a
// time zone) or a single coincidence leave it alone; a shift the other way is found too.
func TestDateShiftsNeedsAConsistentMatch(t *testing.T) {
	r := &Result{Provider: "tmdb"}
	prov := map[int]*Season{1: weekly(1, 12, "2020-01-01", "T")}
	fetch := func(_ *Result, n int) *Season { return prov[n] }
	ens := map[int][]int{1: {1, 2, 3, 4, 5, 6}}
	dates := func(first string, from int) map[[2]int]string {
		out := map[[2]int]string{}
		for i, e := range weekly(1, 6, first, "").Episodes {
			out[[2]int{1, from + i}] = e.AirDate
		}
		return out
	}
	if got := dateShifts(r, fetch, ens, dates("2020-01-02", 1)); len(got) != 0 {
		t.Errorf("a day apart throughout: shift %v, want none", got)
	}
	if got := dateShifts(r, fetch, ens, dates("2020-01-01", 1)); len(got) != 0 {
		t.Errorf("agreeing: shift %v, want none", got)
	}
	one := map[[2]int]string{{1, 1}: "2020-01-08", {1, 2}: "1999-01-01", {1, 3}: "1999-01-08"}
	if got := dateShifts(r, fetch, ens, one); len(got) != 0 {
		t.Errorf("one coincidence: shift %v, want none", got)
	}
	if got := dateShifts(r, fetch, ens, dates("2020-01-08", 1)); got[1] != 1 {
		t.Errorf("provider one ahead: shift %v, want 1", got)
	}
	if got := dateShifts(r, fetch, map[int][]int{1: {2, 3, 4, 5, 6}}, dates("2020-01-01", 2)); got[1] != -1 {
		t.Errorf("files one ahead: shift %v, want -1", got)
	}
}

// The edges of the date rule. A season moves only when one offset is clearly what the
// dates say; anything ambiguous leaves it as it is.
func TestDateShiftsEdges(t *testing.T) {
	r := &Result{Provider: "tmdb"}
	week := func(first string, n int) string {
		d, _ := time.Parse("2006-01-02", first)
		return d.AddDate(0, 0, 7*(n-1)).Format("2006-01-02")
	}
	season := func(eps ...EpisodeResult) func(*Result, int) *Season {
		return func(_ *Result, n int) *Season { return &Season{Number: 1, Episodes: eps} }
	}
	ep := func(n int, date string) EpisodeResult { return EpisodeResult{Season: 1, Episode: n, AirDate: date} }
	cases := []struct {
		name   string
		fetch  func(*Result, int) *Season
		ens    []int
		sonarr map[[2]int]string
		want   int
	}{
		{
			// A double premiere (E1 and E2 the same day) on both sides: unshifted fits all,
			// +1 fits only the shared day.
			name:   "double premiere",
			fetch:  season(ep(1, "2020-01-01"), ep(2, "2020-01-01"), ep(3, "2020-01-08"), ep(4, "2020-01-15")),
			ens:    []int{1, 2, 3, 4},
			sonarr: map[[2]int]string{{1, 1}: "2020-01-01", {1, 2}: "2020-01-01", {1, 3}: "2020-01-08", {1, 4}: "2020-01-15"},
			want:   0,
		},
		{
			// The provider lists the whole season on one day (a streaming drop) and Sonarr
			// too: every offset fits every file, so no offset is clearly right.
			name:   "season dropped in one day",
			fetch:  season(ep(1, "2020-01-01"), ep(2, "2020-01-01"), ep(3, "2020-01-01"), ep(4, "2020-01-01")),
			ens:    []int{1, 2, 3},
			sonarr: map[[2]int]string{{1, 1}: "2020-01-01", {1, 2}: "2020-01-01", {1, 3}: "2020-01-01"},
			want:   0,
		},
		{
			// Missing files in the middle (2, 3 not in the library): the offset is still
			// found from the files there are.
			name:   "gaps in the files",
			fetch:  season(ep(1, week("2018-04-10", 1)), ep(2, week("2018-04-10", 2)), ep(3, week("2018-04-10", 3)), ep(4, week("2018-04-10", 4)), ep(5, week("2018-04-10", 5)), ep(6, week("2018-04-10", 6))),
			ens:    []int{1, 4, 5},
			sonarr: map[[2]int]string{{1, 1}: week("2018-04-10", 2), {1, 4}: week("2018-04-10", 5), {1, 5}: week("2018-04-10", 6)},
			want:   1,
		},
		{
			// One file in the season: a single agreement could be chance.
			name:   "partial season, one file",
			fetch:  season(ep(1, "2020-01-01"), ep(2, "2020-01-08")),
			ens:    []int{1},
			sonarr: map[[2]int]string{{1, 1}: "2020-01-08"},
			want:   0,
		},
		{
			// Sonarr has dates for few of the files: two fits out of six dated is not most.
			name:   "most files disagree",
			fetch:  season(ep(1, week("2020-01-01", 1)), ep(2, week("2020-01-01", 2)), ep(3, week("2020-01-01", 3)), ep(4, week("2020-01-01", 4)), ep(5, week("2020-01-01", 5)), ep(6, week("2020-01-01", 6)), ep(7, week("2020-01-01", 7))),
			ens:    []int{1, 2, 3, 4, 5, 6},
			sonarr: map[[2]int]string{{1, 1}: week("2020-01-01", 2), {1, 2}: week("2020-01-01", 3), {1, 3}: "1999-01-01", {1, 4}: "1999-02-01", {1, 5}: "1999-03-01", {1, 6}: "1999-04-01"},
			want:   0,
		},
		{
			// Episodes two a week, alternating days: -1 and +1 each fit some; neither wins.
			name:   "two offsets fit equally",
			fetch:  season(ep(1, "2020-01-01"), ep(2, "2020-01-02"), ep(3, "2020-01-01"), ep(4, "2020-01-02"), ep(5, "2020-01-01")),
			ens:    []int{2, 3, 4},
			sonarr: map[[2]int]string{{1, 2}: "2020-01-01", {1, 3}: "2020-01-02", {1, 4}: "2020-01-01"},
			want:   0,
		},
		{
			// No Sonarr dates for the season (or no provider dates): nothing to go on.
			name:   "no dates",
			fetch:  season(ep(1, ""), ep(2, ""), ep(3, "")),
			ens:    []int{1, 2},
			sonarr: map[[2]int]string{{1, 1}: "2020-01-01", {1, 2}: "2020-01-08"},
			want:   0,
		},
	}
	for _, c := range cases {
		got := dateShifts(r, c.fetch, map[int][]int{1: c.ens}, c.sonarr)[1]
		if got != c.want {
			t.Errorf("%s: shift %d, want %d", c.name, got, c.want)
		}
	}
}

// dxdHero sets up a series whose Season 2 runs one episode behind TMDB's (DxD Hero's
// "Episode 0"), with Sonarr's dates for it.
func dxdHero(t *testing.T, seriesTVDB bool) (*svcFixture, map[[2]int]string) {
	f := newFixture(t, false)
	if seriesTVDB {
		f.series.ProviderIDs = map[string]string{"tvdb": "254653"}
		f.upsert(f.series)
	}
	f.realSeason(1, 3)
	f.realSeason(2, 12)
	f.tmdb.result = &Result{Provider: "tmdb", ID: "45950", Title: "Show", ProviderIDs: map[string]string{"tmdb": "45950", "tvdb": "254653"},
		Seasons: []SeasonInfo{{Number: 1, EpisodeCount: 3}, {Number: 2, EpisodeCount: 13}}}
	f.tmdb.seasons[1] = weekly(1, 3, "2012-01-06", "A")
	f.tmdb.seasons[2] = weekly(2, 13, "2018-04-10", "H")
	sonarr := map[[2]int]string{}
	for i, e := range weekly(1, 3, "2012-01-06", "").Episodes {
		sonarr[[2]int{1, i + 1}] = e.AirDate
	}
	for i, e := range weekly(2, 12, "2018-04-17", "").Episodes {
		sonarr[[2]int{2, i + 1}] = e.AirDate
	}
	return f, sonarr
}

// A restart while Sonarr is down: no earlier answer in memory. The refresh is postponed
// (nothing written: no titles in TMDB's unshifted numbering, the series not marked
// refreshed, so the retry redoes it all), and once Sonarr answers it lines up.
func TestRefreshWaitsForSonarrAfterRestart(t *testing.T) {
	f, sonarr := dxdHero(t, true)
	down := true
	f.svc.UseEpisodeAirDates(func(context.Context, int) (map[[2]int]string, error) {
		if down {
			return nil, errors.New("connection refused")
		}
		return sonarr, nil
	})
	if err := f.run(false); err == nil {
		t.Fatal("refresh with Sonarr down and nothing remembered: no error, want it postponed")
	}
	got := f.layout()
	if got[2][1].title != "Episode 1" || got[2][12].title != "Episode 12" {
		t.Fatalf("episodes written while Sonarr was down: S2E1=%q S2E12=%q", got[2][1].title, got[2][12].title)
	}
	if series, _ := f.st.GetItem(f.ctx, f.series.ID); series.MetadataRefreshedAt != nil {
		t.Fatal("series marked refreshed: the retry would only look at new episodes")
	}
	down = false
	f.svc.failed = map[string]time.Time{} // the retry's wait, over
	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	if got := f.layout(); got[2][1].title != "H2" || got[2][12].title != "H13" {
		t.Fatalf("after Sonarr is back: S2E1=%q S2E12=%q, want H2 and H13", got[2][1].title, got[2][12].title)
	}
}

// A series matched for the first time has no TheTVDB id saved yet: the id the match
// brings is used, so the first refresh already lines the season up.
func TestFirstMatchUsesTheMatchedTVDBID(t *testing.T) {
	f, sonarr := dxdHero(t, false)
	asked := 0
	f.svc.UseEpisodeAirDates(func(_ context.Context, tvdb int) (map[[2]int]string, error) {
		asked = tvdb
		return sonarr, nil
	})
	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	if got := f.layout(); asked != 254653 || got[2][1].title != "H2" {
		t.Fatalf("Sonarr asked for %d, S2E1=%q; want 254653 and H2", asked, got[2][1].title)
	}
}

// No Sonarr, or a series Sonarr does not have: refreshes go on as before.
func TestNoSonarrSeriesRefreshesAsBefore(t *testing.T) {
	f, _ := dxdHero(t, true)
	f.svc.UseEpisodeAirDates(func(context.Context, int) (map[[2]int]string, error) { return nil, nil })
	if err := f.run(false); err != nil {
		t.Fatal(err)
	}
	if got := f.layout(); got[2][1].title != "H1" {
		t.Fatalf("S2E1=%q, want TMDB's own numbering (H1)", got[2][1].title)
	}
}
