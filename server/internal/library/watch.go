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
	libs, err := l.store.ListLibraries(ctx)
	if err != nil {
		slog.Warn("watching library folders", "err", err)
		return
	}
	roots := map[string]bool{}
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
	total := 0
	for _, lib := range libs {
		roots[lib.Root] = true
		total += add(lib.Root)
	}
	slog.Info("watching library folders", "libraries", len(libs), "folders", total)

	var timer *time.Timer
	var fire <-chan time.Time
	pending := false
	for {
		select {
		case <-ctx.Done():
			return
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
			pending = true
			if timer == nil {
				timer = time.NewTimer(watchQuiet)
				fire = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(watchQuiet)
			}
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
