package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/roxy-media/roxy/server/internal/arr"
	"github.com/roxy-media/roxy/server/internal/discover"
)

// Arr settings (admin): the Sonarr / Radarr connection Discover requests go to.

const maskedKey = "••••••••"

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	p := userFrom(w, r)
	if p == nil {
		return false
	}
	if !p.IsAdmin {
		writeError(w, 403, "admin only")
		return false
	}
	return true
}

func mask(c arr.Config) arr.Config {
	if c.APIKey != "" {
		c.APIKey = maskedKey
	}
	return c
}

// GET /v1/settings/arr -> {sonarr, radarr} with API keys masked, plus whether each is connected.
func (s *Server) arrSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.discoverGuard(w) {
		return
	}
	cfg := s.discover.ArrSettings()
	writeJSON(w, 200, map[string]any{
		"sonarr":          mask(cfg.Sonarr),
		"radarr":          mask(cfg.Radarr),
		"requestsEnabled": map[string]bool{"tv": s.discover.RequestsEnabled("tv"), "movie": s.discover.RequestsEnabled("movie")},
	})
}

// PUT /v1/settings/arr {sonarr, radarr}: a masked API key keeps the saved one.
func (s *Server) arrSettingsPut(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.discoverGuard(w) {
		return
	}
	var body discover.ArrSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, 400, "bad body: "+err.Error())
		return
	}
	cur := s.discover.ArrSettings()
	if body.Sonarr.APIKey == maskedKey {
		body.Sonarr.APIKey = cur.Sonarr.APIKey
	}
	if body.Radarr.APIKey == maskedKey {
		body.Radarr.APIKey = cur.Radarr.APIKey
	}
	for _, c := range []*arr.Config{&body.Sonarr, &body.Radarr} {
		c.URL = strings.TrimSpace(c.URL)
		if c.URL != "" && !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") {
			writeError(w, 400, "URLs must start with http:// or https://")
			return
		}
	}
	if err := s.discover.SaveArrSettings(r.Context(), body); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.arrSettingsGet(w, r)
}

// POST /v1/settings/arr/test {kind, url, apiKey} -> {version, appName, rootFolders, qualityProfiles, tags}
// A masked key tests the saved one.
func (s *Server) arrSettingsTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.discoverGuard(w) {
		return
	}
	var body struct {
		Kind   string `json:"kind"`
		URL    string `json:"url"`
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, 400, "bad body")
		return
	}
	cur := s.discover.ArrSettings()
	if body.APIKey == maskedKey {
		if body.Kind == "radarr" {
			body.APIKey = cur.Radarr.APIKey
		} else {
			body.APIKey = cur.Sonarr.APIKey
		}
	}
	p, err := s.discover.TestArr(r.Context(), body.Kind, arr.Config{URL: body.URL, APIKey: body.APIKey})
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

// POST /v1/discover/requests/retry (admin): push pending requests to the arr apps now.
func (s *Server) discoverRetry(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.discoverGuard(w) {
		return
	}
	s.discover.RetryPending(r.Context())
	items, err := s.discover.Requests(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// GET /v1/discover/browse?type=tv|movie&sort=popular|top|newest|oldest|votes&genres=16,35&lang=ja&anime=1&yearFrom=&yearTo=&minRating=&status=airing|ended&page=
func (s *Server) discoverBrowse(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	q := r.URL.Query()
	bq := discover.BrowseQuery{
		Type: q.Get("type"), Sort: q.Get("sort"), Language: strings.ToLower(q.Get("lang")), Status: q.Get("status"),
		Anime: q.Get("anime") == "1" || q.Get("anime") == "true",
	}
	for _, g := range strings.Split(q.Get("genres"), ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(g)); err == nil && n > 0 {
			bq.Genres = append(bq.Genres, n)
		}
	}
	bq.YearFrom, _ = strconv.Atoi(q.Get("yearFrom"))
	bq.YearTo, _ = strconv.Atoi(q.Get("yearTo"))
	bq.MinRating, _ = strconv.ParseFloat(q.Get("minRating"), 64)
	bq.Page, _ = strconv.Atoi(q.Get("page"))
	if len(bq.Language) > 5 {
		bq.Language = bq.Language[:5]
	}
	res, err := s.discover.Browse(r.Context(), bq)
	if err != nil {
		upstreamError(w, "discover browse", err)
		return
	}
	writeJSON(w, 200, res)
}

// GET /v1/discover/genres?type=tv|movie -> {genres: [{id, name}]}
func (s *Server) discoverGenres(w http.ResponseWriter, r *http.Request) {
	if !s.discoverGuard(w) {
		return
	}
	writeJSON(w, 200, map[string]any{"genres": s.discover.Genres(r.Context(), r.URL.Query().Get("type"))})
}
