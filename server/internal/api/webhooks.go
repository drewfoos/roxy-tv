package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/auth"
)

// Sonarr / Radarr webhooks (docs/api/arr-webhooks.md). The arr app is configured with a
// Webhook connection pointing at /v1/webhooks/sonarr or /radarr and an API key, either as
// the X-Api-Key header or ?api_key= in the URL. An API key or an admin token is required;
// an ordinary user's token is not enough to change the library.

func (s *Server) webhookGuard(w http.ResponseWriter, r *http.Request) bool {
	p := auth.PrincipalFrom(r.Context()) // userFrom() insists on a user; API keys are the point here
	if p == nil {
		writeError(w, 401, "unauthorized")
		return false
	}
	if !p.IsAPIKey && !p.IsAdmin {
		writeError(w, 403, "webhooks need an API key or an admin token")
		return false
	}
	if s.hooks == nil {
		writeError(w, 503, "webhooks not available")
		return false
	}
	return true
}

// POST /v1/webhooks/{source}  (source: sonarr | radarr) -> the recorded event
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if !s.webhookGuard(w, r) {
		return
	}
	source := strings.ToLower(chi.URLParam(r, "source"))
	if source != "sonarr" && source != "radarr" {
		writeError(w, 404, "unknown source")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, 400, "body too large")
		return
	}
	ev, err := s.hooks.Handle(r.Context(), source, body)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, ev)
}

// GET /v1/webhooks/events?limit= (admin) -> {events}
func (s *Server) webhookEvents(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.store.ListWebhookEvents(r.Context(), limit)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"events": events})
}

// ---- API keys (admin): what the arr apps authenticate with ----

// GET /v1/auth/keys -> {keys: [{id, name, createdAt}]}
func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	keys, err := s.store.ListAPIKeys(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"keys": keys})
}

// POST /v1/auth/keys {name} -> {id, name, token}: the token is shown once.
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, 400, "name required")
		return
	}
	k, err := s.authSvc.CreateAPIKey(r.Context(), strings.TrimSpace(body.Name))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": k.ID, "name": k.Name, "token": k.Token, "createdAt": k.CreatedAt})
}

// DELETE /v1/auth/keys/{id} -> 204
func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if err := s.store.DeleteAPIKeyByID(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}
