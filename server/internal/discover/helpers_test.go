package discover

import (
	"time"

	"github.com/roxy-media/roxy/server/internal/arr"
	"github.com/roxy-media/roxy/server/internal/store"
)

// seriesStatus and movieStatus are the status with no commands running.
func (s *Service) seriesStatus(r *store.Request, eps []arr.Episode, queue []arr.QueueItem, history []arr.HistoryRecord, now time.Time) *Download {
	return s.seriesStatusWith(r, eps, queue, history, nil, now)
}

func (s *Service) movieStatus(r *store.Request, m *arr.Movie, queue []arr.QueueItem, history []arr.HistoryRecord, now time.Time) *Download {
	return s.movieStatusWith(r, m, queue, history, nil, now)
}
