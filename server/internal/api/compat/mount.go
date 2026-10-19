package compat

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/store"
)

type Handler struct {
	cfg   *config.Config
	store *store.Store
	auth  *auth.Service
	lib   *library.Library
	// Stream, attachment and subtitle handlers are shared with the native API.
	Stream     http.HandlerFunc
	Attachment http.HandlerFunc
	Subtitle   http.HandlerFunc
}

func New(cfg *config.Config, st *store.Store, a *auth.Service, lib *library.Library) *Handler {
	return &Handler{cfg: cfg, store: st, auth: a, lib: lib}
}

// Mount registers the compat routes on the root router. Public routes are the ones a
// client needs before it has a token; everything else requires a device token or API key.
func (h *Handler) Mount(r chi.Router) {
	ping := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"Roxy Server"`))
	}

	// ---- public ----
	r.Group(func(r chi.Router) {
		r.Use(h.auth.Middleware(false))
		r.Get("/System/Info/Public", h.systemInfoPublic)
		r.Get("/System/Ping", ping)
		r.Post("/System/Ping", ping)
		r.Get("/Branding/Configuration", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, map[string]any{"LoginDisclaimer": "", "CustomCss": "", "SplashscreenEnabled": false})
		})
		r.Get("/Users/Public", h.publicUsers)
		r.Post("/Users/AuthenticateByName", h.authenticateByName)
		r.Get("/QuickConnect/Enabled", h.quickConnectEnabled)
		r.Post("/QuickConnect/Initiate", func(w http.ResponseWriter, _ *http.Request) { writeError(w, 401, "Quick Connect is not enabled") })
		r.Get("/QuickConnect/Connect", func(w http.ResponseWriter, _ *http.Request) { writeError(w, 401, "Quick Connect is not enabled") })
		r.Post("/Users/AuthenticateWithQuickConnect", func(w http.ResponseWriter, _ *http.Request) { writeError(w, 401, "Quick Connect is not enabled") })

		// Media bytes: the token travels in the URL (api_key) because players cannot set
		// headers reliably; the middleware already resolves it, so just require it here.
		r.Group(func(r chi.Router) {
			r.Use(requirePrincipal)
			r.Get("/Videos/{id}/stream", h.stream)
			r.Get("/Videos/{id}/stream.{ext}", h.stream)
			r.Head("/Videos/{id}/stream", h.stream)
			r.Head("/Videos/{id}/stream.{ext}", h.stream)
			r.Get("/Videos/{id}/{sourceId}/Attachments/{index}", h.attachment)
			r.Get("/Videos/{id}/{sourceId}/Subtitles/{index}/Stream.{ext}", h.subtitle)
			r.Get("/Videos/{id}/{sourceId}/Subtitles/{index}/0/Stream.{ext}", h.subtitle)
		})
	})

	// ---- authenticated ----
	r.Group(func(r chi.Router) {
		r.Use(h.auth.Middleware(true))

		r.Get("/System/Info", h.systemInfo)
		r.Get("/System/Configuration", h.systemConfiguration)
		r.Get("/Plugins", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, []any{}) })

		r.Get("/Users", h.users)
		r.Get("/Users/Me", h.user)
		r.Get("/Users/{userId}", h.user)
		r.Get("/Users/{userId}/Views", h.userViews)
		r.Get("/UserViews", h.userViews)
		r.Post("/Auth/Keys", h.createAPIKey)
		r.Get("/Auth/Keys", h.listAPIKeys)
		r.Delete("/Auth/Keys/{key}", h.deleteAPIKey)

		r.Get("/Library/MediaFolders", h.mediaFolders)
		r.Get("/Library/VirtualFolders", h.mediaFolders)
		r.Post("/Library/Media/Updated", h.mediaUpdated)
		r.Post("/Library/Refresh", h.libraryRefresh)
		r.Post("/Notifications/Admin", h.adminNotification)

		r.Get("/Items", h.items)
		r.Get("/Users/{userId}/Items", h.items)
		r.Get("/Items/Latest", h.latest)
		r.Get("/Users/{userId}/Items/Latest", h.latest)
		r.Get("/Items/{id}", h.item)
		r.Get("/Users/{userId}/Items/{id}", h.item)
		r.Get("/Shows/{id}/Seasons", h.seasons)
		r.Get("/Shows/{id}/Episodes", h.episodes)
		r.Get("/Shows/NextUp", h.emptyList)
		r.Get("/Users/{userId}/Items/Resume", h.resumeItems)
		r.Get("/Items/{id}/Images/{type}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
		r.Get("/Items/{id}/Images/{type}/{index}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })

		r.Post("/Items/{id}/PlaybackInfo", h.playbackInfo)
		r.Get("/Items/{id}/PlaybackInfo", h.playbackInfo)

		// Session reporting -> persisted watch state (user_item_data keyed by item GUID).
		r.Post("/Sessions/Playing", h.playingStart)
		r.Post("/Sessions/Playing/Progress", h.playingProgress)
		r.Post("/Sessions/Playing/Stopped", h.playingStopped)
		r.Post("/Sessions/Playing/Ping", h.noContent)
		r.Post("/Users/{userId}/PlayedItems/{id}", h.playedItem)
		r.Delete("/Users/{userId}/PlayedItems/{id}", h.playedItem)
		r.Post("/UserPlayedItems/{id}", h.playedItem)
		r.Delete("/UserPlayedItems/{id}", h.playedItem)
		r.Post("/Users/{userId}/FavoriteItems/{id}", h.favoriteItem)
		r.Delete("/Users/{userId}/FavoriteItems/{id}", h.favoriteItem)
		r.Post("/UserFavoriteItems/{id}", h.favoriteItem)
		r.Delete("/UserFavoriteItems/{id}", h.favoriteItem)
		r.Get("/UserItems/Resume", h.resumeItems)
		r.Post("/Sessions/Capabilities", h.noContent)
		r.Post("/Sessions/Capabilities/Full", h.noContent)
		r.Post("/Sessions/Logout", h.noContent)
		r.Get("/Sessions", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, []any{}) })
		r.Get("/DisplayPreferences/{id}", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, map[string]any{"Id": "usersettings", "SortBy": "SortName", "RememberIndexing": false, "PrimaryImageHeight": 250, "PrimaryImageWidth": 250, "CustomPrefs": map[string]any{}, "ScrollDirection": "Horizontal", "ShowBackdrop": true, "RememberSorting": false, "SortOrder": "Ascending", "ShowSidebar": false, "Client": "emby"})
		})
		r.Post("/DisplayPreferences/{id}", h.noContent)
	})
}

func requirePrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.PrincipalFrom(r.Context()) == nil {
			writeError(w, 401, "Unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
