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
	// FromStart: play from the beginning, even if the player already has this item open.
	FromStart bool `json:"fromStart,omitempty"`
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
	userID   string
	cmds     chan queued
	stateSeq int64 // the newest SetNowPlaying sequence number accepted
}

type Hub struct {
	mu      sync.Mutex
	players map[string]*player
}

// Stale is how long after its last poll a player is still listed.
const Stale = 60 * time.Second

// CommandTTL is how long a queued command stays good. A connected player picks commands up
// within a poll; one still queued after this was sent while the player was away, and
// "pause", "seek" or "play this" from then must not run when it comes back.
const CommandTTL = 30 * time.Second

// queued is a command and when it was sent.
type queued struct {
	cmd Command
	at  time.Time
}

// nowFn is the hub's clock; tests move it.
var nowFn = time.Now

func New() *Hub { return &Hub{players: map[string]*player{}} }

// touch registers or refreshes a player and returns its command queue (read under the
// lock: a player coming back after going stale gets a new, empty queue, so commands
// queued while it was away are never run).
func (h *Hub) touch(deviceID, userID, name, client string) chan queued {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.players[deviceID]
	if p == nil {
		p = &player{cmds: make(chan queued, 8)}
		p.DeviceID = deviceID
		h.players[deviceID] = p
	} else if nowFn().Sub(p.LastSeen) > Stale {
		p.cmds = make(chan queued, 8)
	}
	p.userID = userID
	if name != "" {
		p.Name = name
	}
	if client != "" {
		p.Client = client
	}
	p.LastSeen = nowFn()
	return p.cmds
}

// Wait registers the player (or refreshes it) and returns its next command, or false
// when ctx ends first (the long poll's deadline, or the client going away).
// Commands older than CommandTTL are dropped, not returned.
func (h *Hub) Wait(ctx context.Context, deviceID, userID, name, client string) (Command, bool) {
	cmds := h.touch(deviceID, userID, name, client)
	for {
		select {
		case q := <-cmds:
			if nowFn().Sub(q.at) > CommandTTL {
				continue // sent while the player was away
			}
			return q.cmd, true
		case <-ctx.Done():
			return Command{}, false
		}
	}
}

// Send queues a command for a player of the same login. False when the player is not
// connected (or is someone else's), or its queue is full.
//
// The checks and the queueing happen under h.mu: touch and SetNowPlaying write the same
// player fields from the player's own poll. The send never blocks (a full queue returns
// false), so holding the lock for it is safe.
func (h *Hub) Send(deviceID, userID string, c Command) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.players[deviceID]
	if p == nil || p.userID != userID || nowFn().Sub(p.LastSeen) > Stale {
		return false
	}
	select {
	case p.cmds <- queued{c, nowFn()}:
		return true
	default:
		return false
	}
}

// SetNowPlaying records what a player is doing (nil when it stopped). seq is the
// player's own increasing number for its reports: one not newer than the last accepted
// is out of date (reports can overtake each other on the way) and is ignored, so a late
// "playing A" cannot undo the "stopped" sent after it. 0 means the client sends no
// numbers (older apps): always accepted. False when the report was ignored.
func (h *Hub) SetNowPlaying(deviceID string, now *NowPlaying, seq int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.players[deviceID]
	if p == nil {
		return false
	}
	p.LastSeen = nowFn() // a late report still shows the player is there
	if seq > 0 {
		if seq <= p.stateSeq {
			return false
		}
		p.stateSeq = seq
	}
	p.NowPlaying = now
	return true
}

// Connected: the login's players polling right now (this one included), by device id.
func (h *Hub) Connected(userID string) map[string]Player {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]Player{}
	for id, p := range h.players {
		if p.userID == userID && nowFn().Sub(p.LastSeen) <= Stale {
			out[id] = p.Player
		}
	}
	return out
}

// List is the login's connected players other than the caller's own device, the ones
// playing something first, then by name. Players gone longer than Stale are dropped.
func (h *Hub) List(userID, exceptDevice string) []Player {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Player{}
	for id, p := range h.players {
		if nowFn().Sub(p.LastSeen) > Stale {
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
