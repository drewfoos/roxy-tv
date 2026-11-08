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
