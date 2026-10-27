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

// Grab makes and records the thumb for id when it is an episode or movie with a file and
// no thumb yet; it returns the thumb either way, or nil when there is nothing to grab from.
func Grab(ctx context.Context, ff *ffmpeg.FFmpeg, st *store.Store, cfg *config.Config, id string) (*store.ItemImage, error) {
	if ff == nil {
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := ff.Run(ctx, dir, "-y", "-v", "error", "-ss", strconv.FormatInt(at, 10), "-i", src.Parts[0].Path, "-an", "-sn", "-dn",
		"-frames:v", "1", "-vf", "scale=640:-2", "-q:v", "4", "-f", "image2", tmp); err != nil {
		safefs.Remove(tmp)
		return nil, err
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return nil, err
	}
	imgCfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		safefs.Remove(tmp)
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
