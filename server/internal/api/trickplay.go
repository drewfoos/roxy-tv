package api

import (
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// GET /v1/sources/{id}/trickplay
//
// 200 with the manifest when the sheets exist; 202 {"status":"generating"} after
// queueing them (the client retries later); 404 for an unknown source, or one whose
// thumbnails could not be made (a clip shorter than one interval, a file ffmpeg cannot
// decode): that used to say "generating" for the hour before the next try, so clients
// kept waiting for nothing.
func (s *Server) trickplayManifest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !safeID(id) {
		writeError(w, 404, "no such source")
		return
	}
	if s.trick == nil {
		writeError(w, 404, "trickplay disabled")
		return
	}
	if m := s.trick.Manifest(id); m != nil {
		w.Header().Set("Cache-Control", "private, max-age=3600")
		writeJSON(w, 200, m)
		return
	}
	src, err := s.store.GetSource(r.Context(), id)
	if err != nil || src == nil {
		writeError(w, 404, "source not found")
		return
	}
	if s.trick.Failed(id) {
		writeError(w, 404, "no seek previews for this file")
		return
	}
	s.trick.Enqueue(id)
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "generating"})
}

// GET /v1/sources/{id}/trickplay/{n}: sheet n (0-based), JPEG.
func (s *Server) trickplaySheet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if s.trick == nil || err != nil || n < 0 || !safeID(id) {
		writeError(w, 404, "no such sheet")
		return
	}
	p := s.trick.SheetPath(id, n)
	if _, err := os.Stat(p); err != nil {
		writeError(w, 404, "no such sheet")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	// private: it needs a sign-in, so no shared cache may keep it. An hour, not a year
	// and immutable: a file replaced at the same path (a re-download) gets new sheets
	// under the same URL, which browsers would otherwise never fetch.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, p)
}

// safeID: a source id is letters, digits and dashes, so it can name a folder safely
// (never "..", never a path).
func safeID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
