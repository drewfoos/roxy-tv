package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
)

// Native auth for Roxy clients. Tokens are the same opaque device tokens the compat
// surface issues, so one store serves both; clients send `Authorization: Bearer <token>`
// or, for media URLs the player fetches itself, `?api_key=<token>`.

// POST /v1/auth/login {username, password, client, device, deviceId, version}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Client   string `json:"client"`
		Device   string `json:"device"`
		DeviceID string `json:"deviceId"`
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	ci := auth.ClientInfo{Client: body.Client, Device: body.Device, DeviceID: body.DeviceID, Version: body.Version, Address: auth.RemoteIP(r)}
	if ci.Client == "" {
		ci.Client = "Roxy"
	}
	u, d, token, err := s.authSvc.Login(r.Context(), body.Username, body.Password, ci)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		slog.Warn("login failed", "user", body.Username, "from", ci.Address)
		writeError(w, 401, "invalid username or password")
		return
	}
	var retry *auth.RetryError
	if errors.As(err, &retry) {
		secs := int(retry.After.Seconds()) + 1
		slog.Warn("login throttled", "user", body.Username, "from", ci.Address, "wait_s", secs)
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Too many wrong passwords. Try again in %s.", humanWait(secs)))
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	slog.Info("login", "user", u.Name, "client", ci.Client, "device", ci.Device)
	writeJSON(w, 200, map[string]any{
		"token":    token,
		"user":     userJSON(u),
		"deviceId": d.ID,
	})
}

// GET /v1/auth/me
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if p == nil {
		writeError(w, 401, "unauthorized")
		return
	}
	if p.IsAPIKey {
		writeJSON(w, 200, map[string]any{"apiKey": true, "isAdmin": true})
		return
	}
	out := userJSON(p.User)
	out["profileId"] = p.ScopeID // the profile this request's watch state is keyed by
	writeJSON(w, 200, out)
}

// POST /v1/auth/logout revokes the calling device token.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if p != nil && p.DeviceID != "" {
		_ = s.store.DeleteDevice(r.Context(), p.DeviceID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func userJSON(u *model.User) map[string]any {
	return map[string]any{
		"id":      u.ID,
		"name":    u.Name,
		"isAdmin": u.IsAdmin,
	}
}
