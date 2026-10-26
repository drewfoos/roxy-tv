package discover

import (
	"strings"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/arr"
	"github.com/roxy-media/roxy/server/internal/store"
)

func newTestService() *Service {
	return &Service{dl: tracker{status: map[string]*Download{}, progress: map[string]progressMark{}, kick: make(chan struct{}, 1), refresh: map[string]time.Time{}}}
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
		if d.Progress != 0.75 || d.TimeLeftSec != 300 || len(d.Items) != 1 || d.Items[0].Label != "S01E02–E03" {
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
