package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A request refused because the address as a whole is over its limit costs the client's
// own bucket nothing (and one refused by the client's own bucket costs the address
// nothing). The two used to be charged one after the other, so the first was spent on
// requests that were then refused.
func TestRateLimitRefusalCostsNothing(t *testing.T) {
	rl := &rateLimiter{buckets: map[string]*bucket{}, rate: 0, burst: 4} // no refill: counts are exact
	const ip = "ip|10.0.0.1"
	// Three other clients at the address use up its bucket (3 x 4 = 12 tokens).
	for _, key := range []string{"10.0.0.1|a", "10.0.0.1|b", "10.0.0.1|c"} {
		for i := 0; i < 4; i++ {
			if !rl.allowBoth(key, ip) {
				t.Fatalf("%s request %d refused inside both limits", key, i)
			}
		}
	}
	// A fourth is refused: the address has nothing left. Its own bucket stays full.
	for i := 0; i < 10; i++ {
		if rl.allowBoth("10.0.0.1|d", ip) {
			t.Fatal("allowed past the address's limit")
		}
	}
	if got := rl.buckets["10.0.0.1|d"].tokens; got != 4 {
		t.Errorf("own bucket after 10 refusals by the address = %v tokens, want 4 (untouched)", got)
	}

	// The other way round: a client over its own limit takes nothing from its address.
	rl = &rateLimiter{buckets: map[string]*bucket{}, rate: 0, burst: 4}
	for i := 0; i < 10; i++ {
		rl.allowBoth("10.0.0.1|a", ip)
	}
	if got := rl.buckets[ip].tokens; got != 8 {
		t.Errorf("address bucket after 4 allowed and 6 refused = %v tokens, want 8", got)
	}
}

// A page of music tiles asks for a cover each, and a song is read in ranges: neither is
// counted against the limit for JSON calls. The lists themselves still are.
func TestMusicMediaIsNotRateLimited(t *testing.T) {
	for path, exempt := range map[string]bool{
		"/v1/music/art/afcf5ef500f46ecc0f2157c8ec3310e3439e6143": true,
		"/v1/music/tracks/8e0053e3/stream":                       true,
		"/v1/stream/abc":                                         true,
		"/v1/music/tracks":                                       false,
		"/v1/music/tracks/8e0053e3/lyrics":                       false,
		"/v1/music/tracks/8e0053e3/played":                       false,
		"/v1/music/playlists":                                    false,
		"/v1/music":                                              false,
		"/v1/items":                                              false,
	} {
		if got := exemptFromRateLimit(path); got != exempt {
			t.Errorf("exemptFromRateLimit(%q) = %v, want %v", path, got, exempt)
		}
	}
	// Through the middleware: 200 covers in a burst all pass, where 200 list calls do not.
	rl := newRateLimiter(20, 60)
	h := rl.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	count := func(path string) (refused int) {
		for i := 0; i < 200; i++ {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = "192.0.2.7:5555"
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusTooManyRequests {
				refused++
			}
		}
		return refused
	}
	if n := count("/v1/music/art/afcf5ef500f46ecc0f2157c8ec3310e3439e6143"); n != 0 {
		t.Errorf("%d of 200 cover requests were refused", n)
	}
	if n := count("/v1/music/tracks"); n == 0 {
		t.Error("200 list calls in a burst were all let through: the limiter is not working in this test")
	}
}
