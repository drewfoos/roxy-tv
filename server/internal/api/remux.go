package api

import (
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"math"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
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
	start, _ := strconv.ParseFloat(q.Get("start"), 64)
	if start < 0 || math.IsNaN(start) || math.IsInf(start, 0) || start > 7*24*3600 {
		writeError(w, 400, "bad start")
		return
	}
	video, ok := mediaFile(w, src.Parts[0].Path)
	if !ok {
		return
	}
	req := transcode.RemuxRequest{Path: video, StartSec: start, AudioIndex: -1}
	if a := q.Get("audio"); a != "" {
		idx, err := strconv.Atoi(a)
		if err != nil {
			writeError(w, 400, "bad audio index")
			return
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
			return
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
	select {
	case remuxSlots <- struct{}{}:
		defer func() { <-remuxSlots }()
	default:
		writeError(w, 503, "the server is already remuxing as much as it can")
		return
	}
	s.pipeFFmpeg(w, r, "remux", ffmpeg.LocalInputs(transcode.RemuxArgs(req)), "source", src.ID, "audio", req.AudioIndex, "codec", req.Codec, "start", start)
}
