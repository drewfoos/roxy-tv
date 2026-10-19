package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Discover ("our own request app"): TMDB rows and search annotated with what the library has
// and what was requested; requests go to Sonarr / Radarr when configured.

func (s *Server) discoverGuard(w http.ResponseWriter) bool {
	if s.discover == nil || !s.discover.Enabled() {
		writeError(w, 503, "Discover needs a TMDB key on the server (ROXY_TMDB_API_KEY)")
		return false
	}
	return true
}

// GET /v1/discover -> {rows: [{key, title, items}], requestsEnabled: {tv, movie}}
func (s *Server) discoverRows(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	rows, err := s.discover.Rows(r.Context())
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"rows":            rows,
		"requestsEnabled": map[string]bool{"tv": s.discover.RequestsEnabled("tv"), "movie": s.discover.RequestsEnabled("movie")},
	})
}

// GET /v1/discover/search?q=
func (s *Server) discoverSearch(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	items, err := s.discover.Search(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// GET /v1/discover/{type}/{tmdbId}
func (s *Server) discoverDetail(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "tmdbId"))
	if err != nil {
		writeError(w, 400, "bad tmdb id")
		return
	}
	d, err := s.discover.Detail(r.Context(), chi.URLParam(r, "type"), id)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, d)
}

// POST /v1/discover/request {type, tmdbId} -> the request row
func (s *Server) discoverRequest(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	p := userFrom(w, r)
	if p == nil {
		return
	}
	var body struct {
		Type   string `json:"type"`
		TMDBID int    `json:"tmdbId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.TMDBID == 0 || (body.Type != "tv" && body.Type != "movie") {
		writeError(w, 400, "type and tmdbId required")
		return
	}
	req, err := s.discover.Request(r.Context(), body.Type, body.TMDBID, p)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, req)
}

// GET /v1/discover/requests -> {items}
func (s *Server) discoverRequests(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	items, err := s.discover.Requests(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// DELETE /v1/discover/requests/{id}
func (s *Server) discoverDeleteRequest(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	p := userFrom(w, r)
	if p == nil {
		return
	}
	if err := s.discover.Delete(r.Context(), chi.URLParam(r, "id"), p); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	w.WriteHeader(204)
}
