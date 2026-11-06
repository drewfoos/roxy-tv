package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
)

// brokenPlayer takes the first write, then fails like a connection the player dropped.
type brokenPlayer struct {
	h      http.Header
	writes int
}

func (b *brokenPlayer) Header() http.Header {
	if b.h == nil {
		b.h = http.Header{}
	}
	return b.h
}
func (b *brokenPlayer) WriteHeader(int) {}
func (b *brokenPlayer) Write(p []byte) (int, error) {
	if b.writes++; b.writes > 1 {
		return 0, errors.New("write: broken pipe")
	}
	return len(p), nil
}

// The player goes away while the request context stays alive (as when the write fails
// before the server notices the disconnect): ffmpeg must be stopped and pipeFFmpeg return,
// so the handler frees its transcode slot. `yes` stands in for an ffmpeg that keeps
// writing; without the stop it fills the pipe and waits forever.
func TestPipeFFmpegStopsWhenPlayerGoes(t *testing.T) {
	if _, err := os.Stat("/usr/bin/yes"); err != nil {
		t.Skip("needs /usr/bin/yes")
	}
	s := &Server{ff: &ffmpeg.FFmpeg{FFmpeg: "/usr/bin/yes"}}
	req := httptest.NewRequest(http.MethodGet, "/v1/sources/x/transcode", nil)
	done := make(chan struct{})
	go func() {
		s.pipeFFmpeg(&brokenPlayer{}, req, "test", []string{"roxy"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("still waiting on ffmpeg after the player went away: the slot would stay taken")
	}
	if req.Context().Err() != nil {
		t.Fatal("the request context was cancelled; the test must not rely on that")
	}
}
