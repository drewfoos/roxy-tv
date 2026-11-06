package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
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
	cmd, ok := s.hub.Wait(ctx, p.DeviceID, p.UserID, r.URL.Query().Get("name"), r.URL.Query().Get("client"), pollCaps(r.URL.Query().Get("caps")))
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, 200, map[string]any{"command": cmd})
}

// pollCaps reads the poll's caps=a,b list: at most 8 short words, so a client cannot make
// the player list large.
func pollCaps(raw string) []string {
	var caps []string
	for _, c := range strings.Split(raw, ",") {
		c = strings.TrimSpace(c)
		if c == "" || len(c) > 32 {
			continue
		}
		if caps = append(caps, c); len(caps) == 8 {
			break
		}
	}
	return caps
}

// POST /v1/sessions/state {itemId, title?, positionMs, paused, seq?} -> 204. An empty
// itemId means the player stopped. seq orders the reports (see Hub.SetNowPlaying).
func (s *Server) sessionState(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	if p.DeviceID == "" {
		writeError(w, 400, "a device login is required")
		return
	}
	var body struct {
		sessions.NowPlaying
		Seq int64 `json:"seq"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	now := &body.NowPlaying
	if body.ItemID == "" {
		now = nil
	}
	s.hub.SetNowPlaying(p.DeviceID, now, body.Seq)
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

// DeviceInfo is one device signed in with the login (Device Management), with whether it
// is the caller, connected right now, and what it plays.
type DeviceInfo struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Client     string               `json:"client,omitempty"`
	LastSeen   time.Time            `json:"lastSeen"`
	CreatedAt  time.Time            `json:"createdAt"`
	Current    bool                 `json:"current"`
	Online     bool                 `json:"online"`
	IP         string               `json:"ip,omitempty"`
	Network    string               `json:"network,omitempty"` // home | tailscale | server | internet
	NowPlaying *sessions.NowPlaying `json:"nowPlaying,omitempty"`
}

// GET /v1/devices -> {devices: [DeviceInfo]}: every device signed in with this login, not
// just the players connected right now (/sessions/players is those, for "Play on").
func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	devs, err := s.store.ListDevices(r.Context(), p.UserID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	live := s.hub.Connected(p.UserID)
	out := make([]DeviceInfo, 0, len(devs))
	for _, d := range devs {
		info := DeviceInfo{ID: d.ID, Name: d.Name, Client: d.Client, LastSeen: d.LastSeen, CreatedAt: d.CreatedAt, Current: d.ID == p.DeviceID, IP: d.LastIP, Network: networkOf(d.LastIP)}
		if pl, ok := live[d.ID]; ok {
			info.Online, info.NowPlaying = true, pl.NowPlaying
			if pl.Name != "" {
				info.Name = pl.Name
			}
		}
		out = append(out, info)
	}
	writeJSON(w, 200, map[string]any{"devices": out})
}

// DELETE /v1/devices/{id}: sign one of the login's other devices out (its token stops
// working at once). The caller signs itself out with /auth/logout instead.
func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	if id == p.DeviceID {
		writeError(w, 400, "use Sign out to sign this device out")
		return
	}
	ok, err := s.store.DeleteUserDevice(r.Context(), p.UserID, id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if !ok {
		writeError(w, 404, "no such device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// networkOf names where an address is: this server, the home network, Tailscale, or the
// internet. The page shows this always and the address itself only on request.
func networkOf(ip string) string {
	p := net.ParseIP(strings.Trim(ip, "[] "))
	switch {
	case p == nil:
		return ""
	case p.IsLoopback():
		return "server"
	case tailnet.Contains(p):
		return "tailscale"
	case isLocalIP(ip):
		return "home"
	}
	return "internet"
}

var _, tailnet, _ = net.ParseCIDR("100.64.0.0/10")
