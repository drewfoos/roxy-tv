// Package api exposes Roxy's HTTP surface: the native /v1 API (docs/api/native.md).
// The Jellyfin-compatible API was removed on 2026-10-26: Roxy's clients, Discover and
// the Sonarr / Radarr webhooks all use /v1.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/roxy-media/roxy/server/internal/arrhook"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/discover"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/segments"
	"github.com/roxy-media/roxy/server/internal/sessions"
	"github.com/roxy-media/roxy/server/internal/store"
	"github.com/roxy-media/roxy/server/internal/transcode"
	"github.com/roxy-media/roxy/server/internal/trickplay"
	"github.com/roxy-media/roxy/server/internal/web"
)

type Server struct {
	cfg        *config.Config
	store      *store.Store
	lib        *library.Library
	ff         *ffmpeg.FFmpeg
	authSvc    *auth.Service
	show       showcase          // the sign-in page's poster wall (showcase.go)
	meta       *metadata.Service // may be nil (scan-only runs)
	trick      *trickplay.Service
	segs       *segments.Service // may be nil (scan-only runs)
	discover   *discover.Service // nil without TMDB
	hooks      *arrhook.Service
	transcoder *transcode.Detector
	hub        *sessions.Hub // "Play on": connected players and their command queues
	links      *linkStore    // TV sign-in codes waiting for a phone's approval
	start      time.Time
}

func NewRouter(cfg *config.Config, st *store.Store, lib *library.Library, ff *ffmpeg.FFmpeg, authSvc *auth.Service, meta *metadata.Service, trick *trickplay.Service, segs *segments.Service) http.Handler {
	s := &Server{cfg: cfg, store: st, lib: lib, ff: ff, authSvc: authSvc, meta: meta, trick: trick, segs: segs, start: time.Now()}
	s.hooks = arrhook.New(st, lib)
	s.hub = sessions.New()
	s.links = newLinkStore()
	s.transcoder = transcode.NewDetector(ff.FFmpeg, cfg.HWAccel, cfg.VAAPIDevice)
	go func() { // probe the GPU now, not on the first play; a saved choice beats ROXY_HWACCEL
		s.loadTranscodingSetting()
		s.transcoder.Encoder()
	}()
	s.loadSavedTMDBKey(context.Background())
	// Discover is set up even before a TMDB key exists; it switches on when one is entered.
	if meta != nil {
		s.discover = discover.New(st, cfg, meta.TMDBProvider())
		meta.UseEpisodeAirDates(s.discover.EpisodeAirDates) // specials' dates in Sonarr's numbering
		s.discover.SetImportHook(s.hooks.IngestArrFiles)
		s.discover.Start(context.Background())
		s.startCharacterWorker(context.Background())
	}

	r := chi.NewRouter()
	r.Use(trustedRealIP(parseNets(cfg.TrustedProxies)))
	r.Use(limitBodies)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(cors)
	r.Use(securityHeaders)
	r.Use(newRateLimiter(20, 60).middleware)

	r.Get("/healthz", s.healthz)

	// Native API: what Roxy's own clients speak (docs/api/native.md).
	r.Route("/v1", func(r chi.Router) {
		r.Get("/system/info", s.systemInfo)
		// The sign-in page's poster wall: posters by position only (showcase.go).
		r.Get("/showcase", s.showcaseCount)
		r.Get("/showcase/{n}", s.showcasePoster)
		r.Post("/auth/login", s.login)
		// Signing a TV in from a phone (link.go): the TV's side is public, the approval needs a user.
		r.Post("/link/start", s.linkStart)
		r.Get("/link/{code}/qr.png", s.linkQR)
		r.Get("/link/{code}/wait", s.linkWait)
		r.Get("/setup", s.setupStatus)
		r.Post("/setup/admin", s.setupAdmin)
		r.Get("/legal/terms", s.legalDoc)
		r.Get("/legal/privacy", s.legalDoc)

		r.Group(func(r chi.Router) {
			r.Use(authSvc.Middleware(true)) // Bearer token, or ?api_key= for player-fetched media
			r.Get("/auth/me", s.me)
			r.Post("/diagnostics/crash", s.clientCrash)
			r.Get("/link/{code}", s.linkInfo)
			r.Post("/link/{code}/approve", s.linkApprove)
			r.Get("/legal/status", s.legalStatus)
			r.Post("/legal/accept", s.legalAccept)
			r.Post("/auth/logout", s.logout)
			r.Post("/auth/cookie", s.sessionCookie)
			r.Post("/me/password", s.changePassword)
			r.Get("/users", s.listUsers)
			r.Post("/users", s.createUser)
			r.Patch("/users/{id}", s.updateUser)
			r.Delete("/users/{id}", s.deleteUser)
			r.Get("/requests", s.adminRequests)
			r.Post("/discover/requests/{id}/approve", s.discoverApprove)
			r.Post("/discover/requests/{id}/decline", s.discoverDecline)
			r.Get("/libraries", s.listLibraries)
			r.Post("/libraries/scan", s.scanAll)
			r.Get("/library/manage", s.libraryManage)
			r.Get("/library/folders", s.listFolders)
			r.Post("/library/folders", s.addFolder)
			r.Patch("/library/folders/{id}", s.updateFolder)
			r.Delete("/library/folders/{id}", s.removeFolder)
			r.Post("/library/folders/{id}/accept-empty", s.acceptEmptyFolder)
			r.Get("/library/browse", s.browseFolders)
			r.Post("/library/scan", s.libraryScan)
			r.Post("/library/refresh", s.libraryRefresh)
			r.Get("/items/{id}/identify", s.identifySearch)
			r.Post("/items/{id}/identify", s.identifyApply)
			r.Put("/items/{id}/metadata", s.editMetadata)
			r.Post("/segments/detect", s.detectSegments)
			r.Get("/discover", s.discoverRows)
			r.Get("/people/{tmdbId}", s.person)
			r.Get("/discover/search", s.discoverSearch)
			r.Get("/discover/requests", s.discoverRequests)
			r.Delete("/discover/requests/{id}", s.discoverDeleteRequest)
			r.Post("/discover/request", s.discoverRequest)
			r.Get("/discover/browse", s.discoverBrowse)
			r.Get("/discover/genres", s.discoverGenres)
			r.Post("/discover/requests/retry", s.discoverRetry)
			r.Post("/discover/requests/{id}/search", s.discoverSearchAgain)
			r.Post("/discover/requests/{id}/replace", s.discoverReplaceRelease)
			r.Get("/discover/tv/{tmdbId}/season/{n}", s.discoverEpisodes)
			r.Get("/discover/{type}/{tmdbId}", s.discoverDetail)
			r.Get("/discover/{type}/{tmdbId}/details", s.discoverMoreDetails)
			r.Post("/webhooks/{source}", s.webhook)
			r.Post("/ingest/{source}", s.webhook)
			r.Get("/webhooks/events", s.webhookEvents)
			r.Get("/auth/keys", s.listKeys)
			r.Post("/auth/keys", s.createKey)
			r.Delete("/auth/keys/{id}", s.deleteKey)
			r.Get("/settings/arr", s.arrSettingsGet)
			r.Put("/settings/arr", s.arrSettingsPut)
			r.Post("/settings/arr/test", s.arrSettingsTest)
			r.Get("/settings/fanart", s.fanartSettingsGet)
			r.Put("/settings/fanart", s.fanartSettingsPut)
			r.Get("/settings/transcoding", s.transcodingSettingsGet)
			r.Get("/settings/tmdb", s.tmdbSettingsGet)
			r.Put("/settings/tmdb", s.tmdbSettingsPut)
			r.Put("/settings/transcoding", s.transcodingSettingsPut)
			r.Get("/items/{id}/images/{type}/candidates", s.imageCandidates)
			r.Get("/apps/{app}/latest", s.appLatest)
			r.Get("/apps/{app}/download", s.appDownload)
			r.Get("/apps/{app}/profile", s.appProfileFile)
			r.Get("/items", s.listItems)
			r.Get("/items/{id}", s.getItem)
			r.Get("/items/{id}/images/{type}", s.getItemImage)
			r.Put("/items/{id}/images/{type}", s.putItemImage)
			r.Post("/items/{id}/refresh", s.refreshItem)
			r.Get("/items/{id}/extras", s.itemExtras)
			r.Get("/items/{id}/sections", s.seriesSections)
			r.Get("/items/{id}/next", s.itemNext)
			r.Get("/items/{id}/related", s.related)
			r.Get("/items/{id}/similar", s.itemSimilar)
			r.Get("/items/{id}/details", s.itemDetails)
			r.Get("/home", s.home)
			r.Get("/me/resume", s.resume)
			r.Get("/me/history", s.history)
			r.Get("/sessions/commands", s.sessionCommands)
			r.Post("/sessions/state", s.sessionState)
			r.Get("/sessions/players", s.sessionPlayers)
			r.Get("/devices", s.listDevices)
			r.Delete("/devices/{id}", s.deleteDevice)
			r.Post("/sessions/players/{device}/command", s.sessionCommand)
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
			r.Get("/sources/{id}/token", s.mediaToken)
			r.Get("/sources/{id}/attachments/{index}", s.getAttachment)
			r.Get("/sources/{id}/subtitles/{index}", s.getSubtitle)
			r.Get("/sources/{id}/trickplay", s.trickplayManifest)
			r.Get("/sources/{id}/trickplay/{n}", s.trickplaySheet)
			r.Get("/stream/{id}", s.stream)
			r.Head("/stream/{id}", s.stream)
			r.Get("/sources/{id}/transcode", s.transcodeStream)
			r.Get("/sources/{id}/remux", s.remuxStream)
			r.Get("/system/transcoding", s.transcodingInfo)
		})
		// An unknown /v1 path is an API error, not the web shell.
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeError(w, http.StatusNotFound, "no such endpoint") })
	})

	// Everything else is Roxy Web (the built client embedded in the binary).
	r.NotFound(web.Handler().ServeHTTP)

	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "uptime": time.Since(s.start).String()})
}

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	// Public: the login screen's network sweep reads this to tell a Roxy server from
	// whatever else answers on the port (Jellyfin shares 8096).
	id, _ := s.store.ServerID(r.Context())
	// Any page may read this (no credentials involved): the web sign-in's "Change server"
	// checks an address is a Roxy server before sending the browser there.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	writeJSON(w, http.StatusOK, map[string]any{
		"product": "roxy",
		"id":      id,
		"name":    s.store.ServerName(r.Context()),
		"version": config.Version,
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
	writeJSON(w, status, map[string]any{"error": cleanError(msg)})
}

// cleanError keeps an error readable and short: an upstream service (TMDB, AniList,
// Sonarr) can answer with a whole HTML page, which is noise to the app and shows more
// of the other side than it should.
func cleanError(msg string) string {
	if i := strings.Index(msg, "<"); i >= 0 && strings.Contains(strings.ToLower(msg[i:]), "html") {
		msg = strings.TrimSpace(strings.TrimRight(msg[:i], ": "))
		if msg == "" {
			msg = "the other service refused the request"
		}
	}
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}

// cors lets other origins read only the public probe endpoints (corsPublic), with no
// credentials. Every other path sends no CORS headers, so a web page elsewhere cannot
// read Roxy's API with a token it has stolen or a cookie the browser holds.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !corsPublic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
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
