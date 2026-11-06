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

// A command sent while a player was away must not run when it comes back: after going
// stale it re-registers with an empty queue, and a command older than CommandTTL is
// dropped anyway (a short drop-out that never went stale).
func TestQueuedCommandsDoNotSurviveAnAbsence(t *testing.T) {
	clock := time.Now()
	nowFn = func() time.Time { return clock }
	t.Cleanup(func() { nowFn = time.Now })
	h := New()
	poll := func(d time.Duration) (Command, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		return h.Wait(ctx, "tv", "u1", "TV", "android")
	}
	poll(0) // the TV connects

	// It drops off; a phone sends "pause" while it still counts as connected.
	clock = clock.Add(20 * time.Second)
	if !h.Send("tv", "u1", Command{Type: "pause"}) {
		t.Fatal("pause not queued")
	}
	// The TV comes back much later (stale by then): the old pause must not run.
	clock = clock.Add(2 * Stale)
	if c, ok := poll(10 * time.Millisecond); ok {
		t.Fatalf("ran a command from before the absence: %+v", c)
	}

	// A short drop-out that never went stale: queued, then picked up after the TTL.
	if !h.Send("tv", "u1", Command{Type: "seek", Seconds: 600}) {
		t.Fatal("seek not queued")
	}
	clock = clock.Add(CommandTTL + time.Second)
	if c, ok := poll(10 * time.Millisecond); ok {
		t.Fatalf("ran an expired command: %+v", c)
	}

	// A fresh command still arrives.
	if !h.Send("tv", "u1", Command{Type: "toggle"}) {
		t.Fatal("toggle not queued")
	}
	if c, ok := poll(time.Second); !ok || c.Type != "toggle" {
		t.Fatalf("fresh command: %+v %v", c, ok)
	}
}
