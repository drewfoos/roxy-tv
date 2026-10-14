package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

func (s *Server) listLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := s.store.ListLibraries(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if libs == nil {
		libs = []model.Library{}
	}
	writeJSON(w, 200, map[string]any{"libraries": libs})
}

// POST /v1/libraries/scan?force=1 re-probes unchanged files too.
func (s *Server) scanAll(w http.ResponseWriter, r *http.Request) {
	if f := r.URL.Query().Get("force"); f == "1" || f == "true" {
		s.lib.Force = true
	}
	// Detached from the request context: the scan must outlive this 202 response.
	go func() { _ = s.lib.ScanAll(context.Background()) }()
	w.WriteHeader(http.StatusAccepted)
}

// GET /v1/items?type=episode,movie&parent=<id>&recursive=1&library=<id>&sort=date_created|title|index&desc=1&limit=&offset=
func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	q := store.ItemQuery{
		ParentID:  r.URL.Query().Get("parent"),
		Recursive: r.URL.Query().Get("recursive") == "1" || r.URL.Query().Get("recursive") == "true",
		SortBy:    r.URL.Query().Get("sort"),
		Desc:      r.URL.Query().Get("desc") == "1" || r.URL.Query().Get("desc") == "true",
	}
	if t := r.URL.Query().Get("type"); t != "" {
		for _, p := range strings.Split(t, ",") {
			q.Types = append(q.Types, model.ItemType(strings.TrimSpace(p)))
		}
	}
	if l := r.URL.Query().Get("library"); l != "" {
		q.LibraryID, _ = strconv.ParseInt(l, 10, 64)
	}
	// hasMedia=1 hides missing/unaired placeholders; hasMedia=0 lists only those.
	if hm := r.URL.Query().Get("hasMedia"); hm != "" {
		v := hm == "1" || hm == "true"
		q.HasMedia = &v
	}
	// favorites=1: only the caller's favourites (the client's Watchlist).
	if f := r.URL.Query().Get("favorites"); f == "1" || f == "true" {
		p := auth.PrincipalFrom(r.Context())
		if p == nil || p.User == nil {
			writeError(w, 401, "a user login is required")
			return
		}
		q.FavoriteOf = p.ScopeID
	}
	q.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	q.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if q.SortBy == "" || q.SortBy == "date_created" {
		q.SortBy = "date_created"
		if r.URL.Query().Get("desc") == "" {
			q.Desc = true
		}
	}
	items, total, err := s.store.ListItems(r.Context(), q)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []model.Item{}
	}
	if r.URL.Query().Get("include") == "sources" {
		for i := range items {
			items[i].Sources, _ = s.store.SourcesForItem(r.Context(), items[i].ID)
		}
	}
	s.fillSeriesRefs(r.Context(), items)
	fillAvailability(items)
	s.attachUserData(r.Context(), items)
	s.fillAudioLanguages(r.Context(), items)
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}

// fillSeriesRefs resolves episode -> season -> series with one query so list views can
// label episodes (seriesId, seriesTitle) without a round trip per row.
func (s *Server) fillSeriesRefs(ctx context.Context, items []model.Item) {
	var seasonIDs []string
	seen := map[string]bool{}
	for i := range items {
		if items[i].Type != model.TypeEpisode || items[i].ParentID == nil || items[i].SeriesID != "" {
			continue
		}
		if id := *items[i].ParentID; !seen[id] {
			seen[id] = true
			seasonIDs = append(seasonIDs, id)
		}
	}
	if len(seasonIDs) == 0 {
		return
	}
	refs, err := s.store.SeriesForSeasons(ctx, seasonIDs)
	if err != nil {
		return
	}
	for i := range items {
		if items[i].Type != model.TypeEpisode || items[i].ParentID == nil || items[i].SeriesID != "" {
			continue
		}
		if ref, ok := refs[*items[i].ParentID]; ok {
			items[i].SeriesID, items[i].SeriesTitle = ref.ID, ref.Title
		}
	}
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	it, err := s.store.GetItem(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if it == nil {
		writeError(w, 404, "item not found")
		return
	}
	it.Sources, err = s.store.SourcesForItem(r.Context(), it.ID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	one := []model.Item{*it}
	s.fillSeriesRefs(r.Context(), one)
	fillAvailability(one)
	s.attachUserData(r.Context(), one)
	s.fillAudioLanguages(r.Context(), one)
	writeJSON(w, 200, one[0])
}

func (s *Server) getSource(w http.ResponseWriter, r *http.Request) {
	src, err := s.store.GetSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if src == nil {
		writeError(w, 404, "source not found")
		return
	}
	writeJSON(w, 200, src)
}
