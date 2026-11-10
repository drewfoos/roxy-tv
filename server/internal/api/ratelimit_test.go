package api

import "testing"

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
