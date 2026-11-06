package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Several /wait requests can be parked on one code (a TV that retried or polled twice);
// once it is approved, exactly one of them may receive the device token.
func TestLinkTokenDeliveredOnce(t *testing.T) {
	s := &Server{links: newLinkStore()}
	e := &linkEntry{code: "ABC123", secret: "s3cret", createdAt: time.Now(), done: make(chan struct{})}
	s.links.mu.Lock()
	s.links.entries[e.code] = e
	s.links.mu.Unlock()
	r := chi.NewRouter()
	r.Get("/link/{code}/wait", s.linkWait)

	const waiters = 5
	codes := make([]int, waiters)
	bodies := make([]string, waiters)
	var wg sync.WaitGroup
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/link/ABC123/wait?secret=s3cret", nil))
			codes[i], bodies[i] = rec.Code, rec.Body.String()
		}(i)
	}
	time.Sleep(100 * time.Millisecond) // let them all park on done
	s.links.mu.Lock()
	e.token, e.user, e.deviceID = "device-token", &model.User{ID: "u1", Name: "drew"}, "dev1"
	s.links.mu.Unlock()
	close(e.done)
	wg.Wait()

	got := 0
	for i := range codes {
		if codes[i] == 200 && strings.Contains(bodies[i], "device-token") {
			got++
		} else if codes[i] != 404 || strings.Contains(bodies[i], "device-token") {
			t.Fatalf("waiter %d: %d %s", i, codes[i], bodies[i])
		}
	}
	if got != 1 {
		t.Fatalf("token delivered %d times, want exactly once", got)
	}
	if s.links.get("ABC123") != nil {
		t.Fatal("code still there after the handover")
	}
}
