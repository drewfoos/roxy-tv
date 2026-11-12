package discover

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/roxy-media/roxy/server/internal/store"
)

// ClaimSetting: of many callers at once, exactly one wins.
func TestClaimSettingOneWinner(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := st.ClaimSetting(ctx, "storyfilm:1", "x"); err == nil && ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d winners, want 1", wins.Load())
	}
}

// pickFilm: TMDB's film for an AniList story film only when the search says clearly
// which; a wrong film would be downloaded with nobody asking.
func TestPickFilm(t *testing.T) {
	anim := []int{16}
	e := func(id int, title, date string, genres []int) tmdbListEntry {
		return tmdbListEntry{ID: id, Title: title, ReleaseDate: date, GenreIDs: genres}
	}
	const dg = "Rascal Does Not Dream of a Dreaming Girl"
	cases := []struct {
		name  string
		title string
		year  int
		res   []tmdbListEntry
		want  int
	}{
		{"the one match", dg, 2019, []tmdbListEntry{e(1, dg, "2019-06-15", anim), e(2, "Dreaming", "2019-01-01", anim)}, 1},
		{"a year off is still it", dg, 2020, []tmdbListEntry{e(1, dg, "2019-06-15", anim)}, 1},
		{"no close title", "Something Else Entirely", 2019, []tmdbListEntry{e(1, dg, "2019-06-15", anim)}, 0},
		{"no film that year", dg, 2012, []tmdbListEntry{e(1, dg, "2019-06-15", anim)}, 0},
		{"live-action remake of the same name and year is not it", "Your Name.", 2016,
			[]tmdbListEntry{e(1, "Your Name.", "2016-08-26", []int{16, 10749}), e(2, "Your Name.", "2016-11-01", []int{10749, 18})}, 1},
		{"only a live-action one: none", "Your Name.", 2016, []tmdbListEntry{e(2, "Your Name.", "2016-11-01", []int{10749})}, 0},
		{"the exact year beats a neighbour's", "Ghost in the Shell", 1995,
			[]tmdbListEntry{e(1, "Ghost in the Shell", "1996-03-01", anim), e(2, "Ghost in the Shell", "1995-11-18", anim)}, 2},
		{"two animated films of one name and year: ambiguous", "Kizumonogatari", 2016,
			[]tmdbListEntry{e(1, "Kizumonogatari", "2016-01-08", anim), e(2, "Kizumonogatari", "2016-08-19", anim)}, 0},
		{"a compilation titled alike but not the same", "Attack on Titan: The Final Season", 2023,
			[]tmdbListEntry{e(1, "Attack on Titan: The Final Season", "2023-03-04", anim), e(2, "Attack on Titan: The Final Season - Compilation", "2023-11-04", anim)}, 1},
		{"the same result twice counts once", dg, 2019, []tmdbListEntry{e(1, dg, "2019-06-15", anim), e(1, dg, "2019-06-15", anim)}, 1},
		{"no genres listed: not held against it", dg, 2019, []tmdbListEntry{e(1, dg, "2019-06-15", nil)}, 1},
	}
	for _, c := range cases {
		if got, why := pickFilm(c.title, c.year, c.res); got != c.want {
			t.Errorf("%s: got %d (%s), want %d", c.name, got, why, c.want)
		}
	}
}
