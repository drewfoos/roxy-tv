package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Library settings (admin): scan / refresh the library, see what failed to match, and
// fix a match by hand (Identify) or type a title's details (custom shows like One Pace).

// GET /v1/library/manage -> {scanning, lastScanAt, queue, titles}
func (s *Server) libraryManage(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	titles, err := s.store.LibraryTitles(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if titles == nil {
		titles = []store.TitleStatus{}
	}
	running, last, lastErr := s.lib.ScanStatus()
	out := map[string]any{"scanning": running, "titles": titles, "queue": 0}
	if !last.IsZero() {
		out["lastScanAt"] = last.UTC().Format(time.RFC3339)
	}
	if lastErr != nil {
		out["lastScanError"] = lastErr.Error() // which libraries failed, and why
	}
	// Libraries found with no media files and left alone: the admin can confirm one.
	empty := []map[string]any{}
	for id, files := range s.lib.EmptyRefused() {
		if lib, err := s.store.GetLibrary(r.Context(), id); err == nil && lib != nil {
			empty = append(empty, map[string]any{"id": strconv.FormatInt(id, 10), "name": lib.Name, "root": lib.Root, "files": files})
		}
	}
	out["emptyLibraries"] = empty
	if s.meta != nil {
		out["queue"] = s.meta.QueueLength()
	}
	writeJSON(w, 200, out)
}

// POST /v1/library/scan {mode: files | missing | replace} -> 202
//   - files:   find new, changed and removed files (then look up anything new)
//   - missing: look up titles that have no match yet
//   - replace: look up every title again and replace its details (locked ones are kept)
func (s *Server) libraryScan(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch body.Mode {
	case "", "files":
		go func() { _ = s.lib.ScanAll(context.Background(), false) }()
	case "missing", "replace":
		if s.meta == nil {
			writeError(w, 503, "metadata service not running")
			return
		}
		missing := body.Mode == "missing"
		go func() { _ = s.meta.RefreshAll(context.Background(), missing) }()
	default:
		writeError(w, 400, "mode must be files, missing or replace")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// POST /v1/library/refresh {ids, all, metadata, images, trickplay, segments} -> 202
// Jellyfin's "Refresh metadata" dialog: redo any mix of parts for some titles (series or
// movies) or, with all, the whole library. Locked titles keep their details.
func (s *Server) libraryRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		IDs       []string `json:"ids"`
		All       bool     `json:"all"`
		Metadata  bool     `json:"metadata"`
		Images    bool     `json:"images"`
		Trickplay bool     `json:"trickplay"`
		Segments  bool     `json:"segments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad body")
		return
	}
	if !body.Metadata && !body.Images && !body.Trickplay && !body.Segments {
		writeError(w, 400, "pick something to refresh")
		return
	}
	ids := body.IDs
	if body.All {
		all, err := s.store.TopLevelItemIDs(r.Context())
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		ids = all
	}
	if len(ids) == 0 {
		writeError(w, 400, "no titles given")
		return
	}
	go func() {
		ctx := context.Background()
		for _, id := range ids {
			if s.meta != nil && (body.Metadata || body.Images) {
				if body.Images {
					s.meta.RefreshImages(id)
				} else {
					s.meta.Refresh(id)
				}
			}
			if body.Trickplay && s.trick != nil {
				if srcs, err := s.store.SourceIDsUnder(ctx, id); err == nil {
					for _, src := range srcs {
						s.trick.Regenerate(src)
					}
				}
			}
			if body.Segments && s.segs != nil {
				if seasons, err := s.store.Children(ctx, id, model.TypeSeason); err == nil {
					for _, se := range seasons {
						s.segs.RedetectSeason(se.ID)
					}
				}
			}
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

// GET /v1/items/{id}/identify?provider=tmdb|anilist&q=&year= -> {candidates}
func (s *Server) identifySearch(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	it, ok := s.topItem(w, r)
	if !ok {
		return
	}
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		q = it.Title
	}
	var year *int
	if y, err := strconv.Atoi(r.URL.Query().Get("year")); err == nil && y > 0 {
		year = &y
	}
	cands, err := s.meta.Search(r.Context(), it.Type, r.URL.Query().Get("provider"), q, year)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"candidates": cands, "query": q})
}

// POST /v1/items/{id}/identify {provider, id} -> 202
func (s *Server) identifyApply(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	it, ok := s.topItem(w, r)
	if !ok {
		return
	}
	var body struct {
		Provider string `json:"provider"`
		ID       string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		writeError(w, 400, "provider and id are required")
		return
	}
	if err := s.meta.Identify(r.Context(), it.ID, body.Provider, body.ID); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// PUT /v1/items/{id}/metadata {title, year, overview, genres, studio, premiered, locked} -> item
func (s *Server) editMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	it, err := s.store.GetItem(r.Context(), chi.URLParam(r, "id"))
	if err != nil || it == nil {
		writeError(w, 404, "item not found")
		return
	}
	var m store.ManualMetadata
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil || strings.TrimSpace(m.Title) == "" {
		writeError(w, 400, "a title is required")
		return
	}
	if err := s.store.EditItemMetadata(r.Context(), it.ID, m); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	updated, _ := s.store.GetItem(r.Context(), it.ID)
	writeJSON(w, 200, updated)
}

// topItem loads the {id} item and checks it is a series or movie (what Identify works on).
func (s *Server) topItem(w http.ResponseWriter, r *http.Request) (*model.Item, bool) {
	if s.meta == nil {
		writeError(w, 503, "metadata service not running")
		return nil, false
	}
	it, err := s.store.GetItem(r.Context(), chi.URLParam(r, "id"))
	if err != nil || it == nil {
		writeError(w, 404, "item not found")
		return nil, false
	}
	if it.Type != model.TypeSeries && it.Type != model.TypeMovie {
		writeError(w, 400, "only shows and movies can be identified")
		return nil, false
	}
	return it, true
}
