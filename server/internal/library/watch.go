package library

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchQuiet is how long a library must be still after a change before it is rescanned:
// a file being copied keeps changing, and must not be ingested half written.
var watchQuiet = 30 * time.Second

// watchRefresh is how often the watcher re-reads the library list: libraries added since,
// or whose folder was missing (a drive not mounted yet), start being watched; removed ones
// stop.
var watchRefresh = time.Minute

// watchRelevant are the files whose arrival, change or removal a scan cares about.
func watchRelevant(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return videoExts[ext] || subtitleExts[ext] || ext == ".nfo" || ext == ".ignore" || ext == ".roxyignore" ||
		strings.EqualFold(filepath.Base(name), ".ignore") || strings.EqualFold(filepath.Base(name), ".roxyignore")
}

// Watch rescans when files change in a library folder, as Jellyfin's library monitor does:
// a file copied in by hand (or by anything that sends no webhook) shows up within a minute
// instead of at the next restart. Changes are gathered until the folders have been quiet
// for watchQuiet, then one scan runs. Folders the scanner skips are not watched. Returns
// when ctx ends; a watcher that cannot start is logged and the server goes on without it.
func (l *Library) Watch(ctx context.Context) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("watching library folders: not available", "err", err)
		return
	}
	defer w.Close()
	roots := map[string]bool{} // library folders being watched
	add := func(root string) int {
		n := 0
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if p != root && SkipDirReason(p) != "" {
				return filepath.SkipDir
			}
			if err := w.Add(p); err == nil {
				n++
			}
			return nil
		})
		return n
	}
	var timer *time.Timer
	var fire <-chan time.Time
	pending := false
	poke := func() { // a change: scan once things have been quiet for watchQuiet
		pending = true
		if timer == nil {
			timer = time.NewTimer(watchQuiet)
			fire = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(watchQuiet)
	}
	// sync brings the watched folders in line with the library list. A folder that could
	// not be watched (missing) is tried again next time. Returns whether any library
	// started being watched.
	sync := func(first bool) bool {
		libs, err := l.store.ListLibraries(ctx)
		if err != nil {
			slog.Warn("watching library folders", "err", err)
			return false
		}
		current := map[string]bool{}
		added, folders := false, 0
		for _, lib := range libs {
			current[lib.Root] = true
			if roots[lib.Root] {
				continue
			}
			if n := add(lib.Root); n > 0 {
				roots[lib.Root] = true
				added = true
				folders += n
				if !first {
					slog.Info("watching a new library folder", "library", lib.Name, "folders", n)
				}
			}
		}
		for root := range roots {
			if current[root] {
				continue
			}
			delete(roots, root)
			for _, p := range w.WatchList() {
				if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
					_ = w.Remove(p)
				}
			}
			slog.Info("no longer watching a removed library folder", "root", root)
		}
		if first {
			slog.Info("watching library folders", "libraries", len(roots), "folders", folders)
		}
		return added
	}
	sync(true)
	refresh := time.NewTicker(watchRefresh)
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-refresh.C:
			// A library added (or its drive mounted) since: watched from now on, and
			// scanned once, in case files arrived before the watch was in place.
			if sync(false) {
				poke()
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Warn("watching library folders", "err", err)
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			isDir := false
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := statFile(ev.Name); err == nil && fi.IsDir() {
					isDir = true
					if SkipDirReason(ev.Name) == "" {
						add(ev.Name) // a new folder (and anything already in it)
					}
				}
			}
			// A removed or renamed folder is no longer a name we can stat: count it too.
			if !isDir && !watchRelevant(ev.Name) && ev.Op&(fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			poke()
		case <-fire:
			if !pending {
				continue
			}
			pending = false
			slog.Info("library folders changed: scanning")
			if err := l.ScanAll(ctx, false); err != nil {
				slog.Warn("scan after folder changes", "err", err)
			}
		}
	}
}
