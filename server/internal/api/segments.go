package api

import (
	"context"
	"net/http"
)

// POST /v1/segments/detect?season=<id>&dry=1
//
// Runs intro/outro detection by audio fingerprint for one season and returns the report
// (what was found per episode next to what the chapter markers said). `dry=1` reports
// without writing. Without `season`, every season still missing segments is queued in
// the background (202).
func (s *Server) detectSegments(w http.ResponseWriter, r *http.Request) {
	if s.segs == nil {
		writeError(w, 503, "segment detection not running")
		return
	}
	season := r.URL.Query().Get("season")
	if season == "" {
		go s.segs.SweepAll(context.Background())
		w.WriteHeader(http.StatusAccepted)
		return
	}
	dry := r.URL.Query().Get("dry") == "1"
	rep, err := s.segs.DetectSeason(r.Context(), season, dry)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rep)
}
