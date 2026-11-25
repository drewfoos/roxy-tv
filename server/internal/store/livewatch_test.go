package store

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"
)

// With ROXY_LIVE_COPY set to a copy of a real database: prints, for every series with
// specials, the order its episodes are watched in where that differs from the seasons'
// numbers with the specials last (what it was), and how long the read takes. Skipped
// otherwise.
func TestLiveWatchOrder(t *testing.T) {
	path := os.Getenv("ROXY_LIVE_COPY")
	if path == "" {
		t.Skip("ROXY_LIVE_COPY not set")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	rows, err := s.db.QueryContext(ctx, `SELECT id, title FROM items WHERE type = 'series' ORDER BY title`)
	if err != nil {
		t.Fatal(err)
	}
	type show struct{ id, title string }
	var shows []show
	for rows.Next() {
		var sh show
		if err := rows.Scan(&sh.id, &sh.title); err != nil {
			t.Fatal(err)
		}
		shows = append(shows, sh)
	}
	rows.Close()
	var slowest time.Duration
	slowestOf, changed := "", 0
	for _, sh := range shows {
		start := time.Now()
		eps, err := s.watchEpisodes(ctx, "", sh.id)
		if err != nil {
			t.Fatal(err)
		}
		keys := watchKeys(eps)
		if d := time.Since(start); d > slowest {
			slowest, slowestOf = d, fmt.Sprintf("%s (%d episodes)", sh.title, len(eps))
		}
		var real []watchEpisode
		for _, e := range eps {
			if !e.Virtual {
				real = append(real, e)
			}
		}
		label := func(e watchEpisode) string {
			if e.Season == 0 {
				return fmt.Sprintf("SP%d %q", e.Number, e.Title)
			}
			return fmt.Sprintf("S%dE%d", e.Season, e.Number)
		}
		sort.SliceStable(real, func(i, j int) bool { return keys[real[i].ID].Order < keys[real[j].ID].Order })
		// Between seasons: every special that is not at the foot, with what it follows.
		var between []string
		for i, e := range real {
			if e.Season == 0 && !keys[e.ID].Extra {
				prev := "the start"
				if i > 0 {
					prev = label(real[i-1])
				}
				between = append(between, fmt.Sprintf("after %s: %s (%s, %d min)", prev, label(e), e.Date, e.RuntimeMs/60000))
			}
		}
		if len(between) > 0 {
			changed++
			t.Logf("%s", sh.title)
			for _, b := range between {
				t.Logf("    %s", b)
			}
		}
	}
	t.Logf("%d series, %d with specials between seasons; slowest read: %s in %s", len(shows), changed, slowestOf, slowest)

	// Continue Watching, for every profile: where it now offers a special.
	prows, err := s.db.QueryContext(ctx, `SELECT id, name FROM profiles`)
	if err != nil {
		t.Fatal(err)
	}
	type prof struct{ id, name string }
	var profs []prof
	for prows.Next() {
		var p prof
		if err := prows.Scan(&p.id, &p.name); err != nil {
			t.Fatal(err)
		}
		profs = append(profs, p)
	}
	prows.Close()
	for _, p := range profs {
		start := time.Now()
		items, err := s.ContinueItems(ctx, p.id, 40)
		if err != nil {
			t.Fatal(err)
		}
		took := time.Since(start)
		t.Logf("profile %d of %d: %d on Continue Watching (read in %s)", indexOf(profs, p)+1, len(profs), len(items), took)
		for i, it := range items {
			season := -1
			if it.SeasonAired != nil {
				season = *it.SeasonAired
			}
			if it.Type == "episode" && season == 0 {
				var series string
				_ = s.db.QueryRowContext(ctx, `SELECT sr.title FROM items e JOIN items se ON se.id = e.parent_id JOIN items sr ON sr.id = se.parent_id WHERE e.id = ?`, it.ID).Scan(&series)
				t.Logf("    #%d a special: %s: %q", i+1, series, it.Title)
			}
		}
	}
}

func indexOf[T comparable](list []T, v T) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}
