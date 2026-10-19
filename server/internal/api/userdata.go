package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ItemID == "" {
		writeError(w, 400, "invalid request body")
		return
	}
	if body.PositionMs < 0 {
		body.PositionMs = 0
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
	if played {
		err = s.store.MarkPlayed(r.Context(), p.ScopeID, it.GUID, true)
		slog.Info("playback finished", "user", p.User.Name, "item", it.Title, "positionMs", body.PositionMs)
	} else {
		err = s.store.SetProgress(r.Context(), p.ScopeID, it.GUID, body.PositionMs, body.AudioIndex, body.SubtitleIndex)
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
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
