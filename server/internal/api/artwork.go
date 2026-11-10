package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
)

// Artwork picking (admin): every backdrop / poster / logo the providers hold for a
// title, so the app can offer a grid and PUT /images/{type} locks the choice in.

// FanartSettings is the fanart.tv configuration (settings key "fanart").
type FanartSettings struct {
	APIKey string `json:"apiKey"`
}

func (s *Server) fanartClient(r *http.Request) *metadata.Fanart {
	var cfg FanartSettings
	if ok, err := s.store.GetSetting(r.Context(), "fanart", &cfg); err != nil || !ok {
		return nil
	}
	return metadata.NewFanart(cfg.APIKey)
}

// GET /v1/settings/fanart -> {apiKey (masked), enabled}
func (s *Server) fanartSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var cfg FanartSettings
	_, _ = s.store.GetSetting(r.Context(), "fanart", &cfg)
	writeJSON(w, 200, map[string]any{"apiKey": maskIf(cfg.APIKey), "enabled": cfg.APIKey != ""})
}

// PUT /v1/settings/fanart {apiKey} -> same as GET. An empty key disables fanart.tv.
func (s *Server) fanartSettingsPut(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body FanartSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, 400, "bad body: "+err.Error())
		return
	}
	var cur FanartSettings
	_, _ = s.store.GetSetting(r.Context(), "fanart", &cur)
	if body.APIKey == maskedKey {
		body.APIKey = cur.APIKey
	}
	body.APIKey = strings.TrimSpace(body.APIKey)
	if err := s.store.SetSetting(r.Context(), "fanart", body); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"apiKey": maskIf(body.APIKey), "enabled": body.APIKey != ""})
}

func maskIf(key string) string {
	if key == "" {
		return ""
	}
	return maskedKey
}

// GET /v1/items/{id}/images/{type}/candidates -> {candidates: [...]} (admin).
// TMDB's set for the title, plus fanart.tv's when a key is configured; the art in use
// is marked and sorted first, the rest by the providers' votes.
func (s *Server) imageCandidates(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	typ := chi.URLParam(r, "type")
	if typ != "backdrop" && typ != "poster" && typ != "logo" && typ != "hero" {
		writeError(w, 400, "type must be backdrop, poster, logo or hero")
		return
	}
	it, err := s.store.GetItemOrMoved(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if it == nil {
		writeError(w, 404, "item not found")
		return
	}
	isMovie := it.Type == model.TypeMovie
	var out []metadata.Candidate
	if s.meta != nil {
		if t := s.meta.TMDB(); t != nil {
			if id, _ := strconv.Atoi(it.ProviderIDs["tmdb"]); id != 0 {
				c, err := t.Candidates(r.Context(), isMovie, id, typ)
				if err != nil {
					slog.Warn("artwork: tmdb candidates", "item", it.Title, "err", err)
				}
				out = append(out, c...)
			}
		}
	}
	if f := s.fanartClient(r); f.Enabled() {
		id := it.ProviderIDs["tvdb"]
		if isMovie {
			id = it.ProviderIDs["tmdb"]
		}
		c, err := f.Candidates(r.Context(), isMovie, id, typ)
		if err != nil {
			slog.Warn("artwork: fanart candidates", "item", it.Title, "err", err)
		}
		out = append(out, c...)
	}
	if img, err := s.store.GetItemImage(r.Context(), it.ID, typ); err == nil && img != nil {
		for i := range out {
			if out[i].URL == img.SourceURL {
				out[i].Selected = true
			}
		}
	}
	metadata.SortCandidates(out)
	if out == nil {
		out = []metadata.Candidate{}
	}
	writeJSON(w, 200, map[string]any{"candidates": out})
}
