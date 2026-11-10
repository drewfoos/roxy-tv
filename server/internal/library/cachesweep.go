package library

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// cacheSettle is how long a cache entry is left alone after it was last written: an
// ingest fills a source's folders before the source is recorded.
const cacheSettle = time.Hour

// SweepCaches removes what the caches keep for titles and files that are no longer in
// the library: images are kept per item, subtitles, fonts and seek previews per source,
// and nothing removed them when the item or the source went (a few hundred megabytes
// had gathered). It returns how many entries it removed.
//
// It is careful rather than thorough. The folders are listed before the database is
// read, so nothing made during the sweep is judged; an entry written within the last
// hour is left; so is any name that is not an id (profile pictures, whatever an admin
// put there). If the database cannot be read or lists nothing, or more than half of a
// cache would go, that is taken as a sign something is wrong and nothing is removed.
func (l *Library) SweepCaches(ctx context.Context) (int, error) {
	if l.cfg == nil {
		return 0, nil
	}
	type cache struct {
		dir     string
		sources bool // kept per source (else per item)
		names   []string
	}
	caches := []*cache{
		{dir: l.cfg.ImagesDir()},
		{dir: l.cfg.SubtitlesDir(), sources: true},
		{dir: l.cfg.AttachmentsDir(), sources: true},
		{dir: l.cfg.TrickplayDir(), sources: true},
	}
	for _, c := range caches {
		entries, err := os.ReadDir(c.dir)
		if err != nil {
			continue // not made yet
		}
		for _, e := range entries {
			c.names = append(c.names, e.Name())
		}
	}
	items, sources, err := l.store.CacheOwnerIDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 || len(sources) == 0 {
		return 0, nil
	}
	removed := 0
	for _, c := range caches {
		owners := items
		if c.sources {
			owners = sources
		}
		var gone []string
		ids := 0
		for _, name := range c.names {
			// Seek previews also keep <source>.tmp (a run in progress or killed) and
			// <source>.failed (see trickplay).
			id := strings.TrimSuffix(strings.TrimSuffix(name, ".tmp"), ".failed")
			if _, err := uuid.Parse(id); err != nil {
				continue
			}
			ids++
			if !owners[id] {
				gone = append(gone, name)
			}
		}
		if len(gone)*2 > ids {
			slog.Warn("cache: more than half of a cache belongs to nothing in the library; left alone", "dir", c.dir, "entries", ids, "unowned", len(gone))
			continue
		}
		for _, name := range gone {
			if ctx.Err() != nil {
				return removed, ctx.Err()
			}
			path := filepath.Join(c.dir, name)
			fi, err := os.Lstat(path)
			if err != nil || time.Since(fi.ModTime()) < cacheSettle {
				continue
			}
			if err := safefs.RemoveAll(path); err != nil {
				slog.Warn("cache: removing an unowned entry", "path", path, "err", err)
				continue
			}
			removed++
		}
	}
	if removed > 0 {
		slog.Info("cache: removed what was kept for titles and files no longer in the library", "entries", removed)
	}
	return removed, nil
}
