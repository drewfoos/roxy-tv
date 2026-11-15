package api

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/store"
	"github.com/roxy-media/roxy/server/internal/trickplay"
)

// backgroundWork counts the goroutines of the work the router starts for itself:
// Discover's tracker, pack sweep and ten-minute pass, and the character lookups.
func backgroundWork() int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	count := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "internal/discover.(*Service)") || strings.Contains(g, "startCharacterWorker") {
			count++
		}
	}
	return count
}

// The finding: this work was started on a context that never ends, so at shutdown it was
// still running (asking Sonarr, writing) while the database was closed under it.
func TestBackgroundWorkStopsWithTheServer(t *testing.T) {
	data := t.TempDir()
	st, err := store.Open(filepath.Join(data, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := &config.Config{DataDir: data}
	before := backgroundWork()

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	NewRouterContext(ctx, cfg, st, library.New(st, nil, cfg), &ffmpeg.FFmpeg{}, auth.New(st), metadata.New(st, cfg), trickplay.New(st, &ffmpeg.FFmpeg{}, cfg), nil)
	running := backgroundWork()
	if running <= before {
		t.Fatalf("the router started no background work (%d before, %d after): this test watches nothing", before, running)
	}

	stop()
	deadline := time.Now().Add(5 * time.Second)
	for backgroundWork() > before {
		if time.Now().After(deadline) {
			t.Fatalf("%d background goroutines still run after the server's context ended (%d before it started)", backgroundWork(), before)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
