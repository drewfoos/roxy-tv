// Package activity is "Now playing" for the server's owner: who is watching what right
// now, on which device, and how the server is sending it (the file as it is, repackaged,
// or converted). Jellyfin's dashboard shows the same on its "Active devices" cards.
//
// It is fed from two sides. Players report where they are (POST /v1/me/progress every
// ten seconds, and the apps' "Play on" state), which says who, what and where. The
// stream handlers report what they are sending (direct, remux, transcode), which says
// how. The two meet on the device and the item.
//
// Everything lives in memory: a restart forgets it, and the next report or stream
// request brings it back.
package activity

import (
	"sort"
	"sync"
	"time"
)

// How the server is sending a file.
const (
	Direct    = "direct"    // the file as it is, by byte range
	Remux     = "remux"     // video copied into a new container, audio copied or converted
	Transcode = "transcode" // video re-encoded
)

const (
	// PlayingFor: a session whose last report is this recent is playing, unless the report
	// itself said paused. Players report every 10 s; three missed reports is not playing.
	PlayingFor = 35 * time.Second
	// PausedFor: how long a session that said it paused stays listed.
	PausedFor = 30 * time.Minute
	// QuietFor: how long a session that just went quiet stays listed (as paused). It never
	// said it paused or stopped: a closed lid, a lost connection, an app that does not say.
	QuietFor = 3 * time.Minute
	// streamKept: a stream nobody is reading and no session points at is forgotten after this.
	streamKept = 6 * time.Hour
)

// Report is what a player says about itself.
type Report struct {
	UserID  string
	ScopeID string // the profile; "" leaves the session's as it was
	ItemID  string
	// SourceID is the version playing; "" leaves the session's as it was.
	SourceID   string
	PositionMs int64
	DurationMs int64 // 0 when the player did not say
	// Audio and Subtitle are the tracks the viewer picked (stream indexes; Subtitle -1 =
	// off); nil when the player did not say, which leaves the session's as they were.
	Audio    *int
	Subtitle *int
	// Paused is nil when the player does not say (older apps): the session then counts as
	// playing while its reports keep coming.
	Paused *bool
}

// Stream is one way the server is sending an item to a device.
type Stream struct {
	Method   string `json:"method"`
	ItemID   string `json:"-"`
	SourceID string `json:"-"`
	// AudioIndex is the audio track the stream carries (a remux or a web transcode carry
	// one); -1 when it carries them all.
	AudioIndex int `json:"-"`
	// Web: made for a browser (one audio track, converted unless browsers decode it).
	Web bool `json:"-"`
	// Transcodes: the height and video bitrate asked for (0 = the source's) and the encoder.
	MaxHeight int    `json:"-"`
	BitrateK  int    `json:"-"`
	Encoder   string `json:"-"`
	// Open counts the requests being answered right now (a remux or transcode is one long
	// request; direct play is many short ones).
	Open     int       `json:"-"`
	OpenedAt time.Time `json:"-"`
	LastAt   time.Time `json:"-"`
}

// Session is one device playing one item.
type Session struct {
	DeviceID   string
	UserID     string
	ScopeID    string
	ItemID     string
	SourceID   string
	PositionMs int64
	DurationMs int64
	Audio      *int
	Subtitle   *int
	Paused     bool
	StartedAt  time.Time
	ReportedAt time.Time
	// Stream is how the item is reaching the device; nil when no stream request for it has
	// been seen (the server restarted mid-play, or the player has the whole file buffered).
	Stream *Stream
}

type session struct {
	Session
	saidPaused bool // the last report carried Paused
}

// Board holds the sessions and streams.
type Board struct {
	mu       sync.Mutex
	sessions map[string]*session // by device
	streams  map[string]*Stream  // by device + item
	now      func() time.Time
}

func New() *Board {
	return &Board{sessions: map[string]*session{}, streams: map[string]*Stream{}, now: time.Now}
}

func streamKey(deviceID, itemID string) string { return deviceID + "\x00" + itemID }

// Report records where a player is. A device plays one thing at a time: a report for
// another item replaces the session.
func (b *Board) Report(deviceID string, r Report) {
	if b == nil || deviceID == "" || r.ItemID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	s := b.sessions[deviceID]
	if s == nil || s.ItemID != r.ItemID || b.gone(s, now) {
		s = &session{Session: Session{DeviceID: deviceID, ItemID: r.ItemID, StartedAt: now}}
		b.sessions[deviceID] = s
	}
	s.UserID = r.UserID
	if r.ScopeID != "" {
		s.ScopeID = r.ScopeID
	}
	if r.SourceID != "" {
		s.SourceID = r.SourceID
	}
	if r.DurationMs > 0 {
		s.DurationMs = r.DurationMs
	}
	if r.Audio != nil {
		s.Audio = r.Audio
	}
	if r.Subtitle != nil {
		s.Subtitle = r.Subtitle
	}
	s.PositionMs = r.PositionMs
	s.ReportedAt = now
	s.saidPaused = r.Paused != nil
	s.Paused = r.Paused != nil && *r.Paused
}

// SetState records that a player paused or went on, when it has no new position to give.
// Ignored when the device is not playing that item.
func (b *Board) SetState(deviceID, itemID string, paused bool) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if s := b.sessions[deviceID]; s != nil && s.ItemID == itemID && !b.gone(s, now) {
		s.Paused, s.saidPaused, s.ReportedAt = paused, true, now
	}
}

// Stop ends a device's session: the player closed. itemID "" ends whatever it plays; a
// stop for an item the device has already moved on from is ignored.
func (b *Board) Stop(deviceID, itemID string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.sessions[deviceID]; s != nil && (itemID == "" || s.ItemID == itemID) {
		delete(b.sessions, deviceID)
	}
}

// Forget drops everything about a device (it was signed out).
func (b *Board) Forget(deviceID string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, deviceID)
	for k := range b.streams {
		if len(k) > len(deviceID) && k[:len(deviceID)+1] == deviceID+"\x00" {
			delete(b.streams, k)
		}
	}
}

// Opened records a stream request being answered and returns what to call when the
// answer ends. The newest way an item is sent to a device is the one shown: a player
// that changes quality opens a new stream and lets the old one go.
func (b *Board) Opened(deviceID string, st Stream) (done func()) {
	if b == nil || deviceID == "" || st.ItemID == "" {
		return func() {}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.prune(now)
	key := streamKey(deviceID, st.ItemID)
	cur := b.streams[key]
	if cur == nil || !sameStream(cur, &st) {
		st.Open, st.OpenedAt = 0, now
		cur = &st
		b.streams[key] = cur
	}
	cur.Open++
	cur.LastAt = now
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if cur.Open > 0 {
			cur.Open--
		}
		cur.LastAt = b.now()
	}
}

func sameStream(a, b *Stream) bool {
	return a.Method == b.Method && a.SourceID == b.SourceID && a.AudioIndex == b.AudioIndex && a.Web == b.Web && a.MaxHeight == b.MaxHeight && a.BitrateK == b.BitrateK && a.Encoder == b.Encoder
}

// gone: the session has been quiet too long to list.
func (b *Board) gone(s *session, now time.Time) bool {
	quiet := now.Sub(s.ReportedAt)
	if s.saidPaused && s.Paused {
		return quiet > PausedFor
	}
	return quiet > QuietFor
}

// prune forgets streams nobody reads and no session points at. Called under the lock.
func (b *Board) prune(now time.Time) {
	for k, st := range b.streams {
		if st.Open == 0 && now.Sub(st.LastAt) > streamKept {
			delete(b.streams, k)
		}
	}
}

// List is the sessions to show, the playing ones first, then the most recently started.
func (b *Board) List() []Session {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	out := []Session{}
	for id, s := range b.sessions {
		if b.gone(s, now) {
			delete(b.sessions, id)
			continue
		}
		v := s.Session
		// Quiet past the reporting interval and it never said why: shown as paused.
		if !v.Paused && now.Sub(s.ReportedAt) > PlayingFor {
			v.Paused = true
		}
		if st := b.streams[streamKey(id, s.ItemID)]; st != nil && (s.SourceID == "" || st.SourceID == s.SourceID) {
			c := *st
			v.Stream = &c
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Paused != out[j].Paused {
			return !out[i].Paused
		}
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].DeviceID < out[j].DeviceID
	})
	return out
}

// Converting counts the transcodes being sent right now.
func (b *Board) Converting() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, st := range b.streams {
		if st.Method == Transcode {
			n += st.Open
		}
	}
	return n
}
