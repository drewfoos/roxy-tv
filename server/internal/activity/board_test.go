package activity

import (
	"testing"
	"time"
)

func board() (*Board, *time.Time) {
	b := New()
	at := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return at }
	return b, &at
}

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

func TestAReportListsASession(t *testing.T) {
	b, _ := board()
	b.Report("tv", Report{UserID: "u", ScopeID: "p", ItemID: "ep1", SourceID: "s1", PositionMs: 80_000, DurationMs: 1_450_000})
	got := b.List()
	if len(got) != 1 || got[0].ItemID != "ep1" || got[0].PositionMs != 80_000 || got[0].Paused {
		t.Fatalf("got %+v", got)
	}
}

func TestAQuietSessionIsPausedThenGone(t *testing.T) {
	b, at := board()
	b.Report("tv", Report{UserID: "u", ItemID: "ep1", PositionMs: 1000})
	*at = at.Add(PlayingFor + time.Second)
	if got := b.List(); len(got) != 1 || !got[0].Paused {
		t.Fatalf("quiet past the reporting interval should show as paused: %+v", got)
	}
	*at = at.Add(QuietFor)
	if got := b.List(); len(got) != 0 {
		t.Fatalf("a session that went quiet without a word should be dropped: %+v", got)
	}
}

func TestASessionThatSaidPausedStaysListed(t *testing.T) {
	b, at := board()
	b.Report("tv", Report{UserID: "u", ItemID: "ep1", PositionMs: 1000, Paused: yes()})
	*at = at.Add(QuietFor + time.Minute)
	if got := b.List(); len(got) != 1 || !got[0].Paused {
		t.Fatalf("a paused player is still there after a few minutes: %+v", got)
	}
	*at = at.Add(PausedFor)
	if got := b.List(); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestPlayingAgainAfterAPause(t *testing.T) {
	b, _ := board()
	b.Report("tv", Report{UserID: "u", ItemID: "ep1", PositionMs: 1000, Paused: yes()})
	b.Report("tv", Report{UserID: "u", ItemID: "ep1", PositionMs: 1000, Paused: no()})
	if got := b.List(); len(got) != 1 || got[0].Paused {
		t.Fatalf("got %+v", got)
	}
}

func TestSetStatePausesWithoutAPosition(t *testing.T) {
	b, at := board()
	b.Report("web", Report{UserID: "u", ItemID: "ep1", PositionMs: 5000})
	b.SetState("web", "ep2", true) // not what it plays
	if got := b.List(); got[0].Paused {
		t.Fatalf("got %+v", got)
	}
	b.SetState("web", "ep1", true)
	*at = at.Add(QuietFor + time.Minute)
	if got := b.List(); len(got) != 1 || !got[0].Paused || got[0].PositionMs != 5000 {
		t.Fatalf("paused, said so, and still listed at its position: %+v", got)
	}
	b.SetState("web", "ep1", false)
	if got := b.List(); got[0].Paused {
		t.Fatalf("got %+v", got)
	}
}

func TestStopEndsOnlyThatItem(t *testing.T) {
	b, _ := board()
	b.Report("tv", Report{UserID: "u", ItemID: "ep2", PositionMs: 1000})
	// The last report of the episode before can arrive after the next one started.
	b.Stop("tv", "ep1")
	if got := b.List(); len(got) != 1 {
		t.Fatalf("a stop for the item before must not end the one playing: %+v", got)
	}
	b.Stop("tv", "ep2")
	if got := b.List(); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestANewItemReplacesTheSession(t *testing.T) {
	b, at := board()
	b.Report("tv", Report{UserID: "u", ItemID: "ep1", SourceID: "s1", PositionMs: 1000, Audio: new(int)})
	started := *at
	*at = at.Add(time.Minute)
	b.Report("tv", Report{UserID: "u", ItemID: "ep2", PositionMs: 5000})
	got := b.List()
	if len(got) != 1 || got[0].ItemID != "ep2" || got[0].SourceID != "" || got[0].Audio != nil || !got[0].StartedAt.After(started) {
		t.Fatalf("the next item is a new session, with nothing kept from the last: %+v", got)
	}
}

func TestAReportWithoutDetailsKeepsThem(t *testing.T) {
	b, _ := board()
	sub := 4
	b.Report("tv", Report{UserID: "u", ScopeID: "p", ItemID: "ep1", SourceID: "s1", PositionMs: 1000, DurationMs: 9000, Subtitle: &sub})
	// The apps' "Play on" state names only the item and the position.
	b.Report("tv", Report{UserID: "u", ItemID: "ep1", PositionMs: 2000, Paused: no()})
	got := b.List()[0]
	if got.ScopeID != "p" || got.SourceID != "s1" || got.DurationMs != 9000 || got.Subtitle == nil || *got.Subtitle != 4 || got.PositionMs != 2000 {
		t.Fatalf("got %+v", got)
	}
}

func TestTheStreamMeetsTheSessionOnDeviceAndItem(t *testing.T) {
	b, _ := board()
	done := b.Opened("tv", Stream{Method: Transcode, ItemID: "ep1", SourceID: "s1", AudioIndex: -1, MaxHeight: 720, BitrateK: 4000, Encoder: "vaapi"})
	b.Opened("phone", Stream{Method: Direct, ItemID: "ep1", SourceID: "s1", AudioIndex: -1})()
	b.Report("tv", Report{UserID: "u", ItemID: "ep1", SourceID: "s1", PositionMs: 1000})
	got := b.List()
	if len(got) != 1 || got[0].Stream == nil || got[0].Stream.Method != Transcode || got[0].Stream.Open != 1 {
		t.Fatalf("got %+v", got)
	}
	if b.Converting() != 1 {
		t.Fatalf("converting = %d", b.Converting())
	}
	done()
	if b.Converting() != 0 {
		t.Fatalf("converting after the stream ended = %d", b.Converting())
	}
	if got := b.List(); got[0].Stream == nil || got[0].Stream.Method != Transcode {
		t.Fatalf("a stream the player has finished reading is still how it got the file: %+v", got)
	}
}

func TestTheNewestStreamIsTheOneShown(t *testing.T) {
	b, at := board()
	b.Report("web", Report{UserID: "u", ItemID: "ep1", SourceID: "s1", PositionMs: 1000})
	old := b.Opened("web", Stream{Method: Direct, ItemID: "ep1", SourceID: "s1", AudioIndex: -1})
	*at = at.Add(time.Second)
	b.Opened("web", Stream{Method: Remux, ItemID: "ep1", SourceID: "s1", AudioIndex: 2})
	old() // the direct request ends after the remux began
	if got := b.List(); got[0].Stream.Method != Remux || got[0].Stream.Open != 1 {
		t.Fatalf("got %+v", got[0].Stream)
	}
}

func TestRangeRequestsAreOneStream(t *testing.T) {
	b, at := board()
	b.Report("web", Report{UserID: "u", ItemID: "ep1", SourceID: "s1", PositionMs: 1000})
	b.Opened("web", Stream{Method: Direct, ItemID: "ep1", SourceID: "s1", AudioIndex: -1})()
	opened := *at
	*at = at.Add(time.Minute)
	b.Opened("web", Stream{Method: Direct, ItemID: "ep1", SourceID: "s1", AudioIndex: -1})()
	st := b.List()[0].Stream
	if !st.OpenedAt.Equal(opened) || st.Open != 0 {
		t.Fatalf("got %+v", st)
	}
}

func TestAStreamOfAnotherVersionIsNotTheSessions(t *testing.T) {
	b, _ := board()
	b.Opened("phone", Stream{Method: Direct, ItemID: "film", SourceID: "extended", AudioIndex: -1})()
	b.Report("phone", Report{UserID: "u", ItemID: "film", SourceID: "theatrical", PositionMs: 1000})
	if got := b.List(); got[0].Stream != nil {
		t.Fatalf("got %+v", got[0].Stream)
	}
}

func TestForgetDropsTheDevice(t *testing.T) {
	b, _ := board()
	b.Opened("tv", Stream{Method: Transcode, ItemID: "ep1", SourceID: "s1"})
	b.Opened("tv2", Stream{Method: Transcode, ItemID: "ep1", SourceID: "s1"})
	b.Report("tv", Report{UserID: "u", ItemID: "ep1", PositionMs: 1000})
	b.Forget("tv")
	if got := b.List(); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
	if b.Converting() != 1 {
		t.Fatalf("another device's stream must stay: %d", b.Converting())
	}
}

func TestOldStreamsAreForgotten(t *testing.T) {
	b, at := board()
	b.Opened("phone", Stream{Method: Direct, ItemID: "ep1", SourceID: "s1"})()
	*at = at.Add(streamKept + time.Minute)
	b.Opened("phone", Stream{Method: Direct, ItemID: "ep2", SourceID: "s2"})()
	if len(b.streams) != 1 {
		t.Fatalf("streams kept: %d", len(b.streams))
	}
}
