package discover

import (
	"strings"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/arr"
	"github.com/roxy-media/roxy/server/internal/store"
)

func newTestService() *Service {
	return &Service{dl: tracker{status: map[string]*Download{}, progress: map[string]progressMark{}, kick: make(chan struct{}, 1), refresh: map[string]time.Time{}, stalled: map[string]time.Time{}}}
}

func tp(t time.Time) *time.Time { return &t }

func ep(id, season, n int, hasFile bool, aired *time.Time, searched *time.Time) arr.Episode {
	return arr.Episode{ID: id, SeasonNumber: season, EpisodeNumber: n, HasFile: hasFile, Monitored: true, AirDateUTC: aired, LastSearchTime: searched}
}

func TestSeriesStatus(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	past, future := tp(now.Add(-30*24*time.Hour)), tp(now.Add(7*24*time.Hour))
	searched := tp(now.Add(-time.Hour))
	r := &store.Request{Seasons: []int{1}}

	t.Run("downloading pack with progress and grouping", func(t *testing.T) {
		s := newTestService()
		eps := []arr.Episode{ep(1, 1, 1, true, past, nil), ep(2, 1, 2, false, past, nil), ep(3, 1, 3, false, past, nil), ep(4, 1, 4, false, future, nil)}
		q := []arr.QueueItem{
			{ID: 10, DownloadID: "abc", EpisodeID: 2, Status: "downloading", TrackedDownloadState: "downloading", Size: 1000, SizeLeft: 250, TimeLeft: "00:05:00"},
			{ID: 11, DownloadID: "abc", EpisodeID: 3, Status: "downloading", TrackedDownloadState: "downloading", Size: 1000, SizeLeft: 250, TimeLeft: "00:05:00"},
		}
		d := s.seriesStatus(r, eps, q, nil, now)
		if d.State != DLDownloading || d.Have != 1 || d.Wanted != 3 || d.Upcoming != 1 {
			t.Fatalf("got %+v", d)
		}
		if d.Progress != 0.75 || d.TimeLeftSec != 300 || len(d.Items) != 1 || d.Items[0].Label != "S1 E2–E3" {
			t.Fatalf("progress/items: %+v", d)
		}
		if d.NextAir == nil || !d.NextAir.Equal(*future) {
			t.Fatalf("next air: %v", d.NextAir)
		}
		if len(d.Groups) != 1 || d.Groups[0].State != DLDownloading {
			t.Fatalf("groups: %+v", d.Groups)
		}
	})

	t.Run("import blocked beats downloading, with the arr's message", func(t *testing.T) {
		s := newTestService()
		eps := []arr.Episode{ep(2, 1, 2, false, past, nil), ep(3, 1, 3, false, past, nil)}
		q := []arr.QueueItem{
			{ID: 10, DownloadID: "a", EpisodeID: 2, Status: "downloading", Size: 10, SizeLeft: 5},
			{ID: 11, DownloadID: "b", EpisodeID: 3, Status: "completed", TrackedDownloadStatus: "warning", TrackedDownloadState: "importBlocked",
				StatusMessages: []arr.StatusMessage{{Title: "x.mkv", Messages: []string{"No files found are eligible for import"}}}},
		}
		d := s.seriesStatus(r, eps, q, nil, now)
		if d.State != DLAttention || !strings.Contains(d.Message, "No files found") {
			t.Fatalf("got %s %q", d.State, d.Message)
		}
		if len(d.Groups) != 2 || d.Groups[0].State != DLAttention {
			t.Fatalf("groups order: %+v", d.Groups)
		}
	})

	t.Run("failed in history, nothing queued", func(t *testing.T) {
		s := newTestService()
		eps := []arr.Episode{ep(2, 1, 2, false, past, searched)}
		h := []arr.HistoryRecord{ // newest first
			{EpisodeID: 2, EventType: "downloadFailed", Date: now.Add(-10 * time.Minute), Data: map[string]string{"message": "Out of retention"}},
			{EpisodeID: 2, EventType: "grabbed", Date: now.Add(-20 * time.Minute)},
		}
		d := s.seriesStatus(r, eps, nil, h, now)
		if d.State != DLFailed || d.Message != "Out of retention" {
			t.Fatalf("got %s %q", d.State, d.Message)
		}
	})

	t.Run("searched without a grab", func(t *testing.T) {
		s := newTestService()
		eps := []arr.Episode{ep(2, 1, 2, false, past, searched), ep(3, 1, 3, false, past, searched)}
		d := s.seriesStatus(r, eps, nil, nil, now)
		if d.State != DLNotFound || d.LastSearch == nil {
			t.Fatalf("got %+v", d)
		}
	})

	t.Run("nothing aired yet", func(t *testing.T) {
		s := newTestService()
		d := s.seriesStatus(r, []arr.Episode{ep(4, 1, 4, false, future, nil)}, nil, nil, now)
		if d.State != DLWaiting || d.Upcoming != 1 {
			t.Fatalf("got %+v", d)
		}
	})

	t.Run("complete; episodes outside the request ignored", func(t *testing.T) {
		s := newTestService()
		eps := []arr.Episode{ep(1, 1, 1, true, past, nil), ep(9, 2, 1, false, past, searched)}
		d := s.seriesStatus(r, eps, nil, nil, now)
		if d.State != DLComplete || d.Wanted != 1 {
			t.Fatalf("got %+v", d)
		}
	})

	t.Run("retry after a failure says so", func(t *testing.T) {
		s := newTestService()
		eps := []arr.Episode{ep(2, 1, 2, false, past, nil)}
		q := []arr.QueueItem{{ID: 10, DownloadID: "n", EpisodeID: 2, Status: "downloading", Size: 10, SizeLeft: 9}}
		h := []arr.HistoryRecord{
			{EpisodeID: 2, EventType: "grabbed", Date: now.Add(-time.Minute)},
			{EpisodeID: 2, EventType: "downloadFailed", Date: now.Add(-2 * time.Minute), Data: map[string]string{"message": "Unpacking failed"}},
		}
		d := s.seriesStatus(r, eps, q, h, now)
		if d.State != DLDownloading || !strings.Contains(d.Message, "Unpacking failed") {
			t.Fatalf("got %s %q", d.State, d.Message)
		}
	})

	t.Run("no bytes for ten minutes is a stall", func(t *testing.T) {
		s := newTestService()
		eps := []arr.Episode{ep(2, 1, 2, false, past, nil)}
		q := []arr.QueueItem{{ID: 10, DownloadID: "z", EpisodeID: 2, Status: "downloading", Size: 10, SizeLeft: 6}}
		s.seriesStatus(r, eps, q, nil, now)
		d := s.seriesStatus(r, eps, q, nil, now.Add(11*time.Minute))
		if d.State != DLStalled || !strings.Contains(d.Message, "No progress") {
			t.Fatalf("got %s %q", d.State, d.Message)
		}
	})
}

func TestMovieStatus(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	r := &store.Request{Title: "Spirited Away", MediaType: "movie"}
	s := newTestService()

	if d := s.movieStatus(r, &arr.Movie{IsAvailable: false, DigitalRelease: tp(now.Add(48 * time.Hour))}, nil, nil, now); d.State != DLWaiting || d.NextAir == nil {
		t.Fatalf("unreleased: %+v", d)
	}
	if d := s.movieStatus(r, &arr.Movie{HasFile: true}, nil, nil, now); d.State != DLComplete {
		t.Fatalf("has file: %+v", d)
	}
	paused := []arr.QueueItem{{ID: 1, DownloadID: "p", Status: "paused", Size: 10, SizeLeft: 6}}
	if d := s.movieStatus(r, &arr.Movie{IsAvailable: true}, paused, nil, now); d.State != DLStalled || d.Items[0].Label != "Spirited Away" {
		t.Fatalf("paused: %+v", d)
	}
	h := []arr.HistoryRecord{{EventType: "downloadFailed", Date: now.Add(-time.Minute), Data: map[string]string{"message": "bad"}}, {EventType: "grabbed", Date: now.Add(-time.Hour)}}
	if d := s.movieStatus(r, &arr.Movie{IsAvailable: true, LastSearchTime: tp(now.Add(-2 * time.Hour))}, nil, h, now); d.State != DLFailed || d.Message != "bad" {
		t.Fatalf("failed: %+v", d)
	}
	if d := s.movieStatus(r, &arr.Movie{IsAvailable: true, LastSearchTime: tp(now.Add(-time.Hour))}, nil, nil, now); d.State != DLNotFound {
		t.Fatalf("not found: %+v", d)
	}
}

func TestTimeLeft(t *testing.T) {
	for in, want := range map[string]int{"00:12:34": 754, "1.02:03:04": 93784, "": -1, "garbage": -1} {
		if got := (arr.QueueItem{TimeLeft: in}).TimeLeftSeconds(); got != want {
			t.Errorf("%q: %d want %d", in, got, want)
		}
	}
}

func TestEpisodeStates(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	past, future := tp(now.Add(-30*24*time.Hour)), tp(now.Add(7*24*time.Hour))
	r := &store.Request{Seasons: []int{1}}
	s := newTestService()
	eps := []arr.Episode{
		ep(1, 1, 1, true, past, nil),    // has a file: not listed
		ep(2, 1, 2, false, past, nil),   // downloading
		ep(3, 1, 3, false, past, nil),   // failed earlier
		ep(4, 1, 4, false, past, nil),   // nothing yet: searching
		ep(5, 1, 5, false, future, nil), // airs next week
	}
	q := []arr.QueueItem{{ID: 7, DownloadID: "x", EpisodeID: 2, Status: "downloading", Size: 100, SizeLeft: 40, TimeLeft: "00:02:00"}}
	h := []arr.HistoryRecord{{EpisodeID: 3, EventType: "downloadFailed", Date: now.Add(-time.Minute), Data: map[string]string{"message": "bad"}}}
	d := s.seriesStatus(r, eps, q, h, now)
	got := map[int]EpisodeDL{}
	for _, e := range d.Episodes {
		got[e.Episode] = e
	}
	if len(d.Episodes) != 4 {
		t.Fatalf("want 4 missing episodes, got %+v", d.Episodes)
	}
	if e := got[2]; e.State != DLDownloading || e.Progress != 0.6 || e.TimeLeftSec != 120 {
		t.Errorf("E2: %+v", e)
	}
	if e := got[3]; e.State != DLFailed || e.Message != "bad" {
		t.Errorf("E3: %+v", e)
	}
	if e := got[4]; e.State != DLSearching {
		t.Errorf("E4: %+v", e)
	}
	if e := got[5]; e.State != DLWaiting || e.AirDate == "" {
		t.Errorf("E5: %+v", e)
	}
	if !codeSet([]string{" s1e3 "})["S1E3"] || epCode(1, 3) != "S1E3" {
		t.Error("codes")
	}
}

func TestSearchAndRetryGuards(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	past := tp(now.Add(-30 * 24 * time.Hour))
	r := &store.Request{Seasons: []int{1}, ArrID: 7}
	two := 2 // a running SeasonSearch for season 2 covers none of these

	s := newTestService()
	eps := []arr.Episode{
		ep(1, 1, 1, false, past, tp(now.Add(-2*time.Minute))), // searched 2 min ago
		ep(2, 1, 2, false, past, nil),                         // stalled in the client
		ep(3, 1, 3, false, past, tp(now.Add(-time.Hour))),     // searched an hour ago
		ep(4, 1, 4, false, past, nil),                         // covered by a running SeasonSearch? no: season 2 below
	}
	q := []arr.QueueItem{{ID: 9, DownloadID: "st", EpisodeID: 2, Status: "paused", Size: 10, SizeLeft: 5}}
	cmds := []arr.Command{{Name: "EpisodeSearch", Status: "started"}}
	cmds[0].Body.EpisodeIDs = []int{4}
	other := arr.Command{Name: "SeasonSearch", Status: "queued"}
	other.Body.SeriesID, other.Body.SeasonNumber = 7, &two
	cmds = append(cmds, other)

	d := s.seriesStatusWith(r, eps, q, nil, cmds, now)
	got := map[int]EpisodeDL{}
	for _, e := range d.Episodes {
		got[e.Episode] = e
	}
	if got[1].CanSearch {
		t.Error("E1 was searched 2 min ago: no search yet")
	}
	if got[2].CanSearch || got[2].CanRetry {
		t.Errorf("E2 just stalled: neither search nor retry yet: %+v", got[2])
	}
	if !got[3].CanSearch {
		t.Error("E3 searched an hour ago: searchable")
	}
	if got[4].CanSearch || got[4].State != DLSearching || got[4].Message == "" {
		t.Errorf("E4 is being searched: %+v", got[4])
	}
	if !d.CanSearch || d.CanRetry {
		t.Errorf("request: canSearch=%v canRetry=%v", d.CanSearch, d.CanRetry)
	}

	// Eleven minutes later the same stall may be replaced.
	d = s.seriesStatusWith(r, eps, q, nil, nil, now.Add(11*time.Minute))
	for _, e := range d.Episodes {
		if e.Episode == 2 && !e.CanRetry {
			t.Errorf("E2 stalled 11 min: retry allowed: %+v", e)
		}
	}
	if !d.CanRetry {
		t.Error("request canRetry after a long stall")
	}
}

// Right after a series is added Sonarr has not listed its episodes yet. That used to read
// as 0 of 0 = "complete": every app said "Downloaded" and stopped looking for 10 minutes.
func TestSeriesStatusBeforeEpisodesLoad(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	s := newTestService()
	fresh := &store.Request{UpdatedAt: now.Add(-5 * time.Second).Format(time.RFC3339Nano)}
	d := s.seriesStatus(fresh, nil, nil, nil, now)
	if d.State != DLSearching || d.Wanted != 0 {
		t.Fatalf("just added: got %s, want %s", d.State, DLSearching)
	}
	// Long after: TVDB lists nothing for the season asked for yet (an announced season).
	old := &store.Request{Seasons: []int{3}, UpdatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano)}
	past := tp(now.Add(-24 * time.Hour))
	d = s.seriesStatus(old, []arr.Episode{ep(1, 1, 1, true, past, nil)}, nil, nil, now)
	if d.State != DLWaiting || d.WaitingFor != "airing" {
		t.Fatalf("season not listed: got %s/%s, want waiting/airing", d.State, d.WaitingFor)
	}
	if d.State == DLComplete {
		t.Fatal("never complete with nothing to have")
	}
}

// A search that grabbed a release finishes before the release shows in the arr's queue
// (it polls the download client a little later). In that gap the request used to read
// "not found"; it is queued.
func TestJustGrabbedIsQueuedNotNotFound(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	past := tp(now.Add(-30 * 24 * time.Hour))
	searched := tp(now.Add(-20 * time.Second))
	grab := arr.HistoryRecord{EpisodeID: 2, EventType: "grabbed", Date: now.Add(-10 * time.Second)}
	s := newTestService()
	r := &store.Request{Seasons: []int{1}}
	eps := []arr.Episode{ep(2, 1, 1, false, past, searched)}
	d := s.seriesStatus(r, eps, nil, []arr.HistoryRecord{grab}, now)
	if d.State != DLQueued || len(d.Episodes) != 1 || d.Episodes[0].State != DLQueued {
		t.Fatalf("series: got %s %+v, want queued", d.State, d.Episodes)
	}
	// The same grab long ago with nothing in the queue: it is gone; not found again.
	old := grab
	old.Date = now.Add(-2 * time.Hour)
	oldSearch := tp(now.Add(-3 * time.Hour))
	d = s.seriesStatus(r, []arr.Episode{ep(2, 1, 1, false, past, oldSearch)}, nil, []arr.HistoryRecord{old}, now)
	if d.State == DLQueued {
		t.Fatalf("stale grab: still queued")
	}
	// Movie.
	m := &arr.Movie{ID: 6, IsAvailable: true, LastSearchTime: searched}
	d = s.movieStatus(&store.Request{Title: "X"}, m, nil, []arr.HistoryRecord{{EventType: "grabbed", Date: now.Add(-10 * time.Second)}}, now)
	if d.State != DLQueued {
		t.Fatalf("movie: got %s, want queued", d.State)
	}
	d = s.movieStatus(&store.Request{Title: "X"}, m, nil, nil, now)
	if d.State != DLNotFound {
		t.Fatalf("movie with no grab: got %s, want notFound", d.State)
	}
}

// A torrent the client calls "stalled, no connections" seconds after the grab is looking
// for peers, not a problem; after a couple of minutes it is stalled.
func TestFreshStallWarningIsQueued(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	past := tp(now.Add(-30 * 24 * time.Hour))
	s := newTestService()
	r := &store.Request{Seasons: []int{1}}
	eps := []arr.Episode{ep(2, 1, 1, false, past, nil)}
	q := []arr.QueueItem{{ID: 10, DownloadID: "x", EpisodeID: 2, Status: "warning", TrackedDownloadStatus: "warning", Size: 100, SizeLeft: 100,
		StatusMessages: []arr.StatusMessage{{Title: "x", Messages: []string{"The download is stalled with no connections"}}}}}
	d := s.seriesStatus(r, eps, q, nil, now)
	if d.State != DLQueued || d.IsProblem {
		t.Fatalf("fresh: got %s %q", d.State, d.Message)
	}
	d = s.seriesStatus(r, eps, q, nil, now.Add(3*time.Minute))
	if d.State != DLStalled {
		t.Fatalf("after 3 min: got %s, want stalled", d.State)
	}
	// Paused is a problem straight away.
	q[0].Status, q[0].TrackedDownloadStatus = "paused", ""
	s2 := newTestService()
	if d := s2.seriesStatus(r, eps, q, nil, now); d.State != DLStalled {
		t.Fatalf("paused: got %s", d.State)
	}
}

// Every requested episode has a file and the only download replaces one (a dub for a
// sub-only file): complete with a note, not "downloading".
func TestUpgradeOfCompleteSeasonIsComplete(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	past := tp(now.Add(-30 * 24 * time.Hour))
	s := newTestService()
	r := &store.Request{Seasons: []int{1}}
	eps := []arr.Episode{ep(1, 1, 1, true, past, nil), ep(2, 1, 2, true, past, nil)}
	q := []arr.QueueItem{{ID: 10, DownloadID: "x", EpisodeID: 2, Status: "downloading", TrackedDownloadState: "downloading", Size: 100, SizeLeft: 27}}
	d := s.seriesStatus(r, eps, q, nil, now)
	if d.State != DLComplete || d.Message != "Upgrading S1 E2 to a better release (73%)." {
		t.Fatalf("got %s %q", d.State, d.Message)
	}
	// A missing episode downloading is still "downloading".
	eps[1].HasFile = false
	if d := s.seriesStatus(r, eps, q, nil, now); d.State != DLDownloading {
		t.Fatalf("missing episode: got %s", d.State)
	}
}
