package compat

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// The media routes reuse the native handlers but accept Jellyfin's URL shapes.
// Jellyfin addresses streams by item id + mediaSourceId; Roxy's native handlers take a
// source id, so these shims resolve the source and rewrite the route params.

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	sourceID := firstQuery(r, "mediaSourceId", "MediaSourceId")
	if sourceID == "" {
		sources, err := h.store.SourcesForItem(r.Context(), itemID)
		if err != nil || len(sources) == 0 {
			writeError(w, 404, "Item has no media sources")
			return
		}
		sourceID = sources[0].ID
	}
	h.Stream(w, withParam(r, "id", sourceID))
}

func (h *Handler) attachment(w http.ResponseWriter, r *http.Request) {
	h.Attachment(w, withParam(r, "id", chi.URLParam(r, "sourceId")))
}

func (h *Handler) subtitle(w http.ResponseWriter, r *http.Request) {
	h.Subtitle(w, withParam(r, "id", chi.URLParam(r, "sourceId")))
}

func firstQuery(r *http.Request, keys ...string) string {
	for _, k := range keys {
		if v := r.URL.Query().Get(k); v != "" {
			return v
		}
	}
	return ""
}

// withParam returns a shallow copy of r whose chi URL param `key` is replaced.
func withParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.RouteContext(r.Context())
	for i, k := range rctx.URLParams.Keys {
		if strings.EqualFold(k, key) {
			rctx.URLParams.Values[i] = value
			return r
		}
	}
	rctx.URLParams.Add(key, value)
	return r
}
