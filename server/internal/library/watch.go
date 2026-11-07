package library

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchQuiet is how long a library must be still after a change before it is rescanned:
// a file being copied keeps changing, and must not be ingested half written.
var watchQuiet = 30 * time.Second

// watchMaxDelay is the longest a change waits for the folders to go quiet: one file being
// written for hours (a download, a recording) kept resetting the quiet period, so nothing
// copied anywhere else was scanned meanwhile.
var watchMaxDelay = 5 * time.Minute

// watchRefresh is how often the watcher re-reads the library list: libraries added since,
// or whose folder was missing (a drive not mounted yet), start being watched; removed ones
// stop; a folder that is now a different directory at the same path (a drive unmounted
// and mounted again, a folder deleted and made again) is watched afresh.
var watchRefresh = time.Minute

// watchRelevant are the files whose arrival, change or removal a scan cares about.
func watchRelevant(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return videoExts[ext] || subtitleExts[ext] || ext == ".nfo" || ext == ".ignore" || ext == ".roxyignore" ||
		strings.EqualFold(filepath.Base(name), ".ignore") || strings.EqualFold(filepath.Base(name), ".roxyignore")
}

// isIgnoreFile reports whether name is an .ignore or .roxyignore file.
func isIgnoreFile(name string) bool {
	b := filepath.Base(name)
	return strings.EqualFold(b, ".ignore") || strings.EqualFold(b, ".roxyignore")
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
	var scans sync.WaitGroup
	defer scans.Wait() // the scans it started end before it returns
	// The library folders being watched, with the directory each one was when its watch
	// was set up: a watch belongs to that directory, not to its path, so a drive mounted
	// again at the same path (or a folder made again) sends nothing until it is re-added.
	roots := map[string]fs.FileInfo{}
	// unwatch removes the watches on a root and every folder below it.
	unwatch := func(root string) {
		for _, p := range w.WatchList() {
			if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
				_ = w.Remove(p)
			}
		}
	}
	// libRootOf is the library folder a path is in ("" if none).
	libRootOf := func(p string) string {
		for root := range roots {
			if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
				return root
			}
		}
		return ""
	}
	// add watches dir and the folders below it, except those a scan skips: download
	// folders and the like, and what the library's .ignore rules leave out (their changes
	// are of no interest, and a busy one kept the quiet period from ever ending).
	add := func(dir, root string) int {
		n := 0
		ignores := ignoreCache{}
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if p != dir && SkipDirReason(p) != "" {
				return filepath.SkipDir
			}
			if root != "" && p != root && ignores.ignored(root, p, true) {
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
	var pendingSince time.Time
	// poke: a change. Scan once things have been quiet for watchQuiet, but no later than
	// watchMaxDelay after the first change still waiting.
	poke := func() {
		if !pending {
			pending, pendingSince = true, time.Now()
		}
		wait := watchQuiet
		if left := time.Until(pendingSince.Add(watchMaxDelay)); left < wait {
			wait = max(left, 0)
		}
		if timer == nil {
			timer = time.NewTimer(wait)
			fire = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
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
		watched := map[string]bool{}
		for _, p := range w.WatchList() {
			watched[p] = true
		}
		added, folders := false, 0
		for _, lib := range libs {
			current[lib.Root] = true
			if was, ok := roots[lib.Root]; ok {
				now, err := statFile(lib.Root)
				if err == nil && os.SameFile(was, now) && watched[lib.Root] {
					continue // still the directory being watched
				}
				// Gone (an unmounted drive), or another directory at the same path, or
				// its watch dropped by the system: the old watches see nothing of what is
				// there now. Set up again below, or when the folder is back.
				delete(roots, lib.Root)
				unwatch(lib.Root)
				if err != nil {
					slog.Info("library folder unavailable: watching it again once it is back", "library", lib.Name, "err", err)
					continue
				}
				slog.Info("library folder is a different directory now (mounted again?): watching it afresh", "library", lib.Name)
			}
			fi, err := statFile(lib.Root)
			if err != nil {
				continue
			}
			if n := add(lib.Root, lib.Root); n > 0 {
				roots[lib.Root] = fi
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
			unwatch(root)
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
			// A library added (or its drive mounted, or mounted again) since: watched
			// from now on, and scanned once, in case files arrived before the watch was
			// in place.
			if sync(false) {
				poke()
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Warn("watching library folders", "err", err)
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				// Changes were lost (folders made among them, so not watched): watch every
				// library afresh and scan.
				for root := range roots {
					unwatch(root)
					delete(roots, root)
				}
				sync(false)
				poke()
			}
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			isDir := false
			// A folder renamed or removed: its watches (and those below it) go with its old
			// name. Kept, a folder renamed (a show's folder renamed by the arr app) went on
			// being watched under its old name: folders made in it later could not be found
			// under that name, were never watched, and nothing in them was noticed.
			if ev.Op&(fsnotify.Rename|fsnotify.Remove) != 0 {
				unwatch(ev.Name)
			}
			if ev.Op&fsnotify.Create != 0 {
				fi, err := statFile(ev.Name)
				switch {
				case err != nil:
					isDir = true // gone again, or unreadable: a scan will tell
				case fi.IsDir():
					isDir = true
					if SkipDirReason(ev.Name) == "" {
						add(ev.Name, libRootOf(ev.Name)) // a new folder (and anything already in it)
					}
				}
			}
			// An .ignore file added, changed or removed: the folders it rules on are watched
			// again under the rules as they are now, so a folder it no longer leaves out is
			// watched (and one it now leaves out is not). The watches were set up once, so a
			// folder un-ignored later was scanned but never watched: nothing copied into it
			// was noticed.
			if isIgnoreFile(ev.Name) {
				if root := libRootOf(ev.Name); root != "" {
					dir := filepath.Dir(ev.Name)
					unwatch(dir)
					add(dir, root)
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
			// Beside the loop, which keeps reading events meanwhile: a long scan used to stop
			// them being read, until the kernel's queue overflowed. Requests made while a
			// scan runs are queued by ScanAll.
			scans.Add(1)
			go func() {
				defer scans.Done()
				if err := l.ScanAll(ctx, false); err != nil {
					slog.Warn("scan after folder changes", "err", err)
				}
			}()
		}
	}
}
