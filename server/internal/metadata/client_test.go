package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A server that keeps answering 429 gets maxRateRetries more tries, each after its
// Retry-After, then the 429 comes back as an error (never as an empty answer).
func TestGetJSONRateLimitedGivesUp(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	lim := newLimiter(10, time.Second)
	start := time.Now()
	err := getJSON(context.Background(), srv.Client(), lim, http.MethodGet, srv.URL, nil, nil, nil)
	if !isRateLimited(err) {
		t.Fatalf("err = %v, want a 429 error", err)
	}
	if n != 1+maxRateRetries {
		t.Errorf("calls = %d, want %d", n, 1+maxRateRetries)
	}
	if el := time.Since(start); el < time.Duration(maxRateRetries)*900*time.Millisecond {
		t.Errorf("retried after %s, Retry-After not honoured", el)
	}
}

// A pause (after a 429) holds back every caller sharing the limiter.
func TestLimiterPauseHoldsEveryone(t *testing.T) {
	lim := newLimiter(5, time.Second)
	lim.Pause(300 * time.Millisecond)
	start := time.Now()
	if err := lim.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el < 250*time.Millisecond {
		t.Errorf("Wait returned after %s during a pause", el)
	}
}

func TestRetryAfter(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 5 * time.Second, "0": time.Second, "30": 30 * time.Second, "600": maxRetryAfter, "junk": 5 * time.Second} {
		if got := retryAfter(in); got != want {
			t.Errorf("retryAfter(%q) = %s, want %s", in, got, want)
		}
	}
}

// AniList's limiter is shared by every client and stays under the degraded 30/min.
func TestAniListLimiterShared(t *testing.T) {
	a, b := NewAniList(), NewAniList()
	if a.lim != b.lim || a.lim != anilistLimiter {
		t.Fatal("AniList clients must share one limiter")
	}
	if perMin := anilistLimiter.rate * 60; perMin > 30 {
		t.Errorf("AniList rate %.0f/min, want <= 30", perMin)
	}
}
