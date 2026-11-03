package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
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
	// Decoded over the saved settings: a field the client does not send keeps its value.
	// An older app that has never heard of a setting (the older-anime profile, say) must
	// not wipe it on every save. Clearing a value means sending it empty or 0.
	cur := s.discover.ArrSettings()
	body := cur
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, 400, "bad body: "+err.Error())
		return
	}
	// The saved key is kept only for the saved address: kept for a new one, it would be
	// sent to whatever server that is.
	for _, k := range []struct {
		name     string
		new, old *arr.Config
	}{{"Sonarr", &body.Sonarr, &cur.Sonarr}, {"Radarr", &body.Radarr, &cur.Radarr}} {
		if k.new.APIKey == maskedKey || k.new.APIKey == k.old.APIKey {
			if !sameArrURL(k.new.URL, k.old.URL) && k.old.APIKey != "" && strings.TrimSpace(k.new.URL) != "" {
				writeError(w, 400, "Type the "+k.name+" API key again: the address changed.")
				return
			}
			k.new.APIKey = k.old.APIKey
		}
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
		saved := cur.Sonarr
		if body.Kind == "radarr" {
			saved = cur.Radarr
		}
		// The saved key only goes to the saved address (a test against another URL sent
		// it there, and echoed what came back).
		if !sameArrURL(body.URL, saved.URL) {
			writeError(w, 400, "Type the API key again to test a different address.")
			return
		}
		body.APIKey = saved.APIKey
	}
	p, err := s.discover.TestArr(r.Context(), body.Kind, arr.Config{URL: body.URL, APIKey: body.APIKey})
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	// Does Roxy read each folder the arr app would save into?
	if libs, err := s.store.ListLibraries(r.Context()); err == nil {
		for i := range p.RootFolders {
			rf := &p.RootFolders[i]
			root := filepath.Clean(rf.Path)
			for _, l := range libs {
				if root == l.Root || within(root, l.Root) {
					rf.Roxy, rf.RoxyMatch = l.Name, "yes"
					break
				}
				if rf.RoxyMatch == "" && strings.EqualFold(filepath.Base(root), filepath.Base(l.Root)) {
					rf.Roxy, rf.RoxyMatch = l.Name, "maybe"
				}
			}
		}
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

// sameArrURL: the same arr address, ignoring case, spaces and a trailing slash.
func sameArrURL(a, b string) bool {
	norm := func(s string) string { return strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/")) }
	return norm(a) == norm(b)
}
