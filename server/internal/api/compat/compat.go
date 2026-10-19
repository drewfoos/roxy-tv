// Package compat implements the Jellyfin-compatible subset of Roxy's API
// (docs/api/jellyfin-compat.md). Shapes follow Jellyfin 12.x so request app, Sonarr, Radarr
// and the Roxy TV app v1 (ADR-0006) work unchanged.
//
// This file: server identity, item listing/detail, PlaybackInfo, stream/attachment/
// subtitle routes. Auth, users and library notifications live in their own files.
package compat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// ServerID is a stable identifier clients cache; it must not change between restarts.
const ServerID = "roxy0000000000000000000000000001"

const Version = "10.11.0" // the Jellyfin version we claim compatibility with

// ---- system ----

func (h *Handler) systemInfoPublic(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"LocalAddress":           "",
		"ServerName":             "Roxy Server",
		"Version":                Version,
		"ProductName":            "Roxy Server",
		"OperatingSystem":        "",
		"Id":                     ServerID,
		"StartupWizardCompleted": true,
	})
}

func (h *Handler) systemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"ServerName":             "Roxy Server",
		"Version":                Version,
		"ProductName":            "Roxy Server",
		"Id":                     ServerID,
		"StartupWizardCompleted": true,
		"OperatingSystem":        "",
		"HasPendingRestart":      false,
		"IsShuttingDown":         false,
		"SupportsLibraryMonitor": true,
		"WebSocketPortNumber":    8096,
		"CompletedInstallations": []any{},
		"CanSelfRestart":         false,
		"CanLaunchWebBrowser":    false,
		"ProgramDataPath":        h.cfg.DataDir,
		"WebPath":                "",
		"ItemsByNamePath":        "",
		"CachePath":              h.cfg.CacheDir(),
		"LogPath":                "",
		"InternalMetadataPath":   "",
		"TranscodingTempPath":    "",
		"HasUpdateAvailable":     false,
		"EncoderLocation":        "Custom",
		"SystemArchitecture":     "X64",
		"LocalAddress":           "",
	})
}

func (h *Handler) systemConfiguration(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ServerName": "Roxy Server", "EnableMetrics": false})
}

// ---- libraries ----

func (h *Handler) mediaFolders(w http.ResponseWriter, r *http.Request) {
	libs, err := h.store.ListLibraries(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(libs))
	for _, l := range libs {
		items = append(items, libraryDTO(l))
	}
	writeJSON(w, 200, map[string]any{"Items": items, "TotalRecordCount": len(items), "StartIndex": 0})
}

func (h *Handler) userViews(w http.ResponseWriter, r *http.Request) { h.mediaFolders(w, r) }

func libraryDTO(l model.Library) map[string]any {
	ct := "tvshows"
	if l.Kind == model.KindMovies {
		ct = "movies"
	}
	return map[string]any{
		"Name":           l.Name,
		"ServerId":       ServerID,
		"Id":             libraryID(l.ID),
		"Type":           "CollectionFolder",
		"CollectionType": ct,
		"IsFolder":       true,
		"LocationType":   "FileSystem",
		"Path":           l.Root,
	}
}

// Jellyfin ids are 32-hex strings; libraries are small ints, so pad them.
func libraryID(id int64) string { return fmt.Sprintf("%032x", id) }

func parseLibraryID(s string) (int64, bool) {
	if len(s) != 32 {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimLeft(s, "0"), 16, 64)
	if err != nil {
		if strings.Trim(s, "0") == "" {
			return 0, true
		}
		return 0, false
	}
	return n, true
}

// ---- items ----

func (h *Handler) items(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ctx := r.Context()

	// request app detail fetch: /Items?ids=a,b&fields=...
	if ids := q.Get("ids"); ids != "" || q.Get("Ids") != "" {
		if ids == "" {
			ids = q.Get("Ids")
		}
		var out []map[string]any
		for _, id := range strings.Split(ids, ",") {
			it, err := h.store.GetItem(ctx, strings.TrimSpace(id))
			if err != nil || it == nil {
				continue
			}
			out = append(out, h.itemDTO(ctx, it, true))
		}
		if out == nil {
			out = []map[string]any{}
		}
		writeJSON(w, 200, map[string]any{"Items": out, "TotalRecordCount": len(out), "StartIndex": 0})
		return
	}

	sq := store.ItemQuery{Recursive: boolParam(q, "Recursive")}
	parent := first(q, "ParentId", "parentId")
	if parent != "" {
		if libID, ok := parseLibraryID(parent); ok && libID != 0 {
			sq.LibraryID = libID
		} else {
			sq.ParentID = parent
		}
	}
	for _, t := range strings.Split(first(q, "IncludeItemTypes", "includeItemTypes"), ",") {
		switch strings.TrimSpace(strings.ToLower(t)) {
		case "movie":
			sq.Types = append(sq.Types, model.TypeMovie)
		case "series":
			sq.Types = append(sq.Types, model.TypeSeries)
		case "season":
			sq.Types = append(sq.Types, model.TypeSeason)
		case "episode":
			sq.Types = append(sq.Types, model.TypeEpisode)
		}
	}
	switch strings.ToLower(strings.SplitN(first(q, "SortBy", "sortBy"), ",", 2)[0]) {
	case "sortname", "name":
		sq.SortBy = "title"
	case "datecreated":
		sq.SortBy = "date_created"
	case "indexnumber", "parentindexnumber":
		sq.SortBy = "index"
	default:
		sq.SortBy = "date_created"
		sq.Desc = true
	}
	if so := strings.ToLower(first(q, "SortOrder", "sortOrder")); so == "descending" {
		sq.Desc = true
	} else if so == "ascending" {
		sq.Desc = false
	}
	sq.Offset, _ = strconv.Atoi(first(q, "StartIndex", "startIndex"))
	sq.Limit, _ = strconv.Atoi(first(q, "Limit", "limit"))
	if sq.Limit <= 0 {
		sq.Limit = 1000
	}

	items, total, err := h.store.ListItems(ctx, sq)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	wantSources := strings.Contains(strings.ToLower(first(q, "Fields", "fields")), "mediasources")
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, h.itemDTO(ctx, &items[i], wantSources))
	}
	writeJSON(w, 200, map[string]any{"Items": out, "TotalRecordCount": total, "StartIndex": sq.Offset})
}

func (h *Handler) latest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sq := store.ItemQuery{SortBy: "date_created", Desc: true, Types: []model.ItemType{model.TypeEpisode, model.TypeMovie}}
	if parent := first(q, "ParentId", "parentId"); parent != "" {
		if libID, ok := parseLibraryID(parent); ok {
			sq.LibraryID = libID
		}
	}
	sq.Limit, _ = strconv.Atoi(first(q, "Limit", "limit"))
	if sq.Limit <= 0 {
		sq.Limit = 20
	}
	items, _, err := h.store.ListItems(r.Context(), sq)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, h.itemDTO(r.Context(), &items[i], true))
	}
	writeJSON(w, 200, out)
}

func (h *Handler) item(w http.ResponseWriter, r *http.Request) {
	it, err := h.store.GetItem(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if it == nil {
		writeError(w, 404, "Item not found")
		return
	}
	writeJSON(w, 200, h.itemDTO(r.Context(), it, true))
}

func (h *Handler) seasons(w http.ResponseWriter, r *http.Request) {
	items, total, err := h.store.ListItems(r.Context(), store.ItemQuery{ParentID: chi.URLParam(r, "id"), Types: []model.ItemType{model.TypeSeason}, SortBy: "index"})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, h.itemDTO(r.Context(), &items[i], false))
	}
	writeJSON(w, 200, map[string]any{"Items": out, "TotalRecordCount": total, "StartIndex": 0})
}

func (h *Handler) episodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sq := store.ItemQuery{Types: []model.ItemType{model.TypeEpisode}, SortBy: "index"}
	if season := first(q, "SeasonId", "seasonId"); season != "" {
		sq.ParentID = season
	} else {
		sq.ParentID = chi.URLParam(r, "id")
		sq.Recursive = true
	}
	items, total, err := h.store.ListItems(r.Context(), sq)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	wantSources := strings.Contains(strings.ToLower(first(q, "Fields", "fields")), "mediasources")
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, h.itemDTO(r.Context(), &items[i], wantSources))
	}
	writeJSON(w, 200, map[string]any{"Items": out, "TotalRecordCount": total, "StartIndex": 0})
}

func (h *Handler) emptyList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"Items": []any{}, "TotalRecordCount": 0, "StartIndex": 0})
}

// ---- helpers ----

func first(q map[string][]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := q[k]; ok && len(v) > 0 && v[0] != "" {
			return v[0]
		}
	}
	return ""
}

func boolParam(q map[string][]string, key string) bool {
	v := strings.ToLower(first(q, key, strings.ToLower(key[:1])+key[1:]))
	return v == "true" || v == "1"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"Message": msg})
}

func jfTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.0000000Z")
}

func ticks(ms int64) int64 { return ms * 10_000 }
