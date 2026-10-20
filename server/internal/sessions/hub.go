// Package sessions is "Play on": the players signed in to a login that are open right
// now, and a command channel to each of them. A phone picks a TV from the list and the
// server hands the TV "play this, from here"; the TV plays with its own player (libmpv
// and libass), so everything that works locally works there. Plex's player picker and
// Jellyfin's "Play on" do the same.
//
// Players long-poll for commands, which also keeps them on the list; a player that
// stops polling drops off after a minute. Everything lives in memory: a restart only
// means the players reconnect on their next poll.
package sessions

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Command is what a sender asks a player to do.
type Command struct {
	Type string `json:"type"` // play | pause | resume | toggle | seek | seekBy | next | stop
	// play: the item; the player resumes from the server's saved position unless
	// PositionMs is set, which the sender sets when handing over mid-episode.
	ItemID     string `json:"itemId,omitempty"`
	PositionMs int64  `json:"positionMs,omitempty"`
	// seek: an absolute position; seekBy: an offset.
	Seconds int `json:"seconds,omitempty"`
	// From is the sender's device name, for the player's "Playing from <phone>" line.
	From string `json:"from,omitempty"`
}

// NowPlaying is what a player reports it is doing.
type NowPlaying struct {
	ItemID     string `json:"itemId"`
	Title      string `json:"title,omitempty"`
	PositionMs int64  `json:"positionMs"`
	Paused     bool   `json:"paused"`
}

// Player is one connected client as the list shows it.
type Player struct {
	DeviceID   string      `json:"deviceId"`
	Name       string      `json:"name"`
	Client     string      `json:"client,omitempty"`
	LastSeen   time.Time   `json:"lastSeen"`
	NowPlaying *NowPlaying `json:"nowPlaying,omitempty"`
}

type player struct {
	Player
	userID string
	cmds   chan Command
}

type Hub struct {
	mu      sync.Mutex
	players map[string]*player
}

// Stale is how long after its last poll a player is still listed.
const Stale = 60 * time.Second

func New() *Hub { return &Hub{players: map[string]*player{}} }

func (h *Hub) touch(deviceID, userID, name, client string) *player {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.players[deviceID]
	if p == nil {
		p = &player{cmds: make(chan Command, 8)}
		p.DeviceID = deviceID
		h.players[deviceID] = p
	}
	p.userID = userID
	if name != "" {
		p.Name = name
	}
	if client != "" {
		p.Client = client
	}
	p.LastSeen = time.Now()
	return p
}

// Wait registers the player (or refreshes it) and returns its next command, or false
// when ctx ends first (the long poll's deadline, or the client going away).
func (h *Hub) Wait(ctx context.Context, deviceID, userID, name, client string) (Command, bool) {
	p := h.touch(deviceID, userID, name, client)
	select {
	case c := <-p.cmds:
		return c, true
	case <-ctx.Done():
		return Command{}, false
	}
}

// Send queues a command for a player of the same login. False when the player is not
// connected (or is someone else's), or its queue is full.
func (h *Hub) Send(deviceID, userID string, c Command) bool {
	h.mu.Lock()
	p := h.players[deviceID]
	h.mu.Unlock()
	if p == nil || p.userID != userID || time.Since(p.LastSeen) > Stale {
		return false
	}
	select {
	case p.cmds <- c:
		return true
	default:
		return false
	}
}

// SetNowPlaying records what a player is doing (nil when it stopped).
func (h *Hub) SetNowPlaying(deviceID string, now *NowPlaying) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p := h.players[deviceID]; p != nil {
		p.NowPlaying = now
		p.LastSeen = time.Now()
	}
}

// List is the login's connected players other than the caller's own device, the ones
// playing something first, then by name.
func (h *Hub) List(userID, exceptDevice string) []Player {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Player{}
	for id, p := range h.players {
		if time.Since(p.LastSeen) > Stale {
			delete(h.players, id)
			continue
		}
		if p.userID != userID || id == exceptDevice {
			continue
		}
		out = append(out, p.Player)
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := out[i].NowPlaying != nil, out[j].NowPlaying != nil
		if pi != pj {
			return pi
		}
		return out[i].Name < out[j].Name
	})
	return out
}
