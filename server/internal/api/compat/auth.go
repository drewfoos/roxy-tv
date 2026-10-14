package compat

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
)

// POST /Users/AuthenticateByName {Username, Pw}
func (h *Handler) authenticateByName(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"Username"`
		Pw       string `json:"Pw"`
		Password string `json:"Password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if body.Pw == "" {
		body.Pw = body.Password
	}
	_, ci := auth.TokenFromRequest(r)
	if ci.Client == "" {
		ci.Client = "Unknown"
	}
	u, d, token, err := h.auth.Login(r.Context(), body.Username, body.Pw, ci)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, 401, "Invalid username or password")
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	slog.Info("login", "user", u.Name, "client", ci.Client, "device", ci.Device)
	writeJSON(w, 200, map[string]any{
		"User":        userDTO(u),
		"SessionInfo": sessionDTO(u, d),
		"AccessToken": token,
		"ServerId":    ServerID,
	})
}

func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.ListUsers(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, userDTO(&list[i]))
	}
	writeJSON(w, 200, out)
}

func (h *Handler) publicUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, []any{})
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userId")
	p := auth.PrincipalFrom(r.Context())
	if id == "" || strings.EqualFold(id, "Me") {
		if p == nil || p.User == nil {
			writeError(w, 401, "Unauthorized")
			return
		}
		writeJSON(w, 200, userDTO(p.User))
		return
	}
	u, err := h.store.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if u == nil {
		writeError(w, 404, "User not found")
		return
	}
	writeJSON(w, 200, userDTO(u))
}

// POST /Auth/Keys?App=request app  -> 204
func (h *Handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("App")
	if name == "" {
		name = r.URL.Query().Get("app")
	}
	if name == "" {
		writeError(w, 400, "App is required")
		return
	}
	if _, err := h.auth.CreateAPIKey(r.Context(), name); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /Auth/Keys -> {Items:[{AccessToken, AppName, DateCreated}]}
//
// Jellyfin returns the raw key here (it stores keys in clear). Roxy stores hashes, so the
// plain token is only available in memory for keys created during this process
// lifetime; older keys are listed without it. request app reads the key right after creating it.
func (h *Handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.store.ListAPIKeys(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		items = append(items, map[string]any{
			"Id":          k.ID,
			"AccessToken": h.auth.PlainKey(k.TokenHash),
			"AppName":     k.Name,
			"DateCreated": jfTime(k.CreatedAt),
			"UserId":      "",
			"DeviceId":    "",
			"IsActive":    true,
		})
	}
	writeJSON(w, 200, map[string]any{"Items": items, "TotalRecordCount": len(items), "StartIndex": 0})
}

func (h *Handler) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	_ = h.store.DeleteAPIKeyByHash(r.Context(), auth.HashToken(key))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) quickConnectEnabled(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, false) // device-code login arrives with the auth phase proper
}

func (h *Handler) noContent(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// ---- DTOs ----

func userDTO(u *model.User) map[string]any {
	return map[string]any{
		"Name":                      u.Name,
		"ServerId":                  ServerID,
		"Id":                        u.ID,
		"HasPassword":               u.PasswordHash != "",
		"HasConfiguredPassword":     u.PasswordHash != "",
		"HasConfiguredEasyPassword": false,
		"EnableAutoLogin":           false,
		"LastLoginDate":             jfTime(time.Now()),
		"LastActivityDate":          jfTime(time.Now()),
		"Configuration": map[string]any{
			"PlayDefaultAudioTrack":      true,
			"SubtitleLanguagePreference": "",
			"DisplayMissingEpisodes":     false,
			"GroupedFolders":             []any{},
			"SubtitleMode":               "Default",
			"DisplayCollectionsView":     false,
			"EnableLocalPassword":        false,
			"OrderedViews":               []any{},
			"LatestItemsExcludes":        []any{},
			"MyMediaExcludes":            []any{},
			"HidePlayedInLatest":         true,
			"RememberAudioSelections":    true,
			"RememberSubtitleSelections": true,
			"EnableNextEpisodeAutoPlay":  true,
			"CastReceiverId":             "",
		},
		"Policy": map[string]any{
			"IsAdministrator":                  u.IsAdmin,
			"IsHidden":                         false,
			"IsDisabled":                       u.IsDisabled,
			"BlockedTags":                      []any{},
			"AllowedTags":                      []any{},
			"EnableUserPreferenceAccess":       true,
			"AccessSchedules":                  []any{},
			"BlockUnratedItems":                []any{},
			"EnableRemoteControlOfOtherUsers":  u.IsAdmin,
			"EnableSharedDeviceControl":        true,
			"EnableRemoteAccess":               true,
			"EnableLiveTvManagement":           false,
			"EnableLiveTvAccess":               false,
			"EnableMediaPlayback":              true,
			"EnableAudioPlaybackTranscoding":   true,
			"EnableVideoPlaybackTranscoding":   true,
			"EnablePlaybackRemuxing":           true,
			"ForceRemoteSourceTranscoding":     false,
			"EnableContentDeletion":            u.IsAdmin,
			"EnableContentDeletionFromFolders": []any{},
			"EnableContentDownloading":         true,
			"EnableSyncTranscoding":            true,
			"EnableMediaConversion":            false,
			"EnabledDevices":                   []any{},
			"EnableAllDevices":                 true,
			"EnabledChannels":                  []any{},
			"EnableAllChannels":                true,
			"EnabledFolders":                   []any{},
			"EnableAllFolders":                 true,
			"InvalidLoginAttemptCount":         0,
			"LoginAttemptsBeforeLockout":       -1,
			"MaxActiveSessions":                0,
			"EnablePublicSharing":              false,
			"BlockedMediaFolders":              []any{},
			"BlockedChannels":                  []any{},
			"RemoteClientBitrateLimit":         0,
			"AuthenticationProviderId":         "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
			"PasswordResetProviderId":          "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
			"SyncPlayAccess":                   "CreateAndJoinGroups",
			"EnableCollectionManagement":       u.IsAdmin,
			"EnableSubtitleManagement":         u.IsAdmin,
			"EnableLyricManagement":            u.IsAdmin,
		},
	}
}

func sessionDTO(u *model.User, d *model.Device) map[string]any {
	return map[string]any{
		"Id":                    d.ID,
		"UserId":                u.ID,
		"UserName":              u.Name,
		"Client":                d.Client,
		"DeviceName":            d.Name,
		"DeviceId":              d.DeviceID,
		"ApplicationVersion":    d.ClientVersion,
		"ServerId":              ServerID,
		"LastActivityDate":      jfTime(time.Now()),
		"LastPlaybackCheckIn":   jfTime(time.Now()),
		"IsActive":              true,
		"SupportsMediaControl":  false,
		"SupportsRemoteControl": false,
		"HasCustomDeviceName":   false,
		"PlayableMediaTypes":    []string{"Video"},
		"AdditionalUsers":       []any{},
		"NowPlayingQueue":       []any{},
		"NowPlayingQueueFullItems": []any{},
		"PlayState":             map[string]any{"CanSeek": false, "IsPaused": false, "IsMuted": false, "RepeatMode": "RepeatNone", "PlaybackOrder": "Default"},
		"Capabilities":          map[string]any{"PlayableMediaTypes": []string{"Video"}, "SupportedCommands": []any{}, "SupportsMediaControl": false, "SupportsPersistentIdentifier": true},
	}
}
