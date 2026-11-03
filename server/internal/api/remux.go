package api

import (
	"log/slog"
	"math"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

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
	cmd := exec.CommandContext(r.Context(), s.ff.FFmpeg, transcode.RemuxArgs(req)...)
	stderr := &tailBuffer{max: 64 << 10} // only the end is logged; a chatty ffmpeg cannot grow it
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	began := time.Now()
	if err := cmd.Start(); err != nil {
		writeError(w, 500, "ffmpeg: "+err.Error())
		return
	}
	slog.Info("remux: start", "source", src.ID, "audio", req.AudioIndex, "codec", req.Codec, "start", start)
	w.Header().Set("Content-Type", "video/x-matroska")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	buf := make([]byte, 256*1024)
	flusher, _ := w.(http.Flusher)
	var sent int64
	for {
		n, rerr := stdout.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				break // the player went away (stop or seek); the context kills ffmpeg
			}
			sent += int64(n)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}
	_ = cmd.Wait()
	if r.Context().Err() == nil && stderr.Len() > 0 {
		slog.Warn("remux: ffmpeg", "source", src.ID, "err", strings.TrimSpace(tailString(stderr.String(), 600)))
	}
	slog.Info("remux: end", "source", src.ID, "bytes", sent, "took", time.Since(began).Round(time.Second))
}
