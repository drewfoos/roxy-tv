// Package api exposes Roxy's HTTP surfaces: the native /v1 API and, later, the
// Jellyfin-compatible subset (docs/api/jellyfin-compat.md).
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/roxy-media/roxy/server/internal/api/compat"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/store"
)

type Server struct {
	cfg     *config.Config
	store   *store.Store
	lib     *library.Library
	ff      *ffmpeg.FFmpeg
	authSvc *auth.Service
	meta    *metadata.Service // may be nil (scan-only runs)
	start   time.Time
}

func NewRouter(cfg *config.Config, st *store.Store, lib *library.Library, ff *ffmpeg.FFmpeg, authSvc *auth.Service, meta *metadata.Service) http.Handler {
	s := &Server{cfg: cfg, store: st, lib: lib, ff: ff, authSvc: authSvc, meta: meta, start: time.Now()}

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(cors)

	r.Get("/healthz", s.healthz)

	// Native API: what Roxy's own clients speak (docs/api/native.md).
	r.Route("/v1", func(r chi.Router) {
		r.Get("/system/info", s.systemInfo)
		r.Post("/auth/login", s.login)

		r.Group(func(r chi.Router) {
			r.Use(authSvc.Middleware(true)) // Bearer token, or ?api_key= for player-fetched media
			r.Get("/auth/me", s.me)
			r.Post("/auth/logout", s.logout)
			r.Get("/libraries", s.listLibraries)
			r.Post("/libraries/scan", s.scanAll)
			r.Get("/items", s.listItems)
			r.Get("/items/{id}", s.getItem)
			r.Get("/items/{id}/images/{type}", s.getItemImage)
			r.Post("/items/{id}/refresh", s.refreshItem)
			r.Get("/items/{id}/related", s.related)
			r.Get("/home", s.home)
			r.Get("/me/resume", s.resume)
			r.Post("/me/progress", s.progress)
			r.Post("/me/played/{itemId}", s.played)
			r.Delete("/me/played/{itemId}", s.played)
			r.Post("/me/favorite/{itemId}", s.favorite)
			r.Delete("/me/favorite/{itemId}", s.favorite)
			r.Get("/sources/{id}", s.getSource)
			r.Get("/sources/{id}/attachments/{index}", s.getAttachment)
			r.Get("/sources/{id}/subtitles/{index}", s.getSubtitle)
			r.Get("/stream/{id}", s.stream)
			r.Head("/stream/{id}", s.stream)
		})
	})

	// Dev convenience for the player spike, unauthenticated: /stream/<source id or file
	// name stem>. Remove before anything resembling a release.
	r.Get("/stream/{id}", s.stream)

	// Jellyfin-compatible subset (docs/api/jellyfin-compat.md).
	c := compat.New(cfg, st, authSvc, lib)
	c.Stream, c.Attachment, c.Subtitle = s.stream, s.getAttachment, s.getSubtitle
	c.Mount(r)

	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "uptime": time.Since(s.start).String()})
}

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    "Roxy Server",
		"version": "0.1.0-dev",
		"ffmpeg":  s.ff.Version,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range, X-MediaBrowser-Token, X-Emby-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Range, Accept-Ranges, Content-Length")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "bytes", ww.BytesWritten(), "took", time.Since(start).Round(time.Microsecond), "range", r.Header.Get("Range"))
	})
}
