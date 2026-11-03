package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
)

// First-time setup (Jellyfin's wizard). A new server has no accounts; the first person
// to open the app on the home network creates the admin. After that this door is shut:
// every other step (TMDB key, media folders, Sonarr / Radarr) uses the normal admin API.
// ROXY_ADMIN_PASSWORD skips it (the admin is created at start, as before).

var setupMu sync.Mutex

// GET /v1/setup -> {required, allowed, tmdb, serverName}. Public: the app asks before sign-in.
func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"required":   n == 0,
		"allowed":    n == 0 && fromHomeNetwork(r),
		"tmdb":       s.meta != nil && s.meta.TMDB() != nil,
		"serverName": s.store.ServerName(r.Context()),
	})
}

// POST /v1/setup/admin {name, password, device, deviceId, version, client?} -> sign-in response.
// Only while no account exists, and only from the home network.
func (s *Server) setupAdmin(w http.ResponseWriter, r *http.Request) {
	if !fromHomeNetwork(r) {
		writeError(w, 403, "setup only works from the server's home network")
		return
	}
	var b struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Device   string `json:"device"`
		DeviceID string `json:"deviceId"`
		Version  string `json:"version"`
		Client   string `json:"client"` // "roxy-web" from the browser; the apps leave it out
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	name := strings.TrimSpace(b.Name)
	if err := s.checkUserName(r, name, ""); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := auth.ValidateNewPassword(b.Password); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	setupMu.Lock()
	defer setupMu.Unlock()
	if n, err := s.store.CountUsers(r.Context()); err != nil || n > 0 {
		writeError(w, 409, "this server is already set up; sign in instead")
		return
	}
	hash, err := auth.HashPassword(b.Password)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	u := &model.User{ID: uuid.NewString(), Name: name, PasswordHash: hash, Role: model.RoleAdmin, IsAdmin: true}
	if err := s.store.CreateUser(r.Context(), u); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	slog.Warn("setup: admin account created", "name", u.Name, "from", peerIP(r))
	client := strings.TrimSpace(b.Client)
	if client == "" || len(client) > 40 {
		client = "Roxy"
	}
	ci := auth.ClientInfo{Client: client, Device: b.Device, DeviceID: b.DeviceID, Version: b.Version, Address: auth.RemoteIP(r)}
	u2, d, token, err := s.authSvc.Login(r.Context(), name, b.Password, ci)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"token": token, "user": userJSON(u2), "deviceId": d.ID})
}

// GET /v1/settings/tmdb (admin) -> {set, source}. The key itself never leaves the server.
func (s *Server) tmdbSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	set := s.meta != nil && s.meta.TMDB() != nil
	source := ""
	if set {
		var saved string
		if ok, _ := s.store.GetSetting(r.Context(), "tmdb_api_key", &saved); ok && saved != "" {
			source = "saved"
		} else {
			source = "server" // ROXY_TMDB_API_KEY, tmdb.key or the build's own
		}
	}
	writeJSON(w, 200, map[string]any{"set": set, "source": source})
}

// PUT /v1/settings/tmdb {key} (admin) -> {set}: checks the key with TMDB, then saves it.
// An empty key removes a saved one.
func (s *Server) tmdbSettingsPut(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || s.meta == nil {
		return
	}
	var b struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	key := strings.TrimSpace(b.Key)
	t := s.meta.TMDBProvider()
	if key != "" {
		if err := checkTMDBKey(r.Context(), key); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	if err := s.store.SetSetting(r.Context(), "tmdb_api_key", key); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if key != "" || s.cfg.TMDBAPIKey == "" {
		t.SetKey(key)
	} else {
		t.SetKey(s.cfg.TMDBAPIKey) // back to the server's own key
	}
	slog.Info("tmdb key updated", "set", t.HasKey())
	writeJSON(w, 200, map[string]any{"set": t.HasKey()})
}

// checkTMDBKey asks TMDB whether the key works (v3 key or v4 read token).
func checkTMDBKey(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.themoviedb.org/3/configuration", nil)
	q := req.URL.Query()
	if strings.HasPrefix(key, "eyJ") {
		req.Header.Set("Authorization", "Bearer "+key)
	} else {
		q.Set("api_key", key)
	}
	req.URL.RawQuery = q.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("couldn't reach TMDB; check the server's internet connection")
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("TMDB says that key isn't valid; copy the API Key (v3) or the Read Access Token")
	default:
		return errors.New("TMDB answered " + resp.Status)
	}
}

// loadSavedTMDBKey: a key entered in setup / settings wins over none at start.
func (s *Server) loadSavedTMDBKey(ctx context.Context) {
	if s.meta == nil {
		return
	}
	var saved string
	if ok, _ := s.store.GetSetting(ctx, "tmdb_api_key", &saved); ok && strings.TrimSpace(saved) != "" {
		s.meta.TMDBProvider().SetKey(saved)
	}
}
