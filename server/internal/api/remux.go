package api

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/transcode"
)

// remuxSlots caps simultaneous remuxes; each is a copy, so more than transcodes.
var remuxSlots = make(chan struct{}, 8)

// GET /v1/sources/{id}/remux?audio=<stream index>&start=<seconds>
// The file with its video copied and one audio track, as Matroska: for Roxy Web, whose
// browser decodes the video itself but cannot switch MKV audio tracks or decode AC3,
// E-AC3, DTS or TrueHD. Timestamps restart at zero at `start`; the player adds the
// offset back. Auth: Bearer, ?api_key= or a media token (?mt=).
func (s *Server) remuxStream(w http.ResponseWriter, r *http.Request) {
	src, req, ok := s.remuxRequest(w, r)
	if !ok {
		return
	}
	select {
	case remuxSlots <- struct{}{}:
		defer func() { <-remuxSlots }()
	default:
		writeError(w, 503, "the server is already remuxing as much as it can")
		return
	}
	s.pipeFFmpeg(w, r, "remux", ffmpeg.LocalInputs(transcode.RemuxArgs(req)), "source", src.ID, "audio", req.AudioIndex, "codec", req.Codec, "start", req.StartSec)
}

// GET /v1/sources/{id}/remux/start?start=<seconds> -> {"startSec": <seconds>}
// Where the remux of the same start really begins in the file (its zero): the keyframe
// ffmpeg's seek lands on, which the video copy cannot cut past. Roxy Web adds this, not
// `start`, to the stream's clock; with `start` its subtitles ran early by the gap (up to
// a GOP, 10 s and more). Falls back to `start` when the file cannot be read that way.
func (s *Server) remuxStart(w http.ResponseWriter, r *http.Request) {
	_, req, ok := s.remuxRequest(w, r)
	if !ok {
		return
	}
	at := req.StartSec
	if at > 0 && s.ff != nil {
		out, err := s.ff.Run(r.Context(), "", transcode.RemuxStartArgs(req.Path, req.StartSec)...)
		if k, ok := transcode.ParseRemuxStart(out); err == nil && ok {
			at = min(max(k, 0), req.StartSec)
		} else {
			slog.Warn("remux start: could not find the keyframe", "path", req.Path, "start", req.StartSec, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"startSec": at})
}

// remuxRequest reads a remux's source, start and audio track, answering the error itself.
func (s *Server) remuxRequest(w http.ResponseWriter, r *http.Request) (*model.MediaSource, transcode.RemuxRequest, bool) {
	var req transcode.RemuxRequest
	src, err := s.store.GetSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return nil, req, false
	}
	if src == nil || len(src.Parts) == 0 {
		writeError(w, 404, "source not found")
		return nil, req, false
	}
	q := r.URL.Query()
	start, _ := strconv.ParseFloat(q.Get("start"), 64)
	if start < 0 || math.IsNaN(start) || math.IsInf(start, 0) || start > 7*24*3600 {
		writeError(w, 400, "bad start")
		return nil, req, false
	}
	video, ok := mediaFile(w, src.Parts[0].Path)
	if !ok {
		return nil, req, false
	}
	req = transcode.RemuxRequest{Path: video, StartSec: start, AudioIndex: -1}
	if a := q.Get("audio"); a != "" {
		idx, err := strconv.Atoi(a)
		if err != nil {
			writeError(w, 400, "bad audio index")
			return nil, req, false
		}
		var track *model.MediaStream
		for i := range src.Streams {
			if st := &src.Streams[i]; st.Index == idx && st.Type == model.StreamAudio && !st.IsExternal {
				track = st
				break
			}
		}
		if track == nil {
			writeError(w, 404, "no such audio track")
			return nil, req, false
		}
		req.AudioIndex, req.Codec, req.Channels = track.Index, track.Codec, track.Channels
	} else {
		for i := range src.Streams {
			if st := &src.Streams[i]; st.Type == model.StreamAudio && !st.IsExternal {
				req.AudioIndex, req.Codec, req.Channels = st.Index, st.Codec, st.Channels
				break
			}
		}
	}
	return src, req, true
}
