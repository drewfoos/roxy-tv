package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter is a per-client token bucket for the JSON API: a misbehaving client
// (a loop of list calls, a reconnect storm) gets 429s instead of starving playback.
// Media bytes (/v1/stream, images, trickplay, attachments) are exempt: a player
// legitimately issues many range requests, and images are fetched in bursts.
//
// Defaults: 20 requests/s sustained with a burst of 60 per client IP. A client is
// identified by its remote IP (behind RealIP middleware) plus the bearer token when
// present, so two users on one NAT do not share a bucket.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perSecond, burst float64) *rateLimiter {
	rl := &rateLimiter{buckets: map[string]*bucket{}, rate: perSecond, burst: burst}
	go rl.sweep()
	return rl
}

// allowBoth takes one token from the client's own bucket and one from its address's
// (three times the size), or none at all: a request refused by one bucket costs nothing
// in the other. Taking them one after the other drained a well-behaved client's own
// bucket while its address was over the limit (and the other way round).
func (rl *rateLimiter) allowBoth(key, ipKey string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	own := rl.refill(key, 1, now)
	if ipKey == key { // cannot happen with the "ip|" prefix; one bucket is not charged twice
		if own.tokens < 1 {
			return false
		}
		own.tokens--
		return true
	}
	addr := rl.refill(ipKey, 3, now)
	if own.tokens < 1 || addr.tokens < 1 {
		return false
	}
	own.tokens--
	addr.tokens--
	return true
}

// refill returns key's bucket topped up to now. Call with the lock held.
func (rl *rateLimiter) refill(key string, scale float64, now time.Time) *bucket {
	b := rl.buckets[key]
	if b == nil {
		b = &bucket{tokens: rl.burst * scale, last: now}
		rl.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * rl.rate * scale
	if b.tokens > rl.burst*scale {
		b.tokens = rl.burst * scale
	}
	b.last = now
	return b
}

// sweep drops buckets idle for a few minutes so the map does not grow forever.
func (rl *rateLimiter) sweep() {
	for range time.Tick(5 * time.Minute) {
		cutoff := time.Now().Add(-5 * time.Minute)
		rl.mu.Lock()
		for k, b := range rl.buckets {
			if b.last.Before(cutoff) {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

func exemptFromRateLimit(path string) bool {
	return strings.HasPrefix(path, "/v1/stream/") || strings.HasPrefix(path, "/stream/") ||
		strings.Contains(path, "/images/") || strings.Contains(path, "/trickplay") ||
		strings.Contains(path, "/attachments/") || strings.Contains(path, "/subtitles/")
}

// humanWait: "30 seconds", "2 minutes".
func humanWait(secs int) string {
	if secs < 90 {
		return fmt.Sprintf("%d seconds", secs)
	}
	return fmt.Sprintf("%d minutes", (secs+59)/60)
}

func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exemptFromRateLimit(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		ip := r.RemoteAddr
		if i := strings.LastIndex(ip, ":"); i > 0 {
			ip = ip[:i]
		}
		key := ip
		if tok := r.Header.Get("Authorization"); tok != "" {
			key += "|" + tok
		} else if k := r.URL.Query().Get("api_key"); k != "" {
			key += "|" + k
		}
		// Each token has its own bucket (two people behind one router don't share), and
		// the address as a whole is capped at three times that, so inventing a new token
		// per request doesn't escape the limit.
		if !rl.allowBoth(key, "ip|"+ip) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "too many requests; slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}
