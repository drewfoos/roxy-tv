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
				h.SetNowPlaying("tv", &NowPlaying{ItemID: "x"}, int64(j+1))
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

// Status reports can overtake each other on the way: a stale "playing" arriving after
// the "stopped" sent later must not bring the episode back.
func TestStaleStatusReportIsIgnored(t *testing.T) {
	h := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.Wait(ctx, "tv", "u1", "TV", "android")
	nowPlaying := func() *NowPlaying { return h.Connected("u1")["tv"].NowPlaying }

	if !h.SetNowPlaying("tv", nil, 2) { // stopped, sent second, arrives first
		t.Fatal("the stop report was refused")
	}
	if h.SetNowPlaying("tv", &NowPlaying{ItemID: "A"}, 1) { // playing A, sent first, arrives late
		t.Fatal("an older report was accepted")
	}
	if nowPlaying() != nil {
		t.Fatalf("stale report brought back %v", nowPlaying())
	}
	if !h.SetNowPlaying("tv", &NowPlaying{ItemID: "B"}, 3) || nowPlaying() == nil || nowPlaying().ItemID != "B" {
		t.Fatal("a newer report was not applied")
	}
	if h.SetNowPlaying("tv", &NowPlaying{ItemID: "B", PositionMs: 5}, 3) {
		t.Fatal("a repeated sequence number was accepted")
	}
	// Older apps send no numbers: always applied.
	if !h.SetNowPlaying("tv", nil, 0) || nowPlaying() != nil {
		t.Fatal("an unnumbered report was not applied")
	}
}
