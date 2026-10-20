package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/sessions"
)

// "Play on" (docs/api/native.md "Sessions"): players long-poll /sessions/commands,
// senders list /sessions/players and post to /sessions/players/{device}/command.
// Everything is per login (the device token's user); profiles are not involved.

// GET /v1/sessions/commands?name=<display name>&client=<client>&wait=<s> -> {command}
// or 204 after `wait` seconds (25 max, 25 default) with nothing to do. Polling is also
// how a player stays on the list.
func (s *Server) sessionCommands(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	if p.DeviceID == "" {
		writeError(w, 400, "a device login is required")
		return
	}
	wait := 25
	if n, err := strconv.Atoi(r.URL.Query().Get("wait")); err == nil && n >= 0 && n <= 25 {
		wait = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(wait)*time.Second)
	defer cancel()
	cmd, ok := s.hub.Wait(ctx, p.DeviceID, p.UserID, r.URL.Query().Get("name"), r.URL.Query().Get("client"))
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, 200, map[string]any{"command": cmd})
}

// POST /v1/sessions/state {itemId, title?, positionMs, paused} -> 204. An empty itemId
// means the player stopped.
func (s *Server) sessionState(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	if p.DeviceID == "" {
		writeError(w, 400, "a device login is required")
		return
	}
	var body sessions.NowPlaying
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if body.ItemID == "" {
		s.hub.SetNowPlaying(p.DeviceID, nil)
	} else {
		s.hub.SetNowPlaying(p.DeviceID, &body)
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /v1/sessions/players -> {players: [Player]}: the login's other connected players.
func (s *Server) sessionPlayers(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	writeJSON(w, 200, map[string]any{"players": s.hub.List(p.UserID, p.DeviceID)})
}

// POST /v1/sessions/players/{device}/command {type, itemId?, positionMs?, seconds?} -> 202,
// 404 when that player is not connected to this login.
func (s *Server) sessionCommand(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	var cmd sessions.Command
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&cmd); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	switch cmd.Type {
	case "play":
		if cmd.ItemID == "" {
			writeError(w, 400, "play needs itemId")
			return
		}
	case "pause", "resume", "toggle", "seek", "seekBy", "next", "stop":
	default:
		writeError(w, 400, "unknown command type")
		return
	}
	if cmd.From == "" {
		cmd.From = r.URL.Query().Get("from")
	}
	if !s.hub.Send(chi.URLParam(r, "device"), p.UserID, cmd) {
		writeError(w, 404, "that player is not connected")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
