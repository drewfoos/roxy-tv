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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if len(body.Username) > 64 || len(body.Device) > 128 || len(body.Client) > 64 || len(body.DeviceID) > 128 || len(body.Version) > 64 {
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
// POST /v1/me/password {current, new} -> 204. Other devices are signed out.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	err := s.authSvc.ChangePassword(r.Context(), p.UserID, p.DeviceID, auth.RemoteIP(r), body.Current, body.New)
	var retry *auth.RetryError
	switch {
	case err == nil:
		s.hub.RemoveUser(p.UserID, p.DeviceID) // the other devices were signed out
		slog.Info("password changed", "user", p.User.Name)
		w.WriteHeader(http.StatusNoContent)
	case errors.As(err, &retry):
		secs := int(retry.After.Seconds()) + 1
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Too many wrong passwords. Try again in %s.", humanWait(secs)))
	case errors.Is(err, auth.ErrWrongPassword):
		slog.Warn("password change: wrong current password", "user", p.User.Name)
		writeError(w, 403, err.Error())
	case errors.Is(err, auth.ErrWeakPassword), errors.Is(err, auth.ErrLongPassword), errors.Is(err, auth.ErrSamePassword):
		writeError(w, 400, err.Error())
	default:
		writeError(w, 500, err.Error())
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if p != nil && p.DeviceID != "" {
		_ = s.store.DeleteDevice(r.Context(), p.DeviceID)
		s.hub.Remove(p.DeviceID)
	}
	auth.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/auth/cookie: sets the same-site cookie carrying the caller's device token,
// so a browser can load posters and video without a header (Roxy Web). Only a device
// token qualifies: API keys and media tokens stay out of cookies.
func (s *Server) sessionCookie(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if p == nil || p.IsAPIKey || p.MediaSource != "" || p.DeviceID == "" {
		writeError(w, 403, "a device login is needed for a session cookie")
		return
	}
	token, _ := auth.TokenFromRequest(r)
	if token == "" {
		writeError(w, 400, "no token on the request")
		return
	}
	auth.SetSessionCookie(w, r, token)
	w.WriteHeader(http.StatusNoContent)
}

func userJSON(u *model.User) map[string]any {
	return map[string]any{
		"id":      u.ID,
		"name":    u.Name,
		"isAdmin": u.IsAdmin,
		"role":    u.Role,
	}
}
