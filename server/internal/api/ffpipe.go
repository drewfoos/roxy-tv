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

// pipeStall is how long the player may take to accept the next piece of the stream. One
// that stops reading but keeps the connection open (paused with its buffer full, a phone
// whose app was put away) used to hold ffmpeg, and the transcode slot, for as long as
// the connection lived: four of those and no one could transcode. Past it the stream is
// ended; Roxy's player reopens it where it was when playback goes on (a stream that ends
// well before the end of the file is a dropped connection to it). A variable for tests.
var pipeStall = 10 * time.Minute

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
	rc := http.NewResponseController(w)
	var sent int64
	playerGone := false
	for {
		n, rerr := stdout.Read(buf)
		if n > 0 {
			_ = rc.SetWriteDeadline(time.Now().Add(pipeStall)) // not supported by every writer: then no limit
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
