package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
)

// Native watch state (docs/api/native.md "Watch state"). Shares the store with the
// Jellyfin-compatible session endpoints, so progress reported either way is one record.

// playedThreshold: a position at or beyond this fraction of the runtime counts as
// watched (Jellyfin's default MaxResumePct is 90).
const playedThreshold = 0.90

func userFrom(w http.ResponseWriter, r *http.Request) *model.Principal {
	p := auth.PrincipalFrom(r.Context())
	if p == nil || p.User == nil {
		writeError(w, 401, "a user login is required")
		return nil
	}
	return p
}

// GET /v1/me/resume?limit=
func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.store.ResumeItems(r.Context(), p.ScopeID, limit)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []model.Item{}
	}
	s.fillSeriesRefs(r.Context(), items)
	s.attachUserData(r.Context(), items)
	writeJSON(w, 200, map[string]any{"items": items})
}

// GET /v1/me/history?limit= -> {items}: what the profile played, most recent first.
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.store.HistoryItems(r.Context(), p.ScopeID, limit)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []model.Item{}
	}
	s.fillSeriesRefs(r.Context(), items)
	s.attachUserData(r.Context(), items)
	writeJSON(w, 200, map[string]any{"items": items})
}

// SubtitlesOff is the subtitleIndex a client sends, and gets back in userData, for
// "subtitles turned off". It is never a stream index.
const SubtitlesOff = -1

// POST /v1/me/progress {itemId, positionMs, audioIndex?, subtitleIndex?} -> 204
func (s *Server) progress(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	var body struct {
		ItemID        string `json:"itemId"`
		PositionMs    int64  `json:"positionMs"`
		AudioIndex    *int   `json:"audioIndex"`
		SubtitleIndex *int   `json:"subtitleIndex"`
		// When the position was reached (Unix ms), sent by a phone replaying reports it
		// made offline. Live reports leave it out.
		At int64 `json:"at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ItemID == "" {
		writeError(w, 400, "invalid request body")
		return
	}
	if body.PositionMs < 0 {
		body.PositionMs = 0
	}
	// subtitleIndex -1 means "subtitles off" and is stored as such, so the choice sticks
	// (omitting it keeps the saved one, which used to bring subtitles back next time).
	// Any other negative is read as off too, so a stored index is a real stream or -1.
	if body.SubtitleIndex != nil && *body.SubtitleIndex < 0 {
		off := SubtitlesOff
		body.SubtitleIndex = &off
	}
	it, err := s.store.GetItem(r.Context(), body.ItemID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if it == nil {
		writeError(w, 404, "item not found")
		return
	}
	played := it.RuntimeMs != nil && *it.RuntimeMs > 0 && float64(body.PositionMs) >= float64(*it.RuntimeMs)*playedThreshold
	// A replayed offline report (At set) from before the item's latest activity (finished
	// or watched further on another device since) would wind it back, and on a watched
	// episode leave it watched and part-way at once. The AsOf writes drop it, checking in
	// the same statement as the write: a separate read first let a live report from another
	// device land in between and then be overwritten by the older one.
	applied := true
	switch {
	case body.At > 0 && played:
		applied, err = s.store.FinishPlayAsOf(r.Context(), p.ScopeID, it.GUID, time.UnixMilli(body.At))
	case body.At > 0:
		applied, err = s.store.SetProgressAsOf(r.Context(), p.ScopeID, it.GUID, time.UnixMilli(body.At), body.PositionMs, body.AudioIndex, body.SubtitleIndex)
	case played:
		err = s.store.FinishPlay(r.Context(), p.ScopeID, it.GUID)
	default:
		err = s.store.SetProgress(r.Context(), p.ScopeID, it.GUID, body.PositionMs, body.AudioIndex, body.SubtitleIndex)
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	switch {
	case !applied:
		slog.Info("progress: older than the item's latest activity, ignored", "item", it.Title, "positionMs", body.PositionMs)
	case played:
		slog.Info("playback finished", "user", p.User.Name, "item", it.Title, "positionMs", body.PositionMs)
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST|DELETE /v1/me/played/{itemId}
func (s *Server) played(w http.ResponseWriter, r *http.Request) {
	s.setFlag(w, r, func(ctx context.Context, userID string, it *model.Item, on bool) error {
		// A series or season stands for its episodes ("mark all as watched").
		if it.Type == model.TypeSeries || it.Type == model.TypeSeason {
			return s.store.MarkPlayedUnder(ctx, userID, it.ID, on)
		}
		return s.store.MarkPlayed(ctx, userID, it.GUID, on)
	})
}

// POST|DELETE /v1/me/favorite/{itemId}
func (s *Server) favorite(w http.ResponseWriter, r *http.Request) {
	s.setFlag(w, r, func(ctx context.Context, userID string, it *model.Item, on bool) error {
		return s.store.SetFavorite(ctx, userID, it.GUID, on)
	})
}

func (s *Server) setFlag(w http.ResponseWriter, r *http.Request, set func(ctx context.Context, userID string, it *model.Item, on bool) error) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	it, err := s.store.GetItem(r.Context(), chi.URLParam(r, "itemId"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if it == nil {
		writeError(w, 404, "item not found")
		return
	}
	if err := set(r.Context(), p.ScopeID, it, r.Method == http.MethodPost); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	d, err := s.store.GetUserItemData(r.Context(), p.ScopeID, it.GUID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"itemId": it.ID, "userData": d.Native()})
}
