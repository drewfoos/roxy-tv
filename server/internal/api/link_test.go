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

// linkFixture is a link store on a clock the test moves, with the /wait route.
func linkFixture(t *testing.T) (*Server, http.Handler, *time.Time) {
	t.Helper()
	s := &Server{links: newLinkStore()}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s.links.now = func() time.Time { return now }
	r := chi.NewRouter()
	r.Get("/link/{code}/wait", s.linkWait)
	return s, r, &now
}

func (s *Server) addLink(code string) *linkEntry {
	e := &linkEntry{code: code, secret: "s3cret", createdAt: s.links.now(), done: make(chan struct{})}
	s.links.mu.Lock()
	s.links.entries[e.code] = e
	s.links.mu.Unlock()
	return e
}

// A code approved in its last second, between two of the TV's polls: the TV's next poll
// comes after the ten minutes and must still receive the token. It was told the code had
// expired, while the phone showed "approved" and the account kept a signed-in device
// that nobody held the token of.
func TestLinkApprovedAtExpiryIsStillCollected(t *testing.T) {
	s, h, now := linkFixture(t)
	s.addLink("ABC123")

	*now = now.Add(linkTTL - time.Second)
	e, ok := s.links.claim("abc-123")
	if e == nil || !ok {
		t.Fatalf("a code in its last second could not be approved: %v %v", e, ok)
	}
	s.links.approve(e, "device-token", &model.User{ID: "u1", Name: "drew"}, "dev1")

	*now = now.Add(30 * time.Second) // the next poll: past the code's ten minutes
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/link/ABC123/wait?secret=s3cret", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "device-token") {
		t.Fatalf("the poll after an approval at expiry: %d %s, want the token", rec.Code, rec.Body.String())
	}
	if e2, _ := s.links.claim("ABC123"); e2 != nil {
		t.Fatal("the code is still there after the handover")
	}
}

// An approved code the device never collects: after the collection time the code is
// gone and the device the approval signed in is signed out again. An unapproved code
// just expires, and cannot be approved after its ten minutes.
func TestLinkApprovedButNeverCollectedSignsTheDeviceOut(t *testing.T) {
	s, h, now := linkFixture(t)
	var signedOut []string
	s.links.abandoned = func(id string) { signedOut = append(signedOut, id) }

	s.addLink("ABC123")
	s.addLink("DEF456")
	e, ok := s.links.claim("ABC123")
	if !ok {
		t.Fatal("claim failed")
	}
	if e2, again := s.links.claim("ABC123"); e2 == nil || again {
		t.Fatalf("a second approval of one code: %v %v, want the entry and false", e2, again)
	}
	s.links.approve(e, "device-token", &model.User{ID: "u1"}, "dev1")

	*now = now.Add(linkCollect - time.Second)
	s.links.sweep()
	if len(signedOut) != 0 || s.links.get("ABC123") == nil {
		t.Fatalf("an approved code was dropped before its collection time: signed out %v", signedOut)
	}

	*now = now.Add(linkTTL) // both are over now
	if e3, _ := s.links.claim("DEF456"); e3 != nil {
		t.Fatal("an expired code could still be approved")
	}
	s.links.sweep()
	if len(signedOut) != 1 || signedOut[0] != "dev1" {
		t.Fatalf("signed out %v, want the uncollected device dev1 and nothing else", signedOut)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/link/ABC123/wait?secret=s3cret", nil))
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "device-token") {
		t.Fatalf("a poll after the collection time: %d %s, want 404", rec.Code, rec.Body.String())
	}
	s.links.mu.Lock()
	left := len(s.links.entries)
	s.links.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d codes left after the sweep", left)
	}

	// A collected code is not a device to sign out.
	s.addLink("GHJ789")
	e, _ = s.links.claim("GHJ789")
	s.links.approve(e, "token-2", &model.User{ID: "u1"}, "dev2")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/link/GHJ789/wait?secret=s3cret", nil))
	if rec.Code != 200 {
		t.Fatalf("collecting: %d", rec.Code)
	}
	*now = now.Add(linkTTL)
	s.links.sweep()
	if len(signedOut) != 1 {
		t.Fatalf("a collected device was signed out: %v", signedOut)
	}
}
