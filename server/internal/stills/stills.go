// Package stills makes a thumbnail for an episode or film that has no still of its own:
// hand-made shows (One Pace, fan edits) ship .nfo files but rarely a picture per episode,
// and a title no provider knows has nothing at all. One frame a fifth of the way into the
// file, 640 px wide, is cached and recorded like any other image; a provider still found
// later replaces it.
package stills

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

// locks serialises grabs per item, so a page of cards asking at once runs ffmpeg once.
var locks sync.Map

// slots caps how many ffmpeg frame grabs run at once across all items: a page of a
// hundred thumb-less episodes must not start a hundred decoders.
var slots = make(chan struct{}, 2)

// failures remembers items whose grab failed, so a broken file is not re-decoded on
// every page view. Entries expire after failTTL.
var (
	failMu   sync.Mutex
	failures = map[string]time.Time{}
)

const failTTL = time.Hour

func failedRecently(id string) bool {
	failMu.Lock()
	defer failMu.Unlock()
	at, ok := failures[id]
	if ok && time.Since(at) > failTTL {
		delete(failures, id)
		return false
	}
	return ok
}

func markFailed(id string) {
	failMu.Lock()
	defer failMu.Unlock()
	if len(failures) > 10000 { // bounded: drop the expired, or everything if none are
		for k, at := range failures {
			if time.Since(at) > failTTL {
				delete(failures, k)
			}
		}
		if len(failures) > 10000 {
			failures = map[string]time.Time{}
		}
	}
	failures[id] = time.Now()
}

// Grab makes and records the thumb for id when it is an episode or movie with a file and
// no thumb yet; it returns the thumb either way, or nil when there is nothing to grab from.
func Grab(ctx context.Context, ff *ffmpeg.FFmpeg, st *store.Store, cfg *config.Config, id string) (*store.ItemImage, error) {
	if ff == nil || failedRecently(id) {
		return nil, nil
	}
	it, err := st.GetItem(ctx, id)
	if err != nil || it == nil || (it.Type != model.TypeEpisode && it.Type != model.TypeMovie) {
		return nil, err
	}
	srcs, err := st.SourcesForItem(ctx, id)
	if err != nil || len(srcs) == 0 || len(srcs[0].Parts) == 0 {
		return nil, err
	}
	src := srcs[0]
	mu, _ := locks.LoadOrStore(id, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	defer locks.Delete(id)
	if existing, err := st.GetItemImage(ctx, id, "thumb"); err != nil || existing != nil {
		return existing, err
	}
	video, err := safefs.MediaPath(src.Parts[0].Path) // never decode a file a symlink leads out of media
	if err != nil {
		markFailed(id)
		return nil, err
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	at := src.DurationMs / 5 / 1000
	if at < 5 {
		at = 5
	}
	dir := filepath.Join(cfg.ImagesDir(), id)
	if err := safefs.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "thumb.jpg")
	tmp := filepath.Join(dir, "thumb.frame.tmp.jpg")
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := ff.Run(ctx, dir, "-y", "-v", "error", "-ss", strconv.FormatInt(at, 10), "-i", video, "-an", "-sn", "-dn",
		"-frames:v", "1", "-vf", "scale=640:-2", "-q:v", "4", "-f", "image2", tmp); err != nil {
		safefs.Remove(tmp)
		if parent.Err() == nil { // the viewer leaving is not the file's fault
			markFailed(id)
		}
		return nil, err
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return nil, err
	}
	imgCfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		safefs.Remove(tmp)
		markFailed(id)
		return nil, err
	}
	if err := safefs.Rename(tmp, path); err != nil {
		safefs.Remove(tmp)
		return nil, err
	}
	img := store.ItemImage{ItemID: id, Type: "thumb", Path: path, Width: imgCfg.Width, Height: imgCfg.Height, SourceURL: "frame://" + src.ID}
	if err := st.SetItemImage(ctx, img); err != nil {
		return nil, err
	}
	return &img, nil
}
