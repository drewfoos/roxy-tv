package api

import (
	"encoding/json"
	"log/slog"
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
		upstreamError(w, "discover rows", err)
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
		upstreamError(w, "discover search", err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// GET /v1/discover/tv/{tmdbId}/season/{n} -> {episodes: [{number, title, overview, airDate, still, have}]}
func (s *Server) discoverEpisodes(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	id, err1 := strconv.Atoi(chi.URLParam(r, "tmdbId"))
	n, err2 := strconv.Atoi(chi.URLParam(r, "n"))
	if err1 != nil || err2 != nil {
		writeError(w, 400, "bad id")
		return
	}
	eps, err := s.discover.Episodes(r.Context(), id, n)
	if err != nil {
		upstreamError(w, "discover episodes", err)
		return
	}
	writeJSON(w, 200, map[string]any{"episodes": eps})
}

// GET /v1/discover/{type}/{tmdbId}
// GET /v1/people/{tmdbId} -> Person: a cast member and their credits, library titles marked.
func (s *Server) person(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "tmdbId"))
	if err != nil || id <= 0 {
		writeError(w, 400, "bad tmdb id")
		return
	}
	p, err := s.discover.Person(r.Context(), id)
	if err != nil {
		upstreamError(w, "person", err)
		return
	}
	writeJSON(w, 200, p)
}

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
		upstreamError(w, "discover detail", err)
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
		Type     string   `json:"type"`
		TMDBID   int      `json:"tmdbId"`
		Seasons  []int    `json:"seasons"`
		Episodes []string `json:"episodes"` // specials, "S0E5"
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.TMDBID == 0 || (body.Type != "tv" && body.Type != "movie") {
		writeError(w, 400, "type and tmdbId required")
		return
	}
	if len(body.Episodes) > 12 {
		writeError(w, 400, "at most 12 episodes per request")
		return
	}
	req, err := s.discover.Request(r.Context(), body.Type, body.TMDBID, p, body.Seasons, body.Episodes)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, req)
}

// upstreamError logs the real cause and tells the client only that TMDB did not answer:
// transport errors quote URLs and must not reach the app.
func upstreamError(w http.ResponseWriter, what string, err error) {
	slog.Warn(what+" failed", "err", err)
	writeError(w, 502, "TMDB did not answer; try again in a moment")
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
