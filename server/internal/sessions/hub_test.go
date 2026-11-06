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
				h.Wait(ctx, "tv", "u1", "TV", "android", nil)
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
	h.Wait(ctx, "tv", "u1", "TV", "android", nil) // registers it, returns at once
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
	h.Wait(ctx, "tv", "u1", "TV", "android", nil)
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
		return h.Wait(ctx, "tv", "u1", "TV", "android", nil)
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

// A handed-over "play" carries an id that the player can report back, so the sender can
// tell its own request apart from the same item already playing; caps say who can.
func TestHandoffIdRoundTrip(t *testing.T) {
	h := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.Wait(ctx, "tv", "u1", "TV", "android", []string{"handoff"})
	if !h.Send("tv", "u1", Command{Type: "play", ItemID: "A", Handoff: "h1"}) {
		t.Fatal("play not queued")
	}
	wctx, wcancel := context.WithTimeout(context.Background(), time.Second)
	defer wcancel()
	c, ok := h.Wait(wctx, "tv", "u1", "TV", "android", []string{"handoff"})
	if !ok || c.Handoff != "h1" {
		t.Fatalf("command lost its handoff id: %+v %v", c, ok)
	}
	h.SetNowPlaying("tv", &NowPlaying{ItemID: "A", Handoff: "h1"}, 1)
	p := h.Connected("u1")["tv"]
	if p.NowPlaying == nil || p.NowPlaying.Handoff != "h1" {
		t.Fatalf("handoff id not reported back: %+v", p.NowPlaying)
	}
	if len(p.Caps) != 1 || p.Caps[0] != "handoff" {
		t.Fatalf("caps not kept: %v", p.Caps)
	}
}

// Signing a device out takes it off "Play on" at once: its open poll ends, it is no longer
// listed, and commands to it fail (it used to stay connected until it went stale).
func TestRemoveEndsPollAndCommands(t *testing.T) {
	h := New()
	polled := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, ok := h.Wait(ctx, "tv", "u1", "TV", "android", nil)
		polled <- ok
	}()
	for i := 0; len(h.Connected("u1")) == 0; i++ { // wait until it is registered
		if i > 200 {
			t.Fatal("player never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.Remove("tv")
	select {
	case ok := <-polled:
		if ok {
			t.Fatal("the poll returned a command after sign-out")
		}
	case <-time.After(time.Second):
		t.Fatal("the open poll did not end on sign-out")
	}
	if len(h.Connected("u1")) != 0 || h.Send("tv", "u1", Command{Type: "pause"}) {
		t.Fatal("a signed-out device is still reachable")
	}
}

// A password change signs out every device of the login but the one it was made on.
func TestRemoveUserKeepsOneDevice(t *testing.T) {
	h := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, d := range []string{"tv", "phone", "laptop"} {
		h.Wait(ctx, d, "u1", d, "x", nil)
	}
	h.Wait(ctx, "other", "u2", "other", "x", nil)
	h.RemoveUser("u1", "phone")
	if c := h.Connected("u1"); len(c) != 1 || c["phone"].DeviceID != "phone" {
		t.Fatalf("left connected: %v", c)
	}
	if len(h.Connected("u2")) != 1 {
		t.Fatal("another login's device was removed")
	}
}

// A player back after going stale shows no "now playing" from before its absence.
func TestStaleReconnectClearsNowPlaying(t *testing.T) {
	clock := time.Now()
	nowFn = func() time.Time { return clock }
	t.Cleanup(func() { nowFn = time.Now })
	h := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.Wait(ctx, "tv", "u1", "TV", "android", nil)
	h.SetNowPlaying("tv", &NowPlaying{ItemID: "A"}, 1)
	clock = clock.Add(2 * Stale)
	h.Wait(ctx, "tv", "u1", "TV", "android", nil) // back
	if np := h.Connected("u1")["tv"].NowPlaying; np != nil {
		t.Fatalf("stale state shown after reconnect: %+v", np)
	}
}
