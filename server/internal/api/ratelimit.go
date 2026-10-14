package api

import (
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

func (rl *rateLimiter) allow(key string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b := rl.buckets[key]
	if b == nil {
		b = &bucket{tokens: rl.burst, last: now}
		rl.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
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

func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exemptFromRateLimit(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		key := r.RemoteAddr
		if i := strings.LastIndex(key, ":"); i > 0 {
			key = key[:i]
		}
		if tok := r.Header.Get("Authorization"); tok != "" {
			key += "|" + tok
		} else if k := r.URL.Query().Get("api_key"); k != "" {
			key += "|" + k
		}
		if !rl.allow(key) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "too many requests; slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}
