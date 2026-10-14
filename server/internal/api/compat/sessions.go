package compat

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Jellyfin PlaybackStartInfo / PlaybackProgressInfo / PlaybackStopInfo share these fields.
type playbackReport struct {
	ItemID              string `json:"ItemId"`
	MediaSourceID       string `json:"MediaSourceId"`
	PlaySessionID       string `json:"PlaySessionId"`
	PositionTicks       int64  `json:"PositionTicks"`
	IsPaused            bool   `json:"IsPaused"`
	AudioStreamIndex    *int   `json:"AudioStreamIndex"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex"`
}

// playedThreshold: a stop at or beyond this fraction of the runtime counts as watched
// (Jellyfin's default MaxResumePct is 90).
const playedThreshold = 0.90

func (h *Handler) decodeReport(r *http.Request) (*playbackReport, *model.Item, *model.Principal) {
	var rep playbackReport
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&rep)
	}
	q := r.URL.Query()
	if rep.ItemID == "" {
		rep.ItemID = first(q, "itemId", "ItemId")
	}
	if rep.PositionTicks == 0 {
		rep.PositionTicks, _ = strconv.ParseInt(first(q, "positionTicks", "PositionTicks"), 10, 64)
	}
	p := auth.PrincipalFrom(r.Context())
	if p == nil || p.User == nil || rep.ItemID == "" {
		return &rep, nil, p
	}
	it, err := h.store.GetItem(r.Context(), rep.ItemID)
	if err != nil || it == nil {
		return &rep, nil, p
	}
	return &rep, it, p
}

// POST /Sessions/Playing
func (h *Handler) playingStart(w http.ResponseWriter, r *http.Request) {
	rep, it, p := h.decodeReport(r)
	if it != nil {
		slog.Info("playback start", "user", p.User.Name, "item", it.Title, "session", rep.PlaySessionID)
		_ = h.store.SetProgress(r.Context(), p.UserID, it.GUID, rep.PositionTicks/10_000, rep.AudioStreamIndex, rep.SubtitleStreamIndex)
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /Sessions/Playing/Progress
func (h *Handler) playingProgress(w http.ResponseWriter, r *http.Request) {
	rep, it, p := h.decodeReport(r)
	if it != nil {
		_ = h.store.SetProgress(r.Context(), p.UserID, it.GUID, rep.PositionTicks/10_000, rep.AudioStreamIndex, rep.SubtitleStreamIndex)
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /Sessions/Playing/Stopped
func (h *Handler) playingStopped(w http.ResponseWriter, r *http.Request) {
	rep, it, p := h.decodeReport(r)
	if it != nil {
		posMs := rep.PositionTicks / 10_000
		played := false
		if it.RuntimeMs != nil && *it.RuntimeMs > 0 && float64(posMs) >= float64(*it.RuntimeMs)*playedThreshold {
			played = true
		}
		slog.Info("playback stopped", "user", p.User.Name, "item", it.Title, "positionMs", posMs, "played", played)
		if played {
			_ = h.store.MarkPlayed(r.Context(), p.UserID, it.GUID, true)
		} else {
			_ = h.store.SetProgress(r.Context(), p.UserID, it.GUID, posMs, rep.AudioStreamIndex, rep.SubtitleStreamIndex)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST|DELETE /Users/{userId}/PlayedItems/{id}  and  /UserPlayedItems/{id}
func (h *Handler) playedItem(w http.ResponseWriter, r *http.Request) {
	h.setFlag(w, r, func(ctx context.Context, userID, guid string, on bool) error {
		return h.store.MarkPlayed(ctx, userID, guid, on)
	})
}

// POST|DELETE /Users/{userId}/FavoriteItems/{id}  and  /UserFavoriteItems/{id}
func (h *Handler) favoriteItem(w http.ResponseWriter, r *http.Request) {
	h.setFlag(w, r, func(ctx context.Context, userID, guid string, on bool) error {
		return h.store.SetFavorite(ctx, userID, guid, on)
	})
}

func (h *Handler) setFlag(w http.ResponseWriter, r *http.Request, set func(ctx context.Context, userID, guid string, on bool) error) {
	p := auth.PrincipalFrom(r.Context())
	if p == nil || p.User == nil {
		writeError(w, 401, "Unauthorized")
		return
	}
	it, err := h.store.GetItem(r.Context(), chi.URLParam(r, "id"))
	if err != nil || it == nil {
		writeError(w, 404, "Item not found")
		return
	}
	if err := set(r.Context(), p.UserID, it.GUID, r.Method == http.MethodPost); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	d, _ := h.store.GetUserItemData(r.Context(), p.UserID, it.GUID)
	writeJSON(w, 200, userDataDTO(it, d))
}

// GET /Users/{userId}/Items/Resume and /UserItems/Resume
func (h *Handler) resumeItems(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if p == nil || p.User == nil {
		writeError(w, 401, "Unauthorized")
		return
	}
	limit, _ := strconv.Atoi(first(r.URL.Query(), "Limit", "limit"))
	items, err := h.store.ResumeItems(r.Context(), p.UserID, limit)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, h.itemDTO(r.Context(), &items[i], true))
	}
	writeJSON(w, 200, map[string]any{"Items": out, "TotalRecordCount": len(out), "StartIndex": 0})
}

// userDataDTO renders Jellyfin's UserItemDataDto.
func userDataDTO(it *model.Item, d *store.UserItemData) map[string]any {
	dto := map[string]any{
		"Key":                   it.GUID,
		"ItemId":                it.ID,
		"PlaybackPositionTicks": int64(0),
		"PlayCount":             0,
		"IsFavorite":            false,
		"Played":                false,
	}
	if d == nil {
		return dto
	}
	dto["PlaybackPositionTicks"] = ticks(d.PositionMs)
	dto["PlayCount"] = d.PlayCount
	dto["IsFavorite"] = d.IsFavorite
	dto["Played"] = d.Played
	if !d.LastPlayedAt.IsZero() {
		dto["LastPlayedDate"] = jfTime(d.LastPlayedAt)
	}
	if it.RuntimeMs != nil && *it.RuntimeMs > 0 && d.PositionMs > 0 {
		dto["PlayedPercentage"] = float64(d.PositionMs) / float64(*it.RuntimeMs) * 100
	}
	return dto
}
