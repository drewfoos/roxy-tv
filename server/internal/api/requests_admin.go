package api

import (
	"net/http"

	"github.com/roxy-media/roxy/server/internal/discover"
	"github.com/roxy-media/roxy/server/internal/store"
)

// GET /v1/requests (admin) -> {requests, waiting}: every request with who asked, when, what
// (seasons / episodes), its state and download progress; the admin's Requests page.
func (s *Server) adminRequests(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.discoverGuard(w) {
		return
	}
	all, err := s.store.ListRequests(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	type row struct {
		store.Request
		Poster   string             `json:"poster,omitempty"`
		Download *discover.Download `json:"download,omitempty"`
	}
	out := make([]row, 0, len(all))
	waiting := 0
	for _, q := range all {
		if q.Status == "approval" {
			waiting++
		}
		out = append(out, row{Request: q, Poster: q.PosterPath, Download: s.discover.Download(q.ID)})
	}
	writeJSON(w, 200, map[string]any{"requests": out, "waiting": waiting})
}
