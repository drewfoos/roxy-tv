package api

import (
	"context"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ffmpegWaitDelay bounds the wait for ffmpeg once it has been told to stop.
const ffmpegWaitDelay = 5 * time.Second

// pipeFFmpeg runs ffmpeg with args and streams its stdout to the player as Matroska,
// returning once ffmpeg has exited. what ("transcode", "remux") and attrs label the logs.
//
// A failed write (the player stopped or seeked away) kills ffmpeg at once. It used to
// only stop reading and then wait for ffmpeg, which relied on the request context being
// cancelled: when it was not, ffmpeg blocked writing into the full pipe, the wait never
// returned, and the transcode slot stayed taken; a few of those and no one could start
// a transcode. WaitDelay caps the wait in any case.
func (s *Server) pipeFFmpeg(w http.ResponseWriter, r *http.Request, what string, args []string, attrs ...any) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, s.ff.FFmpeg, args...)
	cmd.WaitDelay = ffmpegWaitDelay
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
	slog.Info(what+": start", attrs...)
	w.Header().Set("Content-Type", "video/x-matroska")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	buf := make([]byte, 256*1024)
	flusher, _ := w.(http.Flusher)
	var sent int64
	playerGone := false
	for {
		n, rerr := stdout.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				playerGone = true
				cancel() // stop ffmpeg now; nothing reads its output any more
				break
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
	if !playerGone && r.Context().Err() == nil && stderr.Len() > 0 {
		slog.Warn(what+": ffmpeg", append(attrs, "err", strings.TrimSpace(tailString(stderr.String(), 600)))...)
	}
	slog.Info(what+": end", append(attrs, "bytes", sent, "took", time.Since(began).Round(time.Second))...)
}
