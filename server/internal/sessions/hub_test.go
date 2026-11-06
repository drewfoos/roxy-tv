package sessions

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Polling (Wait refreshes the player) and command delivery (Send) run on different
// requests at once; run with -race to check they share the player fields safely.
func TestWaitAndSendConcurrently(t *testing.T) {
	h := New()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				h.Wait(ctx, "tv", "u1", "TV", "android")
				cancel()
			}
		}()
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				h.Send("tv", "u1", Command{Type: "toggle"})
				h.SetNowPlaying("tv", &NowPlaying{ItemID: "x"})
				h.List("u1", "")
			}
		}()
	}
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// Send only reaches a connected player of the same login.
func TestSendChecksLoginAndConnection(t *testing.T) {
	h := New()
	if h.Send("tv", "u1", Command{Type: "play"}) {
		t.Fatal("sent to a player that never connected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.Wait(ctx, "tv", "u1", "TV", "android") // registers it, returns at once
	if h.Send("tv", "u2", Command{Type: "play"}) {
		t.Fatal("sent to another login's player")
	}
	if !h.Send("tv", "u1", Command{Type: "play"}) {
		t.Fatal("did not send to a connected player of the same login")
	}
	h.mu.Lock()
	h.players["tv"].LastSeen = time.Now().Add(-2 * Stale)
	h.mu.Unlock()
	if h.Send("tv", "u1", Command{Type: "play"}) {
		t.Fatal("sent to a player that stopped polling")
	}
}
