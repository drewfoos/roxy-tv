package api

import (
	"context"
	"encoding/json"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/transcode"
)

// transcodeSlots caps simultaneous transcodes; a GPU handles a few, software x264 fewer.
var transcodeSlots = make(chan struct{}, 4)

// GET /v1/sources/{id}/transcode?height=720&bitrate=4000&start=123.4
// Streams the source re-encoded as Matroska (H.264 + AAC, subtitles, fonts and chapters
// copied) for Roxy's players; see internal/transcode. Auth: Bearer or ?api_key=.
func (s *Server) transcodeStream(w http.ResponseWriter, r *http.Request) {
	src, err := s.store.GetSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if src == nil || len(src.Parts) == 0 {
		writeError(w, 404, "source not found")
		return
	}
	q := r.URL.Query()
	height, _ := strconv.Atoi(q.Get("height"))
	bitrate, _ := strconv.Atoi(q.Get("bitrate"))
	start, _ := strconv.ParseFloat(q.Get("start"), 64)
	if bitrate < 0 || bitrate > 80000 || height < 0 || height > 4320 || start < 0 || math.IsNaN(start) || math.IsInf(start, 0) || start > 7*24*3600 {
		writeError(w, 400, "bad height, bitrate or start")
		return
	}
	video, ok := mediaFile(w, src.Parts[0].Path)
	if !ok {
		return
	}
	req := transcode.Request{Path: video, StartSec: start, MaxHeight: height, BitrateK: bitrate, Container: src.Container}
	// web=1: one browser-safe audio track (audio=<stream index>, else the first), no
	// subtitles, timestamps from zero at start. See transcode.Request.Web.
	if q.Get("web") == "1" {
		req.Web = true
		want, hasWant := strconv.Atoi(q.Get("audio"))
		for i := range src.Streams {
			st := &src.Streams[i]
			if st.Type != model.StreamAudio || st.IsExternal {
				continue
			}
			if hasWant == nil && st.Index != want {
				continue
			}
			req.WebAudio = &transcode.WebAudioTrack{Index: st.Index, Codec: st.Codec, Channels: st.Channels}
			break
		}
		if hasWant == nil && req.WebAudio == nil {
			writeError(w, 404, "no such audio track")
			return
		}
	}
	videoSeen := false
	for _, st := range src.Streams {
		switch {
		case st.Type == model.StreamVideo && !videoSeen:
			videoSeen = true
			req.SrcHeight = st.Height
			req.SrcHDR = st.ColorTransfer == "smpte2084" || st.ColorTransfer == "arib-std-b67"
		case st.Type == model.StreamAudio && !st.IsExternal:
			req.Audio = append(req.Audio, transcode.AudioTrack{Codec: st.Codec, Channels: st.Channels, Bitrate: st.Bitrate})
		case st.Type == model.StreamSubtitle && !st.IsExternal:
			req.Subtitles = append(req.Subtitles, st.Codec)
		}
	}
	select {
	case transcodeSlots <- struct{}{}:
		defer func() { <-transcodeSlots }()
	default:
		writeError(w, 503, "the server is already transcoding as much as it can")
		return
	}
	args := s.transcoder.Args(req)
	s.pipeFFmpeg(w, r, "transcode", ffmpeg.LocalInputs(args), "source", src.ID, "encoder", s.transcoder.Encoder(), "height", height, "kbps", bitrate, "start", start)
}

// GET /v1/system/transcoding -> {encoder}: which encoder the server uses.
func (s *Server) transcodingInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"encoder": s.transcoder.Encoder()})
}

// TranscodingSettings is the "transcoding" row of the settings table. When it is
// saved it wins over ROXY_HWACCEL, which only sets the first-run default.
type TranscodingSettings struct {
	HWAccel string `json:"hwAccel"` // auto | nvenc | qsv | vaapi | amf | software
}

// loadTranscodingSetting applies a saved choice at startup.
func (s *Server) loadTranscodingSetting() {
	var cfg TranscodingSettings
	ok, err := s.store.GetSetting(context.Background(), "transcoding", &cfg)
	if err != nil || !ok || cfg.HWAccel == "" {
		return
	}
	if _, err := s.transcoder.SetChoice(cfg.HWAccel); err != nil {
		// The GPU went away (new machine, container without the device): keep serving.
		slog.Warn("transcode: saved encoder unavailable, using auto", "setting", cfg.HWAccel, "err", err)
		_, _ = s.transcoder.SetChoice(transcode.Auto)
	}
}

func (s *Server) transcodingSettingsJSON() map[string]any {
	avail := s.transcoder.Available()
	// Auto's label names what it picks on this machine: the first working GPU, else the CPU.
	best := transcode.Software
	for _, e := range transcode.Candidates() {
		if avail[e] {
			best = e
			break
		}
	}
	options := []map[string]any{{"id": transcode.Auto, "label": "Auto (" + best.Label() + ")", "suggested": string(best), "available": true}}
	for _, e := range append(transcode.Candidates(), transcode.Software) {
		options = append(options, map[string]any{"id": string(e), "label": e.Label(), "available": avail[e]})
	}
	return map[string]any{
		"hwAccel": s.transcoder.Choice(),
		"encoder": s.transcoder.Encoder(),
		"options": options,
	}
}

// GET /v1/settings/transcoding (admin) -> {hwAccel, encoder, options: [{id, label, available}]}.
// The first call test-encodes with each GPU encoder (a few seconds); later calls are instant.
func (s *Server) transcodingSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	writeJSON(w, 200, s.transcodingSettingsJSON())
}

// PUT /v1/settings/transcoding {hwAccel} -> same as GET. A hardware encoder that fails
// its test encode is refused (422) and nothing is saved. Running transcodes keep their
// encoder; the next one uses the new choice.
func (s *Server) transcodingSettingsPut(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body TranscodingSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, 400, "bad body: "+err.Error())
		return
	}
	choice := transcode.NormalizeChoice(body.HWAccel)
	if choice == "" {
		writeError(w, 400, "unknown encoder "+body.HWAccel)
		return
	}
	if _, err := s.transcoder.SetChoice(choice); err != nil {
		writeError(w, 422, err.Error())
		return
	}
	if err := s.store.SetSetting(r.Context(), "transcoding", TranscodingSettings{HWAccel: choice}); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	slog.Info("transcode: setting saved", "hwAccel", choice, "encoder", s.transcoder.Encoder())
	writeJSON(w, 200, s.transcodingSettingsJSON())
}

func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// tailBuffer is an io.Writer that keeps only the last max bytes written (ffmpeg's stderr
// for a long stream can be endless; only its end is ever logged).
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) >= t.max {
		t.buf = append(t.buf[:0], p[len(p)-t.max:]...)
		return n, nil
	}
	if over := len(t.buf) + len(p) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	t.buf = append(t.buf, p...)
	return n, nil
}

func (t *tailBuffer) Len() int       { return len(t.buf) }
func (t *tailBuffer) String() string { return string(t.buf) }
