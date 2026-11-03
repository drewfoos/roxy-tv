package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// GET /v1/sources/{id}/token -> {token, expiresAt}: a media token for this source, for
// the URL a player opens (?mt=<token>), so the account token never goes into a URL.
func (s *Server) mediaToken(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	if p.MediaSource != "" {
		writeError(w, 403, "a media token cannot mint tokens")
		return
	}
	src, err := s.store.GetSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil || src == nil {
		writeError(w, 404, "source not found")
		return
	}
	tok, exp, err := s.authSvc.NewMediaToken(r.Context(), src.ID, p.UserID, p.DeviceID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"token": tok, "expiresAt": exp.UTC().Format(time.RFC3339)})
}
