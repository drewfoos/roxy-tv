// Package api exposes Roxy's HTTP surfaces: the native /v1 API and, later, the
// Jellyfin-compatible subset (docs/api/jellyfin-compat.md).
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/roxy-media/roxy/server/internal/api/compat"
	"github.com/roxy-media/roxy/server/internal/arrhook"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/discover"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/segments"
	"github.com/roxy-media/roxy/server/internal/store"
	"github.com/roxy-media/roxy/server/internal/trickplay"
)

type Server struct {
	cfg      *config.Config
	store    *store.Store
	lib      *library.Library
	ff       *ffmpeg.FFmpeg
	authSvc  *auth.Service
	meta     *metadata.Service // may be nil (scan-only runs)
	trick    *trickplay.Service
	segs     *segments.Service // may be nil (scan-only runs)
	discover *discover.Service // nil without TMDB
	hooks    *arrhook.Service
	start    time.Time
}

func NewRouter(cfg *config.Config, st *store.Store, lib *library.Library, ff *ffmpeg.FFmpeg, authSvc *auth.Service, meta *metadata.Service, trick *trickplay.Service, segs *segments.Service) http.Handler {
	s := &Server{cfg: cfg, store: st, lib: lib, ff: ff, authSvc: authSvc, meta: meta, trick: trick, segs: segs, start: time.Now()}
	s.hooks = arrhook.New(st, lib)
	if meta != nil && meta.TMDB() != nil {
		s.discover = discover.New(st, cfg, meta.TMDB())
		s.discover.Start(context.Background())
	}

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(cors)
	r.Use(newRateLimiter(20, 60).middleware)

	r.Get("/healthz", s.healthz)

	// Native API: what Roxy's own clients speak (docs/api/native.md).
	r.Route("/v1", func(r chi.Router) {
		r.Get("/system/info", s.systemInfo)
		r.Post("/auth/login", s.login)
		r.Get("/legal/terms", s.legalDoc)
		r.Get("/legal/privacy", s.legalDoc)

		r.Group(func(r chi.Router) {
			r.Use(authSvc.Middleware(true)) // Bearer token, or ?api_key= for player-fetched media
			r.Get("/auth/me", s.me)
			r.Get("/legal/status", s.legalStatus)
			r.Post("/legal/accept", s.legalAccept)
			r.Post("/auth/logout", s.logout)
			r.Get("/libraries", s.listLibraries)
			r.Post("/libraries/scan", s.scanAll)
			r.Post("/segments/detect", s.detectSegments)
			r.Get("/discover", s.discoverRows)
			r.Get("/discover/search", s.discoverSearch)
			r.Get("/discover/requests", s.discoverRequests)
			r.Delete("/discover/requests/{id}", s.discoverDeleteRequest)
			r.Post("/discover/request", s.discoverRequest)
			r.Get("/discover/browse", s.discoverBrowse)
			r.Get("/discover/genres", s.discoverGenres)
			r.Post("/discover/requests/retry", s.discoverRetry)
			r.Get("/discover/tv/{tmdbId}/season/{n}", s.discoverEpisodes)
			r.Get("/discover/{type}/{tmdbId}", s.discoverDetail)
			r.Post("/webhooks/{source}", s.webhook)
			r.Post("/ingest/{source}", s.webhook)
			r.Get("/webhooks/events", s.webhookEvents)
			r.Get("/auth/keys", s.listKeys)
			r.Post("/auth/keys", s.createKey)
			r.Delete("/auth/keys/{id}", s.deleteKey)
			r.Get("/settings/arr", s.arrSettingsGet)
			r.Put("/settings/arr", s.arrSettingsPut)
			r.Post("/settings/arr/test", s.arrSettingsTest)
			r.Get("/apps/{app}/latest", s.appLatest)
			r.Get("/apps/{app}/download", s.appDownload)
			r.Get("/items", s.listItems)
			r.Get("/items/{id}", s.getItem)
			r.Get("/items/{id}/images/{type}", s.getItemImage)
			r.Post("/items/{id}/refresh", s.refreshItem)
			r.Get("/items/{id}/related", s.related)
			r.Get("/items/{id}/details", s.itemDetails)
			r.Get("/home", s.home)
			r.Get("/me/resume", s.resume)
			r.Post("/me/progress", s.progress)
			r.Post("/me/played/{itemId}", s.played)
			r.Delete("/me/played/{itemId}", s.played)
			r.Post("/me/favorite/{itemId}", s.favorite)
			r.Delete("/me/favorite/{itemId}", s.favorite)
			r.Get("/me/profiles", s.listProfiles)
			r.Post("/me/profiles", s.createProfile)
			r.Patch("/me/profiles/{id}", s.updateProfile)
			r.Delete("/me/profiles/{id}", s.deleteProfile)
			r.Get("/me/profiles/{id}/avatar", s.profileAvatar)
			r.Get("/avatars/search", s.avatarSearch)
			r.Get("/avatars/suggested", s.avatarSuggested)
			r.Get("/sources/{id}", s.getSource)
			r.Get("/sources/{id}/attachments/{index}", s.getAttachment)
			r.Get("/sources/{id}/subtitles/{index}", s.getSubtitle)
			r.Get("/sources/{id}/trickplay", s.trickplayManifest)
			r.Get("/sources/{id}/trickplay/{n}", s.trickplaySheet)
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
