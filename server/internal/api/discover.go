package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/discover"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Discover (Roxy's request flow): TMDB rows and search annotated with what the library has
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

// GET /v1/discover/tv/{tmdbId}/films -> {films: [Film], auto}: the films of an anime
// series, each with where it sits in the season list, whether it may come along with a
// request for the series (eligible) and whether the main Request button brings it now
// (withRequest). The rules are the server's alone (discover/films.go): clients show
// what this says and send back the withRequest films' ids, nothing more.
func (s *Server) discoverFilms(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "tmdbId"))
	if err != nil || id <= 0 {
		writeError(w, 400, "bad tmdb id")
		return
	}
	films, err := s.discover.Films(r.Context(), id)
	if err != nil {
		upstreamError(w, "discover films", err)
		return
	}
	if films.Films == nil {
		films.Films = []discover.Film{}
	}
	writeJSON(w, 200, films)
}

// GET /v1/discover/{type}/{tmdbId}
// GET /v1/items/{id}/similar -> {items: [Discover Item]}: "More like this" for a library
// title, from TMDB by the title's TMDB id; empty for titles TMDB does not know.
func (s *Server) itemSimilar(w http.ResponseWriter, r *http.Request) {
	it, err := s.store.GetItemOrMoved(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if it == nil {
		writeError(w, 404, "item not found")
		return
	}
	id, _ := strconv.Atoi(it.ProviderIDs["tmdb"])
	if s.discover == nil || id == 0 {
		writeJSON(w, 200, map[string]any{"items": []discover.Item{}})
		return
	}
	kind := "tv"
	if it.Type == model.TypeMovie {
		kind = "movie"
	}
	items, err := s.discover.Similar(r.Context(), kind, id)
	if err != nil {
		upstreamError(w, "similar", err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

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
	// Who asked is the requester's and the admins' business: others see the request's
	// state, not the account behind it. (A copy: the detail is cached and shared.)
	if p := auth.PrincipalFrom(r.Context()); d.Request != nil && (p == nil || (!p.IsAdmin && p.UserID != d.Request.UserID)) {
		dd, rq := *d, *d.Request
		rq.UserID, rq.UserName, rq.ArrID = "", "", 0
		dd.Request = &rq
		d = &dd
	}
	writeJSON(w, 200, d)
}

// GET /v1/discover/{type}/{tmdbId}/details -> the "More details" page for a title that
// may not be in the library: the same shape as /v1/items/{id}/details (cast, keywords,
// companies, dates, links) with the TMDB facts standing in for the library item.
func (s *Server) discoverMoreDetails(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "tmdbId"))
	kind := chi.URLParam(r, "type")
	if err != nil || (kind != "tv" && kind != "movie") {
		writeError(w, 400, "bad type or tmdb id")
		return
	}
	d, err := s.discover.Detail(r.Context(), kind, id)
	if err != nil {
		upstreamError(w, "discover details", err)
		return
	}
	it := model.Item{Type: model.TypeSeries, Title: d.Title, Overview: d.Overview, Genres: d.Genres, IsAnime: d.IsAnime, ProviderIDs: map[string]string{"tmdb": strconv.Itoa(id)}}
	if kind == "movie" {
		it.Type = model.TypeMovie
	}
	if d.Year > 0 {
		y := d.Year
		it.Year = &y
	}
	if d.Rating > 0 {
		rt := d.Rating
		it.CommunityRating = &rt
	}
	if d.SeasonCount > 0 {
		n := d.SeasonCount
		it.SeasonCount = &n
	}
	resp := detailsResponse{Item: it, Backdrop: d.Backdrop, Links: map[string]string{"tmdb": "https://www.themoviedb.org/" + kind + "/" + strconv.Itoa(id)}}
	if d.IMDBID != "" {
		resp.Links["imdb"] = "https://www.imdb.com/title/" + d.IMDBID + "/"
	}
	if d.TVDBID > 0 {
		resp.Links["tvdb"] = "https://thetvdb.com/dereferrer/series/" + strconv.Itoa(d.TVDBID)
	}
	if s.meta != nil && s.meta.TMDB() != nil {
		resp.TMDB = s.tmdbExtras(r.Context(), kind == "movie", id)
		// The page's season count follows Discover's layout (an episode group may split a
		// TMDB "season" the way Sonarr counts them).
		if resp.TMDB != nil && d.SeasonCount > 0 {
			c := *resp.TMDB
			c.Seasons = d.SeasonCount
			resp.TMDB = &c
		}
	}
	writeJSON(w, 200, resp)
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
		// Films: TMDB ids of the series' films to request with it (the withRequest films
		// of GET …/films). The server requests only those it would itself put on the
		// button, and from then on brings later films between the seasons this covered.
		Films []int `json:"films"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.TMDBID == 0 || (body.Type != "tv" && body.Type != "movie") {
		writeError(w, 400, "type and tmdbId required")
		return
	}
	if len(body.Episodes) > 12 {
		writeError(w, 400, "at most 12 episodes per request")
		return
	}
	if len(body.Films) > 40 || (len(body.Films) > 0 && (body.Type != "tv" || len(body.Episodes) > 0)) {
		writeError(w, 400, "films come with a request for the series or its seasons")
		return
	}
	// Requests go straight to Sonarr / Radarr; without one there is nowhere to send them,
	// so the app asks an admin to connect it instead of queueing requests nobody acts on.
	if !s.discover.RequestsEnabled(body.Type) {
		app := "Sonarr"
		if body.Type == "movie" {
			app = "Radarr"
		}
		writeError(w, 412, app+" isn't connected to Roxy Server yet. An admin can connect it under Account → Server → Sonarr & Radarr.")
		return
	}
	var req *store.Request
	var err error
	if len(body.Films) > 0 {
		req, err = s.discover.RequestWithFilms(r.Context(), body.TMDBID, p, body.Seasons, body.Films)
	} else {
		req, err = s.discover.Request(r.Context(), body.Type, body.TMDBID, p, body.Seasons, body.Episodes)
	}
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, req)
}

// POST /v1/discover/requests/{id}/search -> {ok}: ask Sonarr / Radarr to look again for
// what is still missing. Anyone may (it only searches).
func (s *Server) discoverSearchAgain(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) || userFrom(w, r) == nil {
		return
	}
	body, ok := searchBody(w, r)
	if !ok {
		return
	}
	if err := s.discover.SearchAgain(r.Context(), chi.URLParam(r, "id"), body.Seasons, body.Episodes...); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /v1/discover/requests/{id}/replace (admin) -> {ok}: drop the failed / stalled /
// unimportable downloads, blocklist those releases and let the arr app pick another.
func (s *Server) discoverReplaceRelease(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.discoverGuard(w) {
		return
	}
	body, ok := searchBody(w, r)
	if !ok {
		return
	}
	if err := s.discover.TryAnotherRelease(r.Context(), chi.URLParam(r, "id"), body.Episodes...); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// searchBody reads an optional {episodes: ["S1E3", ...], seasons: [1]} body: act on those
// episodes and seasons only (empty: everything missing).
type searchTarget struct {
	Episodes []string `json:"episodes"`
	Seasons  []int    `json:"seasons"`
}

// searchBody returns the selection, or answers 400 and returns false.
//
// An empty selection means everything missing, so only a body that really says so may
// produce one: no body at all, or empty lists. Anything malformed is a 400 and nothing
// runs. Decode errors used to be ignored, so a broken body (or `{"episodes": [""]}`,
// whose blank code the service drops) became "everything" and could search for, or
// blocklist and replace, every missing episode instead of the one meant. Too many
// entries used to be cut off silently; now they are refused too. Episode codes are
// checked and written the way the service compares them ("S01E03" -> "S1E3").
func searchBody(w http.ResponseWriter, r *http.Request) (searchTarget, bool) {
	var body searchTarget
	var raw json.RawMessage
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	if err := dec.Decode(&raw); errors.Is(err, io.EOF) {
		return body, true // no body: everything missing, as documented
	} else if err != nil {
		writeError(w, 400, "invalid request body")
		return body, false
	} else if dec.More() {
		writeError(w, 400, "invalid request body: more than one JSON value")
		return body, false
	}
	// An object, with no null in it: JSON null decodes to "nothing chosen", which here means
	// everything missing, so a null body or a null list would act on everything too.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		writeError(w, 400, "invalid request body: want a JSON object")
		return body, false
	}
	for k, v := range fields {
		if string(bytes.TrimSpace(v)) == "null" {
			writeError(w, 400, fmt.Sprintf("invalid request body: %s is null (leave it out, or send an empty list)", k))
			return body, false
		}
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, 400, "invalid request body")
		return body, false
	}
	if len(body.Episodes) > 200 || len(body.Seasons) > 100 {
		writeError(w, 400, "too many episodes or seasons (at most 200 and 100)")
		return body, false
	}
	for i, code := range body.Episodes {
		m := episodeCodeRe.FindStringSubmatch(strings.TrimSpace(code))
		if m == nil {
			writeError(w, 400, fmt.Sprintf("not an episode code: %q (want S1E3)", code))
			return body, false
		}
		season, _ := strconv.Atoi(m[1])
		episode, _ := strconv.Atoi(m[2])
		body.Episodes[i] = fmt.Sprintf("S%dE%d", season, episode)
	}
	for _, n := range body.Seasons {
		if n < 0 {
			writeError(w, 400, fmt.Sprintf("not a season number: %d", n))
			return body, false
		}
	}
	return body, true
}

var episodeCodeRe = regexp.MustCompile(`^[sS](\d{1,4})[eE](\d{1,5})$`)

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

// POST /v1/discover/requests/{id}/approve (admin) -> request: a basic user's request goes on to Sonarr / Radarr.
func (s *Server) discoverApprove(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.discoverGuard(w) {
		return
	}
	req, err := s.discover.Approve(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, req)
}

// POST /v1/discover/requests/{id}/decline {reason?} (admin) -> request
func (s *Server) discoverDecline(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.discoverGuard(w) {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&body)
	req, err := s.discover.Decline(r.Context(), chi.URLParam(r, "id"), body.Reason)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, req)
}
