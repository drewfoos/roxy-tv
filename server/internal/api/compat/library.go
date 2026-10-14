package compat

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

// POST /Library/Media/Updated {Updates:[{Path, UpdateType}]} -> 204
// Sonarr/Radarr's Emby/Jellyfin connect sends the imported file or folder path.
func (h *Handler) mediaUpdated(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Updates []struct {
			Path       string `json:"Path"`
			UpdateType string `json:"UpdateType"`
		} `json:"Updates"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	for _, u := range body.Updates {
		if u.Path == "" {
			continue
		}
		slog.Info("library update notification", "path", u.Path, "type", u.UpdateType)
		go func(p string) {
			if err := h.lib.IngestPath(context.Background(), p); err != nil {
				slog.Warn("ingest from notification failed", "path", p, "err", err)
			}
		}(u.Path)
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /Library/Refresh -> 204, full rescan in the background.
func (h *Handler) libraryRefresh(w http.ResponseWriter, r *http.Request) {
	go func() { _ = h.lib.ScanAll(context.Background()) }()
	w.WriteHeader(http.StatusNoContent)
}

// POST /Notifications/Admin -> 204 (Sonarr/Radarr "test" notification)
func (h *Handler) adminNotification(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"Name"`
		Description string `json:"Description"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	slog.Info("admin notification", "name", body.Name, "description", body.Description)
	w.WriteHeader(http.StatusNoContent)
}
