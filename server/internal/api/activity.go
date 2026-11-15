package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/activity"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/transcode"
)

// "Now playing" (admin): who is watching what, on which device, and how the server is
// sending it. See internal/activity for where it comes from.

// playDevice is the signed-in device a request plays for: the login's own, or the one a
// media token was made for.
func playDevice(p *model.Principal) string {
	if p == nil {
		return ""
	}
	if p.DeviceID != "" {
		return p.DeviceID
	}
	return p.MediaDevice
}

// streaming records a stream request for "Now playing" and returns what to call when
// its answer ends.
func (s *Server) streaming(r *http.Request, src *model.MediaSource, st activity.Stream) func() {
	st.ItemID, st.SourceID = src.ItemID, src.ID
	return s.board.Opened(playDevice(auth.PrincipalFrom(r.Context())), st)
}

// ActivePerson names a login or a profile.
type ActivePerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ActiveDevice is the device a session plays on. The address is left out on purpose:
// this page can end up on a stream, and where a device is says enough.
type ActiveDevice struct {
	Name    string `json:"name"`
	Client  string `json:"client,omitempty"`
	Version string `json:"version,omitempty"`
	Network string `json:"network,omitempty"` // home | tailscale | server | internet
}

// ActiveOutput is what a converted track is sent as.
type ActiveOutput struct {
	Codec    string `json:"codec"`
	Height   int    `json:"height,omitempty"`
	BitrateK int    `json:"bitrateK,omitempty"`
	Channels int    `json:"channels,omitempty"`
	// Encoder names what does the video conversion ("VAAPI (AMD / Intel on Linux)",
	// "Software (CPU)"); Hardware is whether that is a GPU.
	Encoder  string `json:"encoder,omitempty"`
	Hardware bool   `json:"hardware,omitempty"`
}

// ActiveTrack is one of the file's tracks as it plays. To is set when the server
// converts it, and nil when it goes out as it is in the file.
type ActiveTrack struct {
	Codec    string        `json:"codec"`
	Width    int           `json:"width,omitempty"`
	Height   int           `json:"height,omitempty"`
	BitDepth int           `json:"bitDepth,omitempty"`
	Range    string        `json:"range,omitempty"` // SDR | HDR10 | HLG | DOVI ...
	Channels int           `json:"channels,omitempty"`
	Language string        `json:"language,omitempty"`
	Title    string        `json:"title,omitempty"`
	External bool          `json:"external,omitempty"`
	SDH      bool          `json:"sdh,omitempty"`
	To       *ActiveOutput `json:"to,omitempty"`
}

// ActivePlay is how a session's file reaches its device.
type ActivePlay struct {
	// Method: direct | remux | transcode, or "" when no stream request has been seen for
	// the session (the server restarted mid-play).
	Method string `json:"method"`
	// Reason says in plain words why the file is not sent as it is.
	Reason string `json:"reason,omitempty"`
	// Sending: the server is answering a stream request for it right now.
	Sending   bool   `json:"sending"`
	Container string `json:"container,omitempty"`
	Bitrate   int64  `json:"bitrate,omitempty"` // the file's, bits per second
	Version   string `json:"version,omitempty"`
	// Video and Audio are the tracks playing. SubtitlesOff, or Subtitle, is the viewer's
	// choice when the player has said; neither when it has not.
	Video        *ActiveTrack `json:"video,omitempty"`
	Audio        *ActiveTrack `json:"audio,omitempty"`
	Subtitle     *ActiveTrack `json:"subtitle,omitempty"`
	SubtitlesOff bool         `json:"subtitlesOff,omitempty"`
}

// ActiveSession is one device playing one item.
type ActiveSession struct {
	ID         string `json:"id"`    // the device's id: one session per device
	State      string `json:"state"` // playing | paused
	PositionMs int64  `json:"positionMs"`
	DurationMs int64  `json:"durationMs"`
	// AgeMs: how long ago the position was reported. A playing session is that much
	// further on by now; the page counts on from it between its own refreshes.
	AgeMs     int64        `json:"ageMs"`
	StartedAt time.Time    `json:"startedAt"`
	Item      *model.Item  `json:"item"`
	User      ActivePerson `json:"user"`
	Profile   ActivePerson `json:"profile"`
	Device    ActiveDevice `json:"device"`
	Play      ActivePlay   `json:"play"`
}

// GET /v1/sessions/active (admin) -> {sessions: [ActiveSession], converting, convertingMax,
// encoder}: everything playing from this server right now, every login's.
func (s *Server) activeSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	ctx := r.Context()
	now := time.Now()
	out := []ActiveSession{}
	for _, se := range s.board.List() {
		it, err := s.store.GetItem(ctx, se.ItemID)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		if it == nil {
			continue // removed from the library while it played
		}
		one := []model.Item{*it}
		s.fillSeriesRefs(ctx, one)
		v := ActiveSession{ID: se.DeviceID, State: "playing", PositionMs: se.PositionMs, DurationMs: se.DurationMs, AgeMs: max(now.Sub(se.ReportedAt).Milliseconds(), 0), StartedAt: se.StartedAt, Item: &one[0]}
		if se.Paused {
			v.State = "paused"
		}
		if u, err := s.store.GetUser(ctx, se.UserID); err == nil && u != nil {
			v.User = ActivePerson{ID: u.ID, Name: u.Name}
		}
		if se.ScopeID != "" {
			if pr, err := s.store.GetProfile(ctx, se.ScopeID); err == nil && pr != nil {
				v.Profile = ActivePerson{ID: pr.ID, Name: pr.Name}
			}
		}
		if d, err := s.store.GetDevice(ctx, se.DeviceID); err == nil && d != nil {
			v.Device = ActiveDevice{Name: d.Name, Client: d.Client, Version: d.ClientVersion, Network: networkOf(d.LastIP)}
		}
		v.Play = s.activePlay(ctx, se)
		if v.DurationMs <= 0 && it.RuntimeMs != nil {
			v.DurationMs = *it.RuntimeMs
		}
		out = append(out, v)
	}
	enc := s.transcoder.Encoder()
	writeJSON(w, 200, map[string]any{
		"sessions":      out,
		"converting":    s.board.Converting(),
		"convertingMax": cap(transcodeSlots),
		"encoder":       enc.Label(),
	})
}

// activePlay describes how a session's file is reaching its device.
func (s *Server) activePlay(ctx context.Context, se activity.Session) ActivePlay {
	var play ActivePlay
	st := se.Stream
	sourceID := se.SourceID
	if sourceID == "" && st != nil {
		sourceID = st.SourceID
	}
	var src *model.MediaSource
	if sourceID != "" {
		src, _ = s.store.GetSource(ctx, sourceID)
	}
	if src == nil {
		if srcs, err := s.store.SourcesForItem(ctx, se.ItemID); err == nil && len(srcs) == 1 {
			src = &srcs[0] // one version: it is the one playing
		}
	}
	if st != nil {
		play.Method, play.Sending = st.Method, st.Open > 0
	}
	if src == nil {
		return play
	}
	play.Container, play.Bitrate, play.Version = src.Container, src.Bitrate, src.VersionLabel

	var video, audio, firstAudio, subtitle *model.MediaStream
	wantAudio := -1
	switch {
	case se.Audio != nil:
		wantAudio = *se.Audio
	case st != nil && st.AudioIndex >= 0:
		wantAudio = st.AudioIndex
	}
	for i := range src.Streams {
		t := &src.Streams[i]
		switch t.Type {
		case model.StreamVideo:
			if video == nil {
				video = t
			}
		case model.StreamAudio:
			if t.IsExternal {
				continue
			}
			if firstAudio == nil || (t.IsDefault && !firstAudio.IsDefault) {
				firstAudio = t
			}
			if t.Index == wantAudio {
				audio = t
			}
		case model.StreamSubtitle:
			if se.Subtitle != nil && t.Index == *se.Subtitle {
				subtitle = t
			}
		}
	}
	if audio == nil {
		audio = firstAudio
	}
	if video != nil {
		play.Video = &ActiveTrack{Codec: video.Codec, Width: video.Width, Height: video.Height, BitDepth: video.BitDepth, Range: string(video.RangeType)}
	}
	if audio != nil {
		play.Audio = &ActiveTrack{Codec: audio.Codec, Channels: audio.Channels, Language: audio.Language, Title: audio.Title}
	}
	switch {
	case se.Subtitle != nil && *se.Subtitle == SubtitlesOff:
		play.SubtitlesOff = true
	case subtitle != nil:
		lang := subtitle.DetectedLanguage
		if lang == "" {
			lang = subtitle.Language
		}
		play.Subtitle = &ActiveTrack{Codec: subtitle.Codec, Language: lang, Title: subtitle.Title, External: subtitle.IsExternal, SDH: subtitle.IsHearingImpaired}
	}
	if st == nil {
		return play
	}

	aacFor := func(t *model.MediaStream) *ActiveOutput {
		ch := 2
		if t.Channels > 2 && !(st.Method == activity.Transcode && !st.Web && lowQuality(st)) {
			ch = 6
		}
		return &ActiveOutput{Codec: "aac", Channels: ch}
	}
	switch st.Method {
	case activity.Remux:
		if audio != nil && !transcode.BrowserAudio(audio.Codec) {
			play.Audio.To = aacFor(audio)
			play.Reason = fmt.Sprintf("Browsers cannot play %s audio, so it is converted to AAC. The video is sent untouched.", codecName(audio.Codec))
		} else {
			play.Reason = "A browser can only play a file's first audio track, so the chosen one is repackaged with the video. Nothing is re-encoded."
		}
	case activity.Transcode:
		enc := transcode.Encoder(st.Encoder)
		outHeight := 0
		if video != nil {
			outHeight = video.Height
			if st.MaxHeight > 0 && (outHeight == 0 || st.MaxHeight < outHeight) {
				outHeight = st.MaxHeight
			}
			play.Video.To = &ActiveOutput{Codec: "h264", Height: outHeight, BitrateK: st.BitrateK, Encoder: enc.Label(), Hardware: enc != transcode.Software}
		}
		if audio != nil && !transcode.CopiesAudio(st.MaxHeight, st.BitrateK, st.Web, transcode.AudioTrack{Codec: audio.Codec, Channels: audio.Channels, Bitrate: audio.Bitrate}) {
			play.Audio.To = aacFor(audio)
		}
		switch {
		case video != nil && st.MaxHeight > 0 && video.Height > st.MaxHeight:
			play.Reason = fmt.Sprintf("The player asked for %dp; the file is %dp.", st.MaxHeight, video.Height)
		case st.BitrateK > 0:
			play.Reason = fmt.Sprintf("The player asked for a quality of %s.", mbps(st.BitrateK))
		default:
			play.Reason = "The player asked for a converted stream."
		}
	}
	return play
}

func lowQuality(st *activity.Stream) bool {
	return (st.MaxHeight > 0 && st.MaxHeight <= 480) || (st.BitrateK > 0 && st.BitrateK < 2500)
}

func mbps(kbps int) string {
	if kbps >= 1000 {
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(kbps)/1000), ".0") + " Mbps"
	}
	return fmt.Sprintf("%d kbps", kbps)
}

// codecName is a codec as people write it.
func codecName(c string) string {
	switch strings.ToLower(c) {
	case "eac3":
		return "E-AC3"
	case "truehd":
		return "TrueHD"
	case "dts":
		return "DTS"
	case "ac3":
		return "AC3"
	}
	return strings.ToUpper(c)
}

// nowPlaying passes a progress report on to "Now playing". replayed: a report a phone made
// offline and sends now, which says nothing about what is playing at the moment.
func (s *Server) nowPlaying(p *model.Principal, itemID, sourceID string, positionMs, durationMs int64, audio, subtitle *int, state string, replayed bool) {
	if replayed || p.DeviceID == "" {
		return
	}
	if state == "stopped" {
		s.board.Stop(p.DeviceID, itemID)
		return
	}
	rep := activity.Report{UserID: p.UserID, ScopeID: p.ScopeID, ItemID: itemID, SourceID: sourceID, PositionMs: positionMs, DurationMs: durationMs, Audio: audio, Subtitle: subtitle}
	if state == "playing" || state == "paused" {
		paused := state == "paused"
		rep.Paused = &paused
	}
	s.board.Report(p.DeviceID, rep)
}

// POST /v1/me/playing {itemId, state: playing | paused | stopped} -> 204. What a player
// is doing, sent when it has no new position to save with it (paused where it last
// reported, closed after its last report). Only "Now playing" hears it.
func (s *Server) playingState(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	var body struct {
		ItemID string `json:"itemId"`
		State  string `json:"state"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.ItemID == "" {
		writeError(w, 400, "invalid request body")
		return
	}
	if body.State != "playing" && body.State != "paused" && body.State != "stopped" {
		writeError(w, 400, "state must be playing, paused or stopped")
		return
	}
	if p.DeviceID != "" {
		id := body.ItemID
		if it, err := s.store.GetItemOrMoved(r.Context(), id); err == nil && it != nil {
			id = it.ID
		}
		if body.State == "stopped" {
			s.board.Stop(p.DeviceID, id)
		} else {
			s.board.SetState(p.DeviceID, id, body.State == "paused")
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
