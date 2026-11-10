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
	if p := userFrom(w, r); p == nil {
		return
	} else if !p.IsAdmin {
		writeError(w, 403, "admin only")
		return
	}
	f := r.URL.Query().Get("force")
	force := f == "1" || f == "true"
	// Detached from the request context: the scan must outlive this 202 response.
	go func() { _ = s.lib.ScanAll(context.Background(), force) }()
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
	// A parent id the client still holds from before its file moved names the item that
	// replaced it (as GET /v1/items/{id} does): the list used to come back empty.
	if q.ParentID != "" {
		if parent, err := s.store.GetItemOrMoved(r.Context(), q.ParentID); err == nil && parent != nil {
			q.ParentID = parent.ID
		}
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
	if r.URL.Query().Get("include") == "sources" && q.Limit > store.MaxItemsWithSources {
		q.Limit = store.MaxItemsWithSources // with files and tracks a page is ~4x the JSON
	}
	q.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if q.SortBy == "" || q.SortBy == "date_created" {
		q.SortBy = "date_created"
		if r.URL.Query().Get("desc") == "" {
			q.Desc = true
		}
	}
	// Read before the list: a change made while this page is read moves it by the next
	// page, and the client reads again (never the other way round).
	version, err := s.store.ItemsVersion(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
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
		if err := s.attachSources(r.Context(), items); err != nil {
			writeError(w, 500, err.Error())
			return
		}
	}
	s.fillSeriesRefs(r.Context(), items)
	fillAvailability(items)
	s.attachUserData(r.Context(), items)
	s.fillAudioLanguages(r.Context(), items)
	// version: items' change counter (store.ItemsVersion); a client reading pages starts
	// over when it differs between them. It covers the list itself (which items, in which
	// order: items rows), not what each item carries besides (sources, availability, the
	// caller's user data, audio languages): no snapshot of the whole response.
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "version": version})
}

// attachSources fills Sources for every item of a page in one batched lookup. A failed
// lookup is returned, never left as items without sources: those used to be sent with
// HTTP 200, and an episode with no sources reads to a client as one with nothing to play.
func (s *Server) attachSources(ctx context.Context, items []model.Item) error {
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	byItem, err := s.store.SourcesForItems(ctx, ids)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Sources = byItem[items[i].ID]
	}
	return nil
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
	it, err := s.store.GetItemOrMoved(r.Context(), chi.URLParam(r, "id"))
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

// GET /v1/items/{id}/extras -> {items}: a film's or show's trailers, featurettes and the
// like, trailers first, each with its file (play one like any item).
func (s *Server) itemExtras(w http.ResponseWriter, r *http.Request) {
	it, err := s.store.GetItemOrMoved(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if it == nil {
		writeError(w, 404, "item not found")
		return
	}
	extras, err := s.store.Extras(r.Context(), it.ID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if extras == nil {
		extras = []model.Item{}
	}
	fillAvailability(extras)
	s.attachUserData(r.Context(), extras)
	writeJSON(w, 200, map[string]any{"items": extras})
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
