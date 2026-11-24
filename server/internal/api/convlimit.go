package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/auth"
)

// conversions keeps one client from taking the server's conversions for itself.
//
// A conversion (a transcode, a remux, a song converted for a phone) is an ffmpeg run:
// by far the most a request can cost the server. The general API limit does not cover
// them well (twenty a second is nothing for a list call and far too many ffmpeg
// starts), and the stream paths are exempt from it altogether. Until now the only
// limit was how many may run at once on the whole server, so one signed-in device
// could hold every place, and held each for as long as it liked.
//
// Two rules of their own, per client (the device, else the login, else the address):
//
//   - starts are counted: startBurst at once, then startPerSecond. A player that seeks
//     about asks for a conversion per seek, which this allows; a loop does not get an
//     ffmpeg per turn.
//   - a client has at most perClient conversions of a kind running. A player plays one
//     stream, and a seek or a change of quality asks for the next before the old one
//     has gone, so a new one past the limit is not refused: it ends that client's
//     oldest, which the player has left behind anyway.
type conversions struct {
	mu      sync.Mutex
	running map[string][]*runningConversion // kind|client -> oldest first
	starts  *rateLimiter
}

type runningConversion struct {
	cancel context.CancelFunc
}

const (
	// perClientConversions: the stream being played and the one that replaces it.
	perClientConversions = 2
	conversionStartBurst = 30
	conversionStartRate  = 1 // a second, after the burst
	// supersededWait is how long a new conversion waits for a place its own client's
	// ended one is giving up (ffmpeg has to be stopped first).
	supersededWait = 3 * time.Second
)

func newConversions() *conversions {
	return &conversions{running: map[string][]*runningConversion{}, starts: newRateLimiter(conversionStartRate, conversionStartBurst)}
}

// conversionClient names who a conversion is for: the device (its own login's, or the
// one a media token was made for), else the login, else the address.
func conversionClient(r *http.Request) string {
	p := auth.PrincipalFrom(r.Context())
	if d := playDevice(p); d != "" {
		return "device|" + d
	}
	if p != nil && p.UserID != "" {
		return "user|" + p.UserID
	}
	ip := r.RemoteAddr
	if i := strings.LastIndex(ip, ":"); i > 0 {
		ip = ip[:i]
	}
	return "ip|" + ip
}

// mayStart counts a conversion's start against its client; false means too many, too fast.
func (c *conversions) mayStart(client string) bool {
	return c.starts.allow(client)
}

// begin registers a running conversion of kind for client. ctx is the conversion's own:
// it ends when the request does, or when a later conversion of the same client and kind
// takes its place. superseded says whether this call ended an older one (its place on
// the server is on its way to being free). end must be called when the conversion is over.
func (c *conversions) begin(parent context.Context, kind, client string) (ctx context.Context, superseded bool, end func()) {
	ctx, cancel := context.WithCancel(parent)
	mine := &runningConversion{cancel: cancel}
	key := kind + "|" + client
	c.mu.Lock()
	list := c.running[key]
	for len(list) >= perClientConversions {
		list[0].cancel()
		list = list[1:]
		superseded = true
	}
	c.running[key] = append(list, mine)
	c.mu.Unlock()
	return ctx, superseded, func() {
		cancel()
		c.mu.Lock()
		defer c.mu.Unlock()
		list := c.running[key]
		for i, rc := range list {
			if rc == mine {
				list = append(list[:i:i], list[i+1:]...)
				break
			}
		}
		if len(list) == 0 {
			delete(c.running, key)
		} else {
			c.running[key] = list
		}
	}
}

// count is how many conversions of kind the client has registered (for tests).
func (c *conversions) count(kind, client string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.running[kind+"|"+client])
}

// takeSlot takes one of the server's places for a kind of conversion. With wait (the
// client has just ended a conversion of its own, whose place is being given up) it
// waits a moment for one instead of failing at once.
func takeSlot(ctx context.Context, slots chan struct{}, wait bool) bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
	}
	if !wait {
		return false
	}
	t := time.NewTimer(supersededWait)
	defer t.Stop()
	select {
	case slots <- struct{}{}:
		return true
	case <-t.C:
		return false
	case <-ctx.Done():
		return false
	}
}

// tooManyConversions answers a client that starts conversions faster than a player does.
func tooManyConversions(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "2")
	writeError(w, http.StatusTooManyRequests, "too many conversions started; slow down")
}
