// Package library owns ingest: discovering files, probing them, extracting attachments
// and subtitles, and writing items/sources to the store.
//
// Phase 2 MVP: filesystem scan with filename parsing. Sonarr/Radarr webhook ingest
// (docs/api/arr-webhooks.md) is added on top of the same AddFile path.
package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/langdetect"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/probe"
	"github.com/roxy-media/roxy/server/internal/store"
)

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".webm": true, ".avi": true, ".mov": true, ".ts": true, ".m2ts": true, ".wmv": true, ".flv": true, ".ogm": true,
	// The rest of what Jellyfin accepts (older and broadcast formats; played by transcoding).
	".mpg": true, ".mpeg": true, ".mpe": true, ".m2v": true, ".vob": true, ".mts": true, ".m2t": true, ".mk3d": true,
	".rmvb": true, ".rm": true, ".divx": true, ".xvid": true, ".3gp": true, ".3g2": true, ".ogv": true, ".f4v": true,
	".asf": true, ".dvr-ms": true, ".wtv": true, ".qt": true, ".nsv": true, ".dv": true,
}

var subtitleExts = map[string]bool{
	".ass": true, ".ssa": true, ".srt": true, ".vtt": true, ".sub": true, ".idx": true, ".sup": true, ".mks": true,
	".smi": true, ".sami": true, // SAMI: served converted to SRT (probe.ConvertedToSRT)
}

type Library struct {
	store *store.Store
	ff    *ffmpeg.FFmpeg
	cfg   *config.Config

	// OnItemIngested, when set, is called with the id of the top-level item (series or
	// movie) after a file is ingested, and for every top-level item at the end of a full
	// scan. cmd/roxy wires it to the metadata service; this package must not import it.
	OnItemIngested func(itemID string)
	// OnSourceIngested, when set, is called with the media source id after a file is
	// ingested (trickplay generation).
	OnSourceIngested func(sourceID string)
	scanMu           sync.Mutex
	scanning         bool
	lastScan         time.Time
	lastScanErr      error // what the last full scan's last pass failed on (nil: every library scanned)
	// forcedNext holds one queued forced pass: ScanAll(ctx, true) called while a scan runs.
	// A channel (capacity 1), so repeated requests merge and no flag is shared.
	forcedNext chan struct{}
	// normalNext holds one queued normal pass: ScanAll(ctx, false) called while a scan runs.
	normalNext chan struct{}
	// scanOne scans one library; nil means l.scan. Tests replace it to watch passes.
	scanOne func(ctx context.Context, lib model.Library, force bool) error
	// ingestOne adds or updates one file during a scan; nil means l.addFileSafe. Tests
	// replace it, as there is no prober in tests.
	ingestOne func(ctx context.Context, lib model.Library, path string) error

	// watchList, while Watch runs, lists the folders it watches (tests read it).
	watchList atomic.Pointer[func() []string]

	// Libraries whose folder had no media files while files were recorded (see scan):
	// emptyRefused, those the last scan of left alone (id -> files recorded), for the
	// admin screen; emptyAccepted, those an admin confirmed are really empty, for the
	// next scan of each only.
	emptyMu       sync.Mutex
	emptyRefused  map[int64]int
	emptyAccepted map[int64]bool
}

// EmptyRefused is the libraries the last scan found with no media files but records of
// some, and left alone (id -> files recorded): an unmounted drive, or files really removed
// from a folder that is not provably the one scanned before. AcceptEmpty confirms one.
func (l *Library) EmptyRefused() map[int64]int {
	l.emptyMu.Lock()
	defer l.emptyMu.Unlock()
	out := map[int64]int{}
	for id, n := range l.emptyRefused {
		out[id] = n
	}
	return out
}

// AcceptEmpty is an admin confirming that a library's folder really is empty now: its
// next scan removes the records of the files it no longer has (their watch history waits
// as departures, so it returns if the files come back within 30 days).
func (l *Library) AcceptEmpty(id int64) {
	l.emptyMu.Lock()
	defer l.emptyMu.Unlock()
	if l.emptyAccepted == nil {
		l.emptyAccepted = map[int64]bool{}
	}
	l.emptyAccepted[id] = true
}

// takeEmptyAccepted reports (and forgets) whether an admin confirmed the library empty.
func (l *Library) takeEmptyAccepted(id int64) bool {
	l.emptyMu.Lock()
	defer l.emptyMu.Unlock()
	ok := l.emptyAccepted[id]
	delete(l.emptyAccepted, id)
	return ok
}

func (l *Library) setEmptyRefused(id int64, files int) {
	l.emptyMu.Lock()
	defer l.emptyMu.Unlock()
	if files == 0 {
		delete(l.emptyRefused, id)
		return
	}
	if l.emptyRefused == nil {
		l.emptyRefused = map[int64]int{}
	}
	l.emptyRefused[id] = files
}

// rootIdentity is dirIdentity; tests replace it.
var rootIdentity = dirIdentity

// ScanStatus reports whether a full scan is running, when the last one finished, and what
// it failed on (nil when every library was scanned).
func (l *Library) ScanStatus() (running bool, last time.Time, lastErr error) {
	l.scanMu.Lock()
	defer l.scanMu.Unlock()
	return l.scanning, l.lastScan, l.lastScanErr
}

func New(s *store.Store, ff *ffmpeg.FFmpeg, cfg *config.Config) *Library {
	return &Library{store: s, ff: ff, cfg: cfg, forcedNext: make(chan struct{}, 1), normalNext: make(chan struct{}, 1)}
}

// GuardMediaRoots tells safefs every folder that holds media (configured and added in
// the app), so nothing in them is ever written or deleted.
func GuardMediaRoots(ctx context.Context, st *store.Store, configured []string) {
	roots := append([]string{}, configured...)
	if libs, err := st.ListLibraries(ctx); err == nil {
		for _, l := range libs {
			roots = append(roots, l.Root)
		}
	}
	safefs.SetMediaRoots(roots)
}

// EnsureLibraries creates a library row for every configured media root that lacks one.
// The kind is guessed from the folder name ("movie" => movies, else series).
func (l *Library) EnsureLibraries(ctx context.Context, roots []string) error {
	removed, err := l.store.RemovedRoots(ctx)
	if err != nil {
		return err
	}
	for _, root := range roots {
		if removed[root] {
			continue // the admin removed it in the app; ROXY_MEDIA_ROOTS doesn't bring it back
		}
		existing, err := l.store.GetLibraryByRoot(ctx, root)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		name := filepath.Base(root)
		kind := model.KindSeries
		if strings.Contains(strings.ToLower(name), "movie") || strings.Contains(strings.ToLower(name), "film") {
			kind = model.KindMovies
		}
		lib := &model.Library{Name: name, Kind: kind, Root: root, AnimeMode: strings.Contains(strings.ToLower(name), "anime")}
		if err := l.store.CreateLibrary(ctx, lib); err != nil {
			return err
		}
		slog.Info("created library", "name", name, "kind", kind, "root", root)
	}
	return nil
}

// ScanAll scans every library, one full scan at a time. With force, every file of every
// library is re-probed, also those whose size and mtime are unchanged (after a probe or
// segment change). A call while a scan runs returns at once: a normal one is covered by
// the running scan; a forced one is queued and runs as one more pass when it ends.
//
// force is passed down per call. It used to be a shared Library.Force field that the
// API handler set and the first library's scan reset, so only that library was forced
// (and handler and scanner touched it without a lock).
func (l *Library) ScanAll(ctx context.Context, force bool) error {
	l.scanMu.Lock()
	if l.scanning {
		// Queued, not dropped: the running pass may already have walked past what this
		// request is about (a file copied in mid-scan, which the folder watcher reports).
		// Requests merge: at most one forced and one normal pass wait.
		next := l.normalNext
		if force {
			next = l.forcedNext
		}
		select {
		case next <- struct{}{}:
		default: // one is queued already
		}
		l.scanMu.Unlock()
		return nil
	}
	l.scanning = true
	l.scanMu.Unlock()
	for {
		err := l.scanAll(ctx, force)
		// The queue is checked under the lock that clears `scanning`, so a forced request
		// cannot arrive between "nothing queued" and "not scanning" and be left waiting.
		l.scanMu.Lock()
		again, forced := false, false
		select {
		case <-l.forcedNext:
			// A library that failed does not hold back the forced pass for the others.
			again, forced = ctx.Err() == nil, true
			select { // the forced pass covers a normal one asked for too
			case <-l.normalNext:
			default:
			}
		default:
			select {
			case <-l.normalNext:
				again = ctx.Err() == nil
			default:
			}
		}
		if !again {
			l.scanning, l.lastScan, l.lastScanErr = false, time.Now(), err
			l.scanMu.Unlock()
			return err
		}
		l.scanMu.Unlock()
		if forced {
			slog.Info("a forced scan was requested during the scan: scanning again, forced")
		} else {
			slog.Info("a scan was requested during the scan: scanning again")
		}
		force = forced
	}
}

func (l *Library) scanAll(ctx context.Context, force bool) error {
	one := l.scanOne
	if one == nil {
		one = l.scan
	}
	libs, err := l.store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	// A library that fails is logged and the others still scanned, but the failure is
	// returned (all of them, by library): it used to be logged only, so ScanAll reported
	// success, `roxy scan` exited 0 and the admin page showed a fresh scan time.
	var failed []error
	for _, lib := range libs {
		if err := one(ctx, lib, force); err != nil {
			slog.Error("scan failed", "library", lib.Name, "err", err)
			failed = append(failed, fmt.Errorf("library %q: %w", lib.Name, err))
		}
	}
	// Sweep every series/movie so metadata that is missing or stale gets looked up;
	// the service skips anything refreshed recently, so this is cheap.
	if l.OnItemIngested != nil {
		ids, err := l.store.TopLevelItemIDs(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			l.OnItemIngested(id)
		}
	}
	return errors.Join(failed...)
}

// Scan walks a library root, adds new/changed files and removes vanished ones. Files
// whose size and mtime are unchanged are skipped (ScanAll with force re-probes them).
func (l *Library) Scan(ctx context.Context, lib model.Library) error {
	return l.scan(ctx, lib, false)
}

// inScanKey marks a context as a scan's: its ingests leave settling departures to it.
type inScanKey struct{}

func (l *Library) scan(ctx context.Context, lib model.Library, force bool) error {
	ctx = context.WithValue(ctx, inScanKey{}, true)
	start := time.Now()
	// Taken at the start, so a confirmation applies to the next scan only and is never
	// left over for a later one (when the drive may really be missing).
	accepted := l.takeEmptyAccepted(lib.ID)
	l.setEmptyRefused(lib.ID, 0) // until this scan finds otherwise
	known, err := l.store.ListPartPaths(ctx, lib.ID)
	if err != nil {
		return err
	}
	// The sidecar subtitles recorded per source, and each folder's listing read once per
	// scan: a video that did not change is still re-ingested when its sidecars did.
	storedSidecars, err := l.store.ListSidecarStats(ctx, lib.ID)
	if err != nil {
		return err
	}
	probedAt, err := l.store.SourceProbedAt(ctx, lib.ID)
	if err != nil {
		return err
	}
	type listing struct {
		entries []os.DirEntry
		err     error
	}
	dirEntries := map[string]listing{}
	// sidecarsChanged is false with an error when the sidecars could not be checked: the
	// video is then left as it is (a failed listing used to read as "every sidecar
	// removed" and re-ingested the video with none), and the scan reports the folder.
	sidecarsChanged := func(path, sourceID string) (bool, error) {
		dir := filepath.Dir(path)
		ls, ok := dirEntries[dir]
		if !ok {
			ls.entries, ls.err = readDir(dir)
			dirEntries[dir] = ls
		}
		if ls.err != nil {
			return false, ls.err
		}
		stored := storedSidecars[sourceID]
		listed, err := sidecarFiles(path, lib.Root, ls.entries)
		if err != nil {
			return false, err
		}
		// A .mks with no recorded tracks (none it holds is a text track) that has not
		// changed since the video's last ingest was looked at then: not a change. Counted,
		// it re-ingested the video at every scan.
		current := listed[:0:0]
		for _, p := range listed {
			if _, rec := stored[p]; !rec && strings.EqualFold(filepath.Ext(p), ".mks") {
				if fi, err := statFile(p); err == nil && !fi.ModTime().After(probedAt[sourceID]) {
					continue
				}
			}
			current = append(current, p)
		}
		if len(current) != len(stored) {
			return true, nil
		}
		for _, p := range current {
			rec, ok := stored[p]
			if !ok {
				return true, nil // added or renamed
			}
			fi, err := statFile(p)
			if errors.Is(err, fs.ErrNotExist) {
				return true, nil // gone since the listing: removed
			}
			if err != nil {
				// Not "unchanged": the scan reports it like a folder it could not read.
				return false, fmt.Errorf("reading subtitle %s: %w", p, err)
			}
			size, mtime := fi.Size(), fi.ModTime().UTC().Truncate(time.Millisecond)
			if rec.ModTime == "" {
				// Recorded before file details were kept: record them now, no re-ingest.
				if err := l.store.RecordSidecarStat(ctx, sourceID, p, size, mtime); err != nil {
					slog.Warn("recording sidecar details", "path", p, "err", err)
				}
				continue
			}
			if rec.Size != size || rec.ModTime != mtime.Format(store.TimeFormat) {
				return true, nil // replaced under the same name
			}
		}
		return false, nil
	}
	seen := map[string]bool{}
	var added, updated, skipped int
	var extras []string      // extras found by the walk, ingested after it
	ignores := ignoreCache{} // each folder's .ignore patterns, read once per scan
	// What the walk could not read. Any of it makes this scan a failed one for removals:
	// nothing recorded is deleted until a scan that read everything shows a file is
	// really gone (a read error can mean a failing disk or a half-mounted drive, so what
	// looks missing elsewhere is not trusted either). Walk errors used to be logged and
	// skipped, and everything unseen deleted, so a folder that lost its permissions, or a
	// library root that could not be read at all, emptied the library.
	var unreadable []string
	var rootErr error
	// hidden counts folders and videos the library's .ignore rules left out: a walk that
	// found nothing because the rules hide everything (an .ignore dropped in the library's
	// own folder, a "*" pattern) is not a library whose files were removed.
	hidden := 0
	// Files that could not be added or updated (their details unreadable, or a probe or
	// parse failure):
	// the rest of the scan goes on, and the count is reported as a partial failure. They
	// used to be logged only, so such a scan still reported success.
	ingestFailed := 0
	var ingestErrs []string // the first few, "name (reason)"
	// Extras (trailers, featurettes) that could not be added: reported on their own line.
	// They used to be logged only, so a scan that failed on them reported success.
	extrasFailed := 0
	var extrasErrs []string

	err = walkDir(lib.Root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == lib.Root {
				rootErr = walkErr
				return filepath.SkipAll
			}
			slog.Warn("walk error", "path", path, "err", walkErr)
			unreadable = append(unreadable, path)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			// Download folders, recycle bins, .ignore'd folders: see skip.go; and what an
			// .ignore's patterns name, in any folder down from the root (ignore.go).
			if path != lib.Root && SkipDirReason(path) != "" {
				return filepath.SkipDir
			}
			if path != lib.Root && ignores.ignored(lib.Root, path, true) {
				hidden++
				return filepath.SkipDir
			}
			return nil
		}
		if ignores.ignored(lib.Root, path, false) {
			if videoExts[strings.ToLower(filepath.Ext(path))] {
				hidden++
			}
			return nil
		}
		if !videoExts[strings.ToLower(filepath.Ext(path))] || SkipFileReason(d.Name()) != "" {
			return nil
		}
		if _, err := safefs.ResolveUnder(path, lib.Root); err != nil {
			if errors.Is(err, safefs.ErrEscapes) || errors.Is(err, fs.ErrNotExist) {
				// A symlink out of the library, or one to nothing: never ingested, and
				// dropped if it was before.
				slog.Warn("skipping file outside the library", "path", path, "err", err)
				return nil
			}
			// Could not be checked (a folder that can be listed but not entered, an I/O
			// error): like a folder the walk could not read. Its record is kept and
			// nothing is removed this scan; it used to be taken for a symlink out of the
			// library and its record deleted.
			slog.Warn("checking a file's location failed", "path", path, "err", err)
			seen[path] = true
			if dir := filepath.Dir(path); len(unreadable) == 0 || unreadable[len(unreadable)-1] != dir {
				unreadable = append(unreadable, dir)
			}
			return nil
		}
		seen[path] = true
		// Extras wait until the titles are in: "Film-trailer.mkv" sorts before "Film.mkv".
		if rel, err := filepath.Rel(lib.Root, path); err == nil {
			if _, _, _, isExtra := ExtraOf(filepath.ToSlash(rel)); isExtra {
				extras = append(extras, path)
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			// Seen, so its record is kept; but it could not be checked or updated, and the
			// scan says so (it used to return here silently and report success).
			slog.Error("reading file details failed", "path", path, "err", err)
			ingestFailed++
			if len(ingestErrs) < 3 {
				ingestErrs = append(ingestErrs, fmt.Sprintf("%s (reading it failed: %v)", filepath.Base(path), err))
			}
			return nil
		}
		if _, ok := known[path]; ok {
			part, err := l.store.GetPartByPath(ctx, path)
			if !force && err == nil && part != nil && part.Size == info.Size() && part.ModTime.Equal(info.ModTime().UTC().Truncate(time.Millisecond)) {
				// The video is unchanged, but sidecar subtitles beside it are only read when
				// it is ingested: one added, removed or renamed since would otherwise never
				// show up (or never go away) until a forced scan.
				changed, err := sidecarsChanged(path, known[path])
				if err != nil {
					// Like a folder the walk could not read: nothing recorded is removed this
					// scan, and the scan reports it.
					slog.Warn("checking sidecar subtitles failed", "path", path, "err", err)
					if dir := filepath.Dir(path); len(unreadable) == 0 || unreadable[len(unreadable)-1] != dir {
						unreadable = append(unreadable, dir)
					}
				}
				if !changed {
					skipped++
					return nil
				}
				slog.Info("sidecar subtitles changed, re-ingesting", "path", path)
			}
			updated++
		} else {
			added++
		}
		ingest := l.ingestOne
		if ingest == nil {
			ingest = l.addFileSafe
		}
		if err := ingest(ctx, lib, path); err != nil {
			slog.Error("ingest failed", "path", path, "err", err)
			ingestFailed++
			if len(ingestErrs) < 3 {
				ingestErrs = append(ingestErrs, fmt.Sprintf("%s (%v)", filepath.Base(path), err))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if hook := afterWalk.Load(); hook != nil {
		(*hook)(lib) // tests: hold the scan here, after it has read every folder
	}
	for _, path := range extras {
		ingest := l.ingestOne
		if ingest == nil {
			ingest = l.addFileSafe
		}
		if l.extraUpToDate(ctx, path) {
			skipped++
			continue
		}
		if _, ok := known[path]; ok {
			updated++
		} else {
			added++
		}
		if err := ingest(ctx, lib, path); err != nil {
			slog.Warn("extra not added", "path", path, "err", err)
			extrasFailed++
			if len(extrasErrs) < 3 {
				extrasErrs = append(extrasErrs, fmt.Sprintf("%s (%v)", filepath.Base(path), err))
			}
		}
	}
	if rootErr != nil {
		return fmt.Errorf("the library folder %s cannot be read (%v); nothing was removed", lib.Root, rootErr)
	}
	// Files are recorded but the walk found none at all. Most likely the drive is not
	// mounted and what is there is the empty folder Docker creates for a missing bind
	// mount: removing everything would empty the library, so nothing is. But if the folder
	// is provably the directory a scan found files in before (same device, inode and
	// creation time), its files really were removed, and their records go; so they do
	// when an admin confirms it (AcceptEmpty). Otherwise an emptied library kept its
	// records, and failed every scan, for good.
	identity, idErr := rootIdentity(lib.Root)
	if idErr != nil {
		slog.Warn("reading the library folder's identity", "library", lib.Name, "err", idErr)
		identity = ""
	}
	if len(seen) == 0 && len(known) > 0 {
		stored, err := l.store.LibraryRootIdentity(ctx, lib.ID)
		if err != nil {
			return err
		}
		switch {
		case hidden > 0 && !accepted:
			l.setEmptyRefused(lib.ID, len(known))
			return fmt.Errorf("the library folder %s shows no media files because its .ignore rules leave out everything in it (%d folder(s) or file(s)); %d are recorded and nothing was removed. Change the rules, or confirm on the Library page that the files really are gone", lib.Root, hidden, len(known))
		case accepted:
			slog.Warn("library folder confirmed empty by an admin: removing its records", "library", lib.Name, "files", len(known))
		case identity != "" && identity == stored:
			slog.Warn("library folder emptied (the same folder as before, not a missing drive): removing its records", "library", lib.Name, "files", len(known))
		default:
			l.setEmptyRefused(lib.ID, len(known))
			return fmt.Errorf("the library folder %s has no media files but %d are recorded; nothing was removed (is the drive mounted? If the files really are gone, confirm it on the Library page)", lib.Root, len(known))
		}
	}
	if len(seen) > 0 && identity != "" {
		if stored, err := l.store.LibraryRootIdentity(ctx, lib.ID); err == nil && stored != identity {
			if err := l.store.SetLibraryRootIdentity(ctx, lib.ID, identity); err != nil {
				slog.Warn("recording the library folder's identity", "library", lib.Name, "err", err)
			}
		}
	}

	removed := 0
	for path, sourceID := range known {
		if seen[path] || len(unreadable) > 0 {
			continue
		}
		// Noted first, so its watch history can follow it if it turns up elsewhere (this
		// scan, another library's, or later).
		if err := l.store.RecordDeparture(ctx, path); err != nil {
			slog.Warn("noting a vanished file", "path", path, "err", err)
		}
		if err := l.store.DeleteSource(ctx, sourceID); err != nil {
			slog.Warn("removing vanished source", "path", path, "err", err)
			continue
		}
		removed++
	}
	l.settleDepartures(ctx) // before the old items are pruned
	// Always: orphans can also date from before this pruning existed, and the query is cheap.
	if n, err := l.store.PruneOrphans(ctx); err != nil {
		slog.Warn("pruning orphaned items", "err", err)
	} else if n > 0 {
		slog.Info("pruned orphaned items", "count", n)
	}
	slog.Info("scan complete", "library", lib.Name, "added", added, "updated", updated, "unchanged", skipped, "removed", removed, "forced", force, "took", time.Since(start).Round(time.Millisecond))
	// One line for the library (the admin screens show a line per library): unreadable
	// folders, then files that failed, each with its own count, so one odd file reads as
	// that and not as the library being unavailable.
	var problems []string
	if len(unreadable) > 0 {
		shown := unreadable
		if len(shown) > 3 {
			shown = shown[:3]
		}
		problems = append(problems, fmt.Sprintf("%d folder(s) could not be read, so nothing was removed this scan: %s", len(unreadable), strings.Join(shown, ", ")))
	}
	if ingestFailed > 0 {
		more := ""
		if ingestFailed > len(ingestErrs) {
			more = fmt.Sprintf("; and %d more", ingestFailed-len(ingestErrs))
		}
		problems = append(problems, fmt.Sprintf("%d file(s) could not be added or updated: %s%s", ingestFailed, strings.Join(ingestErrs, "; "), more))
	}
	if extrasFailed > 0 {
		more := ""
		if extrasFailed > len(extrasErrs) {
			more = fmt.Sprintf("; and %d more", extrasFailed-len(extrasErrs))
		}
		problems = append(problems, fmt.Sprintf("%d extra(s) could not be added: %s%s", extrasFailed, strings.Join(extrasErrs, "; "), more))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, ". "))
	}
	return nil
}

// walkDir is filepath.WalkDir; tests replace it to inject read failures.
var walkDir = filepath.WalkDir

// readDir is os.ReadDir for the sidecar listings; tests replace it to inject read failures.
var readDir = os.ReadDir

// statFile is os.Stat for sidecar files; tests replace it to inject read failures.
var statFile = os.Stat

// afterWalk, when set, is called by a scan once its walk of a library is done, before it
// ingests extras and removes vanished files. Tests use it to hold a scan at that point
// and change the library underneath it (nil in the server).
var afterWalk atomic.Pointer[func(lib model.Library)]

// addFileSafe wraps AddFile so a parser bug on one odd filename cannot take down a scan.
func (l *Library) addFileSafe(ctx context.Context, lib model.Library, path string) error {
	return l.addFileSafeHinted(ctx, lib, path, nil)
}

func (l *Library) addFileSafeHinted(ctx context.Context, lib model.Library, path string, hint *EpisodeHint) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic ingesting %s: %v", filepath.Base(path), r)
		}
	}()
	return l.AddFileHinted(ctx, lib, path, hint)
}

// AddFile ingests one video file: resolves (or creates) its item from the folder
// structure and filename, probes it, extracts attachments and text subtitles, attaches
// sidecar subtitles, and stores everything.
func (l *Library) AddFile(ctx context.Context, lib model.Library, path string) error {
	return l.AddFileHinted(ctx, lib, path, nil)
}

// AddFileHinted is AddFile with the arr app's season/episode for the file (may be nil).
func (l *Library) AddFileHinted(ctx context.Context, lib model.Library, path string, hint *EpisodeHint) error {
	// The file must really be inside the library: a symlink out of it is not media.
	if _, err := safefs.ResolveUnder(path, lib.Root); err != nil {
		return err
	}
	// A file new to the library may be one that left it elsewhere (moved, renamed,
	// re-encoded): checked against departures once it is in (see settleDepartures).
	prior, err := l.store.GetPartByPath(ctx, path)
	if err != nil {
		return err
	}
	// The item it is recorded under, and that item's parents: if this ingest reads it as
	// something else (a parser that now understands its name, an .nfo, a folder renamed
	// above it), its watch history follows it to the new item.
	var priorChain []store.ChainEntry
	if prior != nil {
		if old, err := l.store.ItemForPath(ctx, path); err == nil && old != nil {
			priorChain, _ = l.store.ItemChain(ctx, old.ID)
		}
	}
	item, err := l.resolveForIngest(ctx, lib, path, hint)
	if err != nil {
		return err
	}

	res, err := probe.File(ctx, l.ff, path)
	if err != nil {
		return err
	}

	sourceID := stableID("source", path)
	src := &model.MediaSource{
		ID:         sourceID,
		ItemID:     item.ID,
		Name:       filepath.Base(path),
		Container:  res.Container,
		Size:       res.Size,
		DurationMs: res.DurationMs,
		Bitrate:    res.Bitrate,
		IsPrimary:  true,
		Parts: []model.MediaPart{{
			ID: stableID("part", path), SourceID: sourceID, Index: 0, Path: path, Size: res.Size,
			ModTime: res.ModTime.UTC().Truncate(time.Millisecond), DurationMs: res.DurationMs,
		}},
		Streams:     res.Streams,
		Attachments: res.Attachments,
		Segments:    segmentsFromChapters(res.Chapters, res.DurationMs),
	}
	for i := range src.Streams {
		src.Streams[i].SourceID = sourceID
	}
	for i := range src.Attachments {
		src.Attachments[i].SourceID = sourceID
	}

	// A file replaced in place (same path, so same source id) must not keep the old
	// file's subtitles and seek thumbnails: both caches are reused whenever they exist.
	l.dropStaleCaches(ctx, path, sourceID, res.Size, res.ModTime)

	// Eager attachment extraction (fonts).
	if len(src.Attachments) > 0 {
		dir := filepath.Join(l.cfg.AttachmentsDir(), sourceID)
		if err := probe.ExtractAttachments(ctx, l.ff, path, dir, src.Attachments); err != nil {
			slog.Warn("attachment extraction failed", "path", path, "err", err)
		}
	}

	// Sidecar subtitles next to the file (Bazarr naming), keeping the numbers they had: a
	// profile's saved subtitle choice is a stream number, so renumbering on a re-ingest
	// (a new sidecar sorting before an old one) moved that choice to another file.
	// If they cannot be read the ingest fails and the stored record (with its subtitle
	// tracks) stays as it was, rather than being replaced by one with none.
	// Sidecars are numbered from sidecarBase, above every number the file's own streams
	// use (ffprobe numbers them all, including the data and cover-art streams Roxy leaves
	// out, which counting the kept ones missed: a sidecar could get an embedded track's
	// number and the file could not be stored).
	found, err := sidecarSubtitles(path, lib.Root, sidecarBase)
	if err != nil {
		return err
	}
	// The .mks files that had tracks recorded: one of them failing to read now fails the
	// ingest (the record and its numbers stay); one that never had any (new, or corrupt
	// from the start) is skipped, so it cannot keep the video out of the library.
	known := map[string]bool{}
	if old, err := l.store.GetSource(ctx, sourceID); err == nil && old != nil {
		for _, st := range old.Streams {
			if st.ExternalIndex != nil {
				known[st.ExternalPath] = true
			}
		}
	}
	if found, err = l.expandSubtitleContainers(ctx, found, known); err != nil {
		return err
	}
	embedded := res.StreamCount
	for _, st := range src.Streams {
		embedded = max(embedded, st.Index+1)
	}
	for _, a := range src.Attachments {
		embedded = max(embedded, a.Index+1)
	}
	sidecars, moved, err := l.keepSidecarNumbers(ctx, sourceID, found, embedded)
	if err != nil {
		return err
	}
	src.Streams = append(src.Streams, sidecars...)

	// Eager text-subtitle extraction + language sniffing. Extracted files land where the
	// subtitle endpoint looks for them, so this also warms that cache.
	l.detectSubtitleLanguages(ctx, path, src)

	if item.RuntimeMs == nil && res.DurationMs > 0 {
		d := res.DurationMs
		item.RuntimeMs = &d
		_ = l.store.UpsertItem(ctx, item)
	}

	src.VersionLabel = versionLabel(path, src.Streams)
	if err := l.store.ReplaceSource(ctx, src, string(res.Raw)); err != nil {
		return err
	}
	// A sidecar that had to give up its number (the video, re-encoded, now has a stream
	// of its own there): saved subtitle choices follow it to its new number, instead of
	// landing on the embedded track that has the old one.
	if len(moved) > 0 {
		if err := l.store.RemapSubtitleChoices(ctx, item.GUID, moved); err != nil {
			slog.Warn("moving saved subtitle choices to renumbered sidecars", "item", item.Title, "err", err)
		}
	}
	// Several files of one film or episode: the best one first and primary.
	if err := l.store.RankVersions(ctx, item.ID); err != nil {
		slog.Warn("ranking versions", "item", item.Title, "err", err)
	}
	if prior == nil {
		// During a scan the departures are settled once, after the walk: settling after
		// every new file cost a full pass each (seconds per hundred files on a big import).
		if ctx.Value(inScanKey{}) == nil {
			l.settleDepartures(ctx)
		}
	} else if len(priorChain) > 0 && priorChain[0].ID != item.ID {
		if _, err := l.store.CarryChain(ctx, priorChain, item.ID); err != nil {
			slog.Warn("carrying watch history to a re-identified file", "path", path, "err", err)
		}
	}
	slog.Info("ingested", "item", item.Title, "type", item.Type, "file", filepath.Base(path), "streams", len(src.Streams), "fonts", len(src.Attachments))
	if l.OnItemIngested != nil {
		if top, err := l.store.TopLevelID(ctx, item.ID); err == nil {
			l.OnItemIngested(top)
		}
	}
	if l.OnSourceIngested != nil {
		l.OnSourceIngested(sourceID)
	}
	return nil
}

// dropStaleCaches deletes the extracted subtitles and trickplay sheets of a source whose
// file changed (size or modification time) since it was last ingested. The subtitle
// endpoint and trickplay both reuse whatever is cached for the source id, and the id
// comes from the path alone, so without this a re-encoded or replaced file kept serving
// the old file's subtitles and thumbnails. A forced rescan of an unchanged file keeps them.
func (l *Library) dropStaleCaches(ctx context.Context, path, sourceID string, size int64, modTime time.Time) {
	if l.cfg == nil {
		return
	}
	part, err := l.store.GetPartByPath(ctx, path)
	if err != nil {
		return
	}
	// No record of this path: a file new here, or one whose record was removed (deleted by
	// the arr app for an upgrade, then a new download under the same name). The caches
	// are named by path alone, so anything there is an earlier file's: it used to be
	// served for the new one (its subtitles, its seek previews).
	if part != nil && part.Size == size && part.ModTime.Equal(modTime.UTC().Truncate(time.Millisecond)) {
		return
	}
	for _, dir := range []string{filepath.Join(l.cfg.SubtitlesDir(), sourceID), filepath.Join(l.cfg.TrickplayDir(), sourceID)} {
		if err := safefs.RemoveAll(dir); err != nil {
			slog.Warn("removing stale cache", "dir", dir, "err", err)
		}
	}
}

// detectSubtitleLanguages fills DetectedLanguage for every text subtitle stream by
// reading its text (sidecar file directly, embedded track via extraction into the
// subtitle cache). Failures are logged and leave the field empty.
func (l *Library) detectSubtitleLanguages(ctx context.Context, path string, src *model.MediaSource) {
	for i := range src.Streams {
		st := &src.Streams[i]
		if st.Type != model.StreamSubtitle || !st.IsTextFormat {
			continue
		}
		var textPath string
		if cache := probe.SidecarCachePath(l.cfg.SubtitlesDir(), src.ID, *st); cache != "" {
			// A track inside a .mks (or a SAMI file): read from its extracted copy, which
			// also warms the cache the subtitle endpoint serves from.
			input, index := st.ExternalPath, 0
			if st.ExternalIndex != nil {
				index = *st.ExternalIndex
			}
			if err := probe.EnsureTextSubtitle(ctx, l.ff, input, index, st.Codec, cache); err != nil {
				slog.Warn("subtitle extraction failed", "file", filepath.Base(input), "stream", index, "err", err)
				continue
			}
			textPath = cache
		} else if st.IsExternal {
			textPath = st.ExternalPath
		} else {
			ext := map[string]string{"ass": ".ass", "ssa": ".ass", "subrip": ".srt", "srt": ".srt", "webvtt": ".vtt", "vtt": ".vtt"}[st.Codec]
			if ext == "" {
				continue
			}
			textPath = filepath.Join(l.cfg.SubtitlesDir(), src.ID, fmt.Sprintf("%d%s", st.Index, ext))
			if _, err := os.Stat(textPath); err != nil {
				if err := probe.EnsureTextSubtitle(ctx, l.ff, path, st.Index, st.Codec, textPath); err != nil {
					slog.Warn("subtitle extraction failed", "file", filepath.Base(path), "stream", st.Index, "err", err)
					continue
				}
			}
		}
		// A sample is all language detection needs; a sidecar can be any size.
		b, err := readHead(textPath, 64<<10)
		if err != nil {
			continue
		}
		if r := langdetect.Detect(string(b)); r.Lang != "" && r.Confidence >= 0.4 {
			st.DetectedLanguage = r.Lang
			if st.Language != "" && langdetect.Normalize(st.Language) != r.Lang {
				slog.Info("subtitle language mismatch", "file", filepath.Base(path), "stream", st.Index, "tagged", st.Language, "detected", r.Lang, "title", st.Title)
			}
		}
	}
}

// resolveItem maps a file to its item using folder structure and filename (fallback mode).
// resolveForIngest is the item a file being ingested belongs to: an extra under its title
// (extras.go), or the movie or episode its name and folders say.
func (l *Library) resolveForIngest(ctx context.Context, lib model.Library, path string, hint *EpisodeHint) (*model.Item, error) {
	if rel, err := filepath.Rel(lib.Root, path); err == nil {
		if kind, ownerDir, base, isExtra := ExtraOf(filepath.ToSlash(rel)); isExtra {
			return l.resolveExtra(ctx, lib, path, kind, ownerDir, base)
		}
	}
	return l.resolveItemHinted(ctx, lib, path, hint)
}

func (l *Library) resolveItem(ctx context.Context, lib model.Library, path string) (*model.Item, error) {
	return l.resolveItemHinted(ctx, lib, path, nil)
}

func (l *Library) resolveItemHinted(ctx context.Context, lib model.Library, path string, hint *EpisodeHint) (*model.Item, error) {
	rel, err := filepath.Rel(lib.Root, path)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	fileName := parts[len(parts)-1]
	parsed, hinted := l.parseEpisodePath(ctx, lib, path, parts, true, hint)
	return l.resolveParsed(ctx, lib, path, parts, fileName, parsed, hinted)
}

// parseEpisodePath reads a file's numbers from its name and folders. With useHints, a name
// with no episode number takes the arr app's numbers (hint) or the ones its item already
// has (a hint from an earlier import, an .nfo); hinted says whether it did.
func (l *Library) parseEpisodePath(ctx context.Context, lib model.Library, path string, parts []string, useHints bool, hint *EpisodeHint) (ParsedName, bool) {
	fileName := parts[len(parts)-1]
	parsed := ParseEpisodeFile(fileName)
	hinted := false
	// A name with no episode number at all: take the arr app's numbers, or keep the ones
	// this file already has (a hint from an earlier import), so a rescan of a name the
	// parser cannot read does not turn a numbered episode back into a bare filename.
	if useHints && lib.Kind != model.KindMovies && parsed.Episode == nil && parsed.Absolute == nil {
		if h := l.episodeHintFor(ctx, path, hint); h != nil {
			parsed.applyHint(h)
			hinted = true
		}
	}
	// Still no number: the looser shapes (a bracketed number, "ep01", a name starting with
	// its number), then the number in the episode's own folder name ("Show - 05/video.mkv").
	// After the hint, so they never override the arr app's numbers. Not for a file that
	// looks like a film (its year would be read as an episode).
	weakStem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	if lib.Kind != model.KindMovies && parsed.Episode == nil && parsed.Absolute == nil && len(parts) > 1 &&
		!LooksLikeMovie(parsed, weakStem, parts[0], len(parts)) {
		if n, ok := ParseWeakEpisode(weakStem); ok {
			parsed.Episode = &n
		} else if len(parts) > 2 {
			if _, isSeason := ParseSeasonDir(parts[len(parts)-2]); !isSeason {
				folder := ParseEpisodeFile(parts[len(parts)-2])
				switch {
				case folder.Episode != nil:
					parsed.Episode, parsed.Season = folder.Episode, folder.Season
				case folder.Absolute != nil:
					parsed.Absolute = folder.Absolute
				}
			}
		}
	}
	return parsed, hinted
}

// seasonNumberFor is the season a file belongs to: its name's, or a season folder's below
// the series folder (parts[si]).
func seasonNumberFor(parsed ParsedName, parts []string, si int) int {
	seasonNum := 1
	if parsed.Season != nil {
		seasonNum = *parsed.Season
	}
	if len(parts) > si+2 {
		for _, dir := range parts[si+1 : len(parts)-1] {
			if n, ok := ParseSeasonDir(dir); ok {
				seasonNum = n
			}
		}
	}
	return seasonNum
}

func (l *Library) resolveParsed(ctx context.Context, lib model.Library, path string, parts []string, fileName string, parsed ParsedName, hinted bool) (*model.Item, error) {
	stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	dir := ""
	if len(parts) > 1 {
		dir = parts[0]
	}
	// Movies: either a movies library, or a file in a mixed/series library that carries a
	// year and no episode numbering (anime libraries often keep films next to series).
	if lib.Kind == model.KindMovies || LooksLikeMovie(parsed, stem, dir, len(parts)) {
		existing, err := l.store.ItemGUIDForPath(ctx, path)
		if err != nil {
			return nil, err
		}
		m, err := movieIdentity(lib.Root, path, parsed.CleanTitle, existing)
		if err != nil {
			return nil, err
		}
		// Files of different films merged into one item by an older release (a collection
		// folder read as one film): only one of them keeps that item and its watch history
		// (legacyKeeper), the others are separate films from now on. Keeping every one of
		// them on it would leave the merge in place forever.
		if existing != "" && m.guid == existing {
			if mates, err := l.store.PathsForGUID(ctx, existing); err == nil && len(mates) > 1 {
				first := legacyKeeper(mates)
				if first != path {
					if fresh, err := movieIdentity(lib.Root, path, parsed.CleanTitle, ""); err == nil && fresh.guid != existing {
						if fm, err := movieIdentity(lib.Root, first, "", ""); err == nil && fm.guid != fresh.guid {
							m = fresh // a different film from the one that keeps the item
						}
					}
				}
			}
		}
		if err != nil {
			return nil, err
		}
		it := &model.Item{ID: uuid.NewString(), GUID: m.guid, LibraryID: lib.ID, Type: model.TypeMovie, Title: m.title, Year: m.year, ProviderIDs: m.ids}
		if err := l.store.UpsertItem(ctx, it); err != nil {
			return nil, err
		}
		return it, nil
	}

	// Series library: <root>/[<grouping folder>/]<Series dir>/[Season NN/]<file>
	si := SeriesFolderIndex(parts)
	seriesDir := parts[si]
	if len(parts) == 1 {
		seriesDir = parsed.CleanTitle
		if seriesDir == "" {
			seriesDir = strings.TrimSuffix(fileName, filepath.Ext(fileName))
		}
	}
	title, year, ids := ParseSeriesDir(seriesDir)
	series := &model.Item{ID: uuid.NewString(), GUID: guidFor(ids, "series", lib.Root, filepath.Join(parts[:si+1]...)), LibraryID: lib.ID, Type: model.TypeSeries, Title: title, Year: year, ProviderIDs: ids}
	if err := l.store.UpsertItem(ctx, series); err != nil {
		return nil, err
	}

	seasonNum := seasonNumberFor(parsed, parts, si)
	season := &model.Item{
		ID: uuid.NewString(), GUID: fmt.Sprintf("%s:season:%d", series.GUID, seasonNum), LibraryID: lib.ID, Type: model.TypeSeason,
		ParentID: &series.ID, Title: seasonTitle(seasonNum), IndexAired: &seasonNum,
	}
	// Ids on the season's folder ("Season 2 [anidbid-5678]"): anime seasons are often
	// separate entries at AniDB / AniList.
	if len(parts) > 2 {
		if ids := idTags(parts[len(parts)-2]); len(ids) > 0 {
			if _, isSeason := ParseSeasonDir(parts[len(parts)-2]); isSeason {
				season.ProviderIDs = ids
			}
		}
	}
	if err := l.store.UpsertItem(ctx, season); err != nil {
		return nil, err
	}

	epNum := parsed.Episode
	if epNum == nil {
		epNum = parsed.Absolute
	}
	// "Arlong Park 07.mp4" inside "[One Pace][69-95] Arlong Park": the folder's own name
	// plus a number (Jellyfin reads "<name> <number>" too). Only the number is set: the
	// item keeps its path GUID, because arc folders are not "Season N" folders and every
	// arc would otherwise collide on s01eNN.
	if epNum == nil && len(parts) > 1 {
		if n, ok := FolderTitleNumber(stem, parts[len(parts)-2]); ok {
			epNum = &n
		}
	}
	epTitle := episodeTitle(fileName, parsed)
	epGUID, err := l.episodeGUID(ctx, series.GUID, season.ID, seasonNum, parsed, path)
	if err != nil {
		return nil, err
	}
	// A file once identified by its own path (its name gave no number) keeps that identity
	// when it later has numbers from elsewhere: an .nfo, or the numbers its item was given
	// (read back as a hint on the next ingest). That identity belongs to this path alone,
	// so keeping it can collide with nothing, and switching would leave its watch history
	// behind (412 One Pace episodes would have moved on a forced rescan).
	if own := "file:" + hashHex(path); epGUID != own {
		if cur, err := l.store.ItemGUIDForPath(ctx, path); err == nil && cur == own {
			epGUID = own
		}
	}
	// Likewise a file whose numbers came from a hint (the numbers its item was given by
	// metadata or an .nfo, read back on the next ingest) keeps the identity its path alone
	// gives, when that is the one it has: a date-named episode stays :date: once metadata
	// numbers it, and a loosely numbered one ("[05]") stays s01e0005 once an .nfo places it
	// in season 2. The hint used to change the identity on the next rescan, leaving the
	// watch history behind. Only the identity this very path gives is kept, so it can
	// merge the file with no other.
	if hinted {
		pathParsed, _ := l.parseEpisodePath(ctx, lib, path, parts, false, nil)
		if pg, err := l.episodeGUID(ctx, series.GUID, "", seasonNumberFor(pathParsed, parts, si), pathParsed, path); err == nil && pg != epGUID {
			if cur, err := l.store.ItemGUIDForPath(ctx, path); err == nil && cur == pg {
				epGUID = pg
			}
		}
	}
	var epEnd *int
	if parsed.Episode != nil && parsed.EpisodeEnd != nil && *parsed.EpisodeEnd > *parsed.Episode {
		epEnd = parsed.EpisodeEnd
	}
	ep := &model.Item{
		ID: uuid.NewString(), GUID: epGUID, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID,
		Title: epTitle, IndexAired: epNum, SeasonAired: &seasonNum, IndexAbsolute: parsed.Absolute, IndexAiredEnd: epEnd,
	}
	if err := l.store.UpsertItem(ctx, ep); err != nil {
		return nil, err
	}
	if parsed.AirDate != "" && epNum == nil {
		if err := l.store.SetAirDateIfUnset(ctx, ep.ID, parsed.AirDate); err != nil {
			return nil, err
		}
	}
	return ep, nil
}

// episodeGUID is an episode file's stable GUID: s<season>e<episode> for season-numbered
// files, abs<number> for absolute-numbered ones, else the path's hash.
//
// An absolute-numbered file in a Season N folder (N > 1) gets s<N>abs<number>, so a
// show whose season folders each restart at "- 01" does not collapse every season's
// episode 1 into one item. Season 1 keeps the plain abs<number> it always had, and an
// item already stored under the old GUID in the same season keeps it (also when a
// "S2 - 01" name, once read as absolute 1, is now read as S2E1), so a rescan never
// duplicates an item or loses its watch state.
func (l *Library) episodeGUID(ctx context.Context, seriesGUID, seasonID string, seasonNum int, parsed ParsedName, path string) (string, error) {
	var guid string
	switch {
	case parsed.Episode != nil:
		guid = fmt.Sprintf("%s:s%02de%04d", seriesGUID, seasonNum, *parsed.Episode)
	case parsed.Absolute != nil && seasonNum > 1:
		guid = fmt.Sprintf("%s:s%02dabs%04d", seriesGUID, seasonNum, *parsed.Absolute)
	case parsed.Absolute != nil:
		return fmt.Sprintf("%s:abs%04d", seriesGUID, *parsed.Absolute), nil
	case parsed.AirDate != "":
		// A daily show's episode, named by its date: the date is its identity.
		return fmt.Sprintf("%s:date:%s", seriesGUID, parsed.AirDate), nil
	default:
		return "file:" + hashHex(path), nil
	}
	legacy := ""
	switch {
	case parsed.Absolute != nil:
		legacy = fmt.Sprintf("%s:abs%04d", seriesGUID, *parsed.Absolute)
	case parsed.SeasonDash:
		legacy = fmt.Sprintf("%s:abs%04d", seriesGUID, *parsed.Episode)
	}
	if legacy == "" {
		return guid, nil
	}
	if cur, err := l.store.GetItemByGUID(ctx, guid); err != nil || cur != nil {
		return guid, err
	}
	old, err := l.store.GetItemByGUID(ctx, legacy)
	if err != nil {
		return "", err
	}
	if old != nil && !old.IsVirtual && old.ParentID != nil && *old.ParentID == seasonID {
		return legacy, nil
	}
	return guid, nil
}

// legacyKeeper is which of the files an older release merged into one item keeps it (and
// its watch history): the one named as its folder's film ("Film (2020)/Film (2020).mkv"
// over "Film (2020)/Deleted Scenes.mkv", which used to keep the item by sorting first,
// moving the film itself to a new item with none), else the first by path.
func legacyKeeper(paths []string) string {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	for _, p := range sorted {
		ft, fy, _ := ParseMovieName(filepath.Base(filepath.Dir(p)))
		stem := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		t, y, _ := ParseMovieName(stem)
		if ft != "" && looseTitle(t) == looseTitle(ft) && (fy == nil || (y != nil && *y == *fy)) {
			return p
		}
	}
	return sorted[0]
}

// looseTitle is a title for comparing: lower case, letters and digits only.
func looseTitle(t string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(t) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// movieID is what a movie file belongs to: the item's GUID and what it is called.
type movieID struct {
	guid  string
	title string
	year  *int
	ids   map[string]string
}

// movieIdentity decides which movie item a file in a movies folder belongs to.
//
// A file directly in the library root is a movie of its own. A file in a folder belongs
// to the folder's film when the folder holds one film: a single video, or videos that are
// all named after the folder ("Film (2022) - 1080p", "Film (2022) - 2160p") or all name
// the same film (versions of it). Title, year and provider ids then come from the folder
// (the file's own ids fill in what the folder lacks), and the identity is the folder.
// Otherwise the folder is a collection ("Studio Collection/Movie A (2022).mkv",
// "Studio Collection/Movie B (2023).mkv", or a series folder holding its films): each film
// is told apart by its own name, title and year, so versions of one film there still share
// an item. Identity used to be the top folder alone, which merged every film in a
// collection folder into one item with one title and one watch history.
//
// A folder that cannot be listed is an error, not a guess: guessing differently from the
// last scan would move the file to another item and leave its watch history behind.
// cleanTitle is the episode parser's title, used when the movie parser finds none.
func movieIdentity(root, path, cleanTitle, existing string) (movieID, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return movieID{}, err
	}
	rel = filepath.ToSlash(rel)
	fileName := filepath.Base(path)
	stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	title, year, ids := ParseMovieName(stem)
	if title == "" {
		title = cleanTitle
	}
	relDir := filepath.ToSlash(filepath.Dir(rel))
	if relDir == "." {
		return movieID{guidFor(ids, "movie", root, rel), title, year, ids}, nil
	}
	// The two readings of a file in a folder: its own film in a collection, or the
	// folder's one film.
	key := strings.ToLower(title)
	if year != nil {
		key = fmt.Sprintf("%s (%d)", key, *year)
	}
	if key == "" {
		key = strings.ToLower(stem)
	}
	own := movieID{guidFor(ids, "movie", root, relDir+"/"+key), title, year, ids}
	folder := movieID{title: title, year: year, ids: ids}
	if t, y, i := ParseMovieName(filepath.Base(relDir)); t != "" {
		folder.title = t
		if y != nil {
			folder.year = y
		}
		for k, v := range ids { // the file's ids fill in what the folder's name lacks
			if i == nil {
				i = map[string]string{}
			}
			if _, ok := i[k]; !ok {
				i[k] = v
			}
		}
		folder.ids = i
	}
	folder.guid = guidFor(folder.ids, "movie", root, relDir)

	one, err := folderHoldsOneFilm(filepath.Dir(path))
	if err != nil {
		return movieID{}, err
	}
	m := own
	if one {
		m = folder
	}
	// A file already in the library keeps the item it has while that is still one of its
	// readings: which reading fits depends on what else is in the folder, and a film that
	// was alone there and is now joined by another (or the reverse) must not move to a
	// new item and leave its watch history behind. Its names follow the folder as it is.
	if existing != "" && (existing == own.guid || existing == folder.guid) {
		m.guid = existing
	}
	return m, nil
}

// folderHoldsOneFilm reports whether the videos directly in dir are one film: at most
// one video, or all named after the folder, or all parsing to the same title and year.
// FolderHoldsOneFilm reports whether a folder is one film's own folder (its versions, its
// extras), not a collection of films: what folder-wide files (movie.nfo, poster.jpg)
// belong to.
func FolderHoldsOneFilm(dir string) (bool, error) { return folderHoldsOneFilm(dir) }

func folderHoldsOneFilm(dir string) (bool, error) {
	entries, err := readDir(dir)
	if err != nil {
		return false, fmt.Errorf("listing %s: %w", dir, err)
	}
	folder := strings.ToLower(filepath.Base(dir))
	var stems []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !videoExts[strings.ToLower(filepath.Ext(n))] || SkipFileReason(n) != "" {
			continue
		}
		if _, _, _, isExtra := ExtraOf("x/" + n); isExtra {
			continue // "Film (2020)-trailer.mkv" is the film's extra, not another version
		}
		stems = append(stems, strings.TrimSuffix(n, filepath.Ext(n)))
	}
	if len(stems) <= 1 {
		return true, nil
	}
	named := true
	films := map[string]bool{}
	for _, st := range stems {
		if !strings.HasPrefix(strings.ToLower(st), folder) {
			named = false
		}
		t, y, _ := ParseMovieName(st)
		k := strings.ToLower(t)
		if y != nil {
			k = fmt.Sprintf("%s (%d)", k, *y)
		}
		films[k] = true
	}
	return named || len(films) == 1, nil
}

func guidFor(ids map[string]string, kind, root, dir string) string {
	for _, p := range []string{"tvdb", "tmdb", "imdb"} {
		if v, ok := ids[p]; ok {
			return fmt.Sprintf("%s:%s:%s", p, kind, v)
		}
	}
	return "path:" + kind + ":" + hashHex(filepath.Join(root, dir))
}

func seasonTitle(n int) string {
	if n == 0 {
		return "Specials"
	}
	return fmt.Sprintf("Season %d", n)
}

func episodeTitle(fileName string, p ParsedName) string {
	stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	// TRaSH: "Series (Year) - S01E01 - 001 - Episode Title [tags]-Group"
	segs := strings.Split(stripBrackets(stem), " - ")
	if len(segs) >= 2 {
		last := strings.TrimSpace(segs[len(segs)-1])
		if i := strings.LastIndex(last, "-"); i > 0 && p.ReleaseGroup != "" && strings.HasSuffix(last, "-"+p.ReleaseGroup) {
			last = strings.TrimSpace(last[:i])
		}
		if last != "" && !reSxxExx.MatchString(last) && !allDigits(last) {
			return last
		}
	}
	if p.Episode != nil {
		return fmt.Sprintf("Episode %d", *p.Episode)
	}
	if p.Absolute != nil {
		return fmt.Sprintf("Episode %d", *p.Absolute)
	}
	return stem
}

// FileEpisodeTitle is the episode title a file name carries (Sonarr's
// "Series - S01E01 - Title [tags]"), or "" when it has none.
func FileEpisodeTitle(fileName string) string {
	p := ParseEpisodeFile(fileName)
	t := strings.TrimSpace(strings.TrimLeft(episodeTitle(fileName, p), "-"))
	stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	if t == stem || t == "" || t == p.ReleaseGroup || strings.HasPrefix(t, "Episode ") && allDigits(strings.TrimPrefix(t, "Episode ")) {
		return ""
	}
	return t
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sidecarSubtitles finds <base>.<lang>[.forced][.hi|.cc|.sdh][.tags].<ext> next to the file.
// A sidecar that is a symlink out of the library root is skipped: it is served as-is to
// any signed-in user, so one pointing at /config/roxy.db would leak the database.
func sidecarSubtitles(videoPath, root string, startIndex int) ([]model.MediaStream, error) {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	// A folder that cannot be listed is an error, not "no subtitles": stored as none, a
	// passing read error would delete every sidecar track of the video.
	entries, err := readDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing subtitles beside %s: %w", filepath.Base(videoPath), err)
	}
	files, err := sidecarFiles(videoPath, root, entries)
	if err != nil {
		return nil, err
	}
	var out []model.MediaStream
	idx := startIndex
	for _, full := range files {
		name := filepath.Base(full)
		ext := strings.ToLower(filepath.Ext(name))
		cut := foldPrefixLen(name, base)
		if cut < 0 {
			continue // sidecarFiles matched it; cannot happen
		}
		rest := strings.TrimSuffix(name[cut:], filepath.Ext(name))
		tokens := strings.Split(strings.Trim(rest, "."), ".")
		st := model.MediaStream{Index: idx, Type: model.StreamSubtitle, IsExternal: true, ExternalPath: filepath.Join(dir, name)}
		fi, err := statFile(st.ExternalPath)
		if errors.Is(err, fs.ErrNotExist) {
			continue // gone since the listing: not a subtitle any more
		}
		if err != nil {
			// The ingest fails and the stored record stays, rather than keeping a track
			// whose details are unknown (see the folder listing above).
			return nil, fmt.Errorf("reading subtitle %s: %w", st.ExternalPath, err)
		}
		st.ExternalSize, st.ExternalModTime = fi.Size(), fi.ModTime().UTC().Truncate(time.Millisecond)
		switch ext {
		case ".ass", ".ssa":
			st.Codec = "ass"
		case ".srt":
			st.Codec = "subrip"
		case ".vtt":
			st.Codec = "webvtt"
		case ".sup":
			st.Codec = "hdmv_pgs_subtitle"
		case ".sub", ".idx":
			st.Codec = "dvd_subtitle"
			// A .sub with no .idx beside it is a text one (MicroDVD), not a VobSub's
			// pictures: it used to be offered as an image track that could not be shown.
			if ext == ".sub" {
				if _, err := statFile(strings.TrimSuffix(st.ExternalPath, filepath.Ext(st.ExternalPath)) + ".idx"); errors.Is(err, fs.ErrNotExist) {
					st.Codec = "microdvd"
				}
			}
		case ".smi", ".sami":
			st.Codec = "sami"
		default:
			st.Codec = strings.TrimPrefix(ext, ".")
		}
		st.IsTextFormat = model.IsTextSubtitleCodec(st.Codec)
		tags := parseSidecarTags(tokens)
		st.Language, st.Title, st.IsForced, st.IsHearingImpaired, st.IsDefault = tags.language, tags.title, tags.forced, tags.hi, tags.isDefault
		if ext == ".sub" && st.Language == "" {
			// A VobSub's language is in its .idx ("id: en, index: 0").
			st.Language = idxLanguage(strings.TrimSuffix(st.ExternalPath, filepath.Ext(st.ExternalPath)) + ".idx")
		}
		out = append(out, st)
		idx++
	}
	return out, nil
}

// foldPrefixLen is how many bytes at the start of name spell prefix, ignoring case, or -1.
// Byte lengths of the two can differ (the Kelvin sign is three bytes, "k" one): slicing
// the name at the prefix's own length cut it in the wrong place, or panicked.
func foldPrefixLen(name, prefix string) int {
	i := 0
	for _, pr := range prefix {
		if i >= len(name) {
			return -1
		}
		nr, size := utf8.DecodeRuneInString(name[i:])
		if !strings.EqualFold(string(nr), string(pr)) {
			return -1
		}
		i += size
	}
	return i
}

// keepSidecarNumbers numbers a source's sidecar streams for a (re-)ingest: each keeps the
// number it had at the last ingest, and new ones go above the source's high-water mark
// (see numberSidecars).
func (l *Library) keepSidecarNumbers(ctx context.Context, sourceID string, streams []model.MediaStream, embedded int) ([]model.MediaStream, map[int]int, error) {
	floor, err := l.store.SidecarNextIndex(ctx, sourceID)
	if err != nil {
		return nil, nil, err
	}
	var prev []model.MediaStream
	if old, err := l.store.GetSource(ctx, sourceID); err == nil && old != nil {
		for _, st := range old.Streams {
			if st.IsExternal && st.ExternalPath != "" {
				prev = append(prev, st)
			}
		}
	}
	out, moved := numberSidecars(streams, prev, floor, embedded)
	return out, moved, nil
}

// sidecarBase is the first number a new sidecar gets: far above any file's own streams
// (ffprobe's numbers), so a re-encode that adds streams cannot reach it.
const sidecarBase = 1000

// numberSidecars gives each sidecar stream the number it had at the last ingest (prev),
// and new sidecars numbers from sidecarBase up, above every number in prev and the
// source's high-water mark (floor, store.SidecarNextIndex: every number a sidecar of it
// has ever had), so no number is ever handed to a different file and a saved subtitle
// choice never moves.
//
// A previous number is kept unless the video's own streams now reach it (embedded: the
// count of ffprobe's numbers; a re-encode with more streams): such a sidecar gets a new
// number, reported in moved (old -> new) so saved choices can follow it. A track inside a
// .mks keeps its number only while it is the same track (language, codec, title, flags);
// remuxed, a track whose position in the .mks changed is found by those instead, so a
// saved English choice never turns into the Spanish track that took its place.
func numberSidecars(streams []model.MediaStream, prev []model.MediaStream, floor, embedded int) ([]model.MediaStream, map[int]int) {
	if len(streams) == 0 {
		return streams, nil
	}
	next := max(sidecarBase, floor)
	byKey := map[string]model.MediaStream{}
	for _, p := range prev {
		byKey[sidecarKey(p)] = p
		if p.Index+1 > next {
			next = p.Index + 1
		}
	}
	sameTrack := func(a, b model.MediaStream) bool {
		return a.Language == b.Language && a.Codec == b.Codec && a.Title == b.Title &&
			a.IsForced == b.IsForced && a.IsHearingImpaired == b.IsHearingImpaired
	}
	// Each stream's previous number, if it has one.
	was := make([]int, len(streams))
	claimed := map[int]bool{}
	for i, st := range streams {
		was[i] = -1
		if p, ok := byKey[sidecarKey(st)]; ok && (st.ExternalIndex == nil || sameTrack(st, p)) && !claimed[p.Index] {
			was[i] = p.Index
			claimed[p.Index] = true
		}
	}
	for i, st := range streams { // .mks tracks that moved inside their file
		if was[i] >= 0 || st.ExternalIndex == nil {
			continue
		}
		for _, p := range prev {
			if p.ExternalPath == st.ExternalPath && p.ExternalIndex != nil && !claimed[p.Index] && sameTrack(st, p) {
				was[i] = p.Index
				claimed[p.Index] = true
				break
			}
		}
	}
	used := map[int]bool{}
	keep := make([]bool, len(streams))
	for i := range streams {
		if n := was[i]; n >= 0 && n >= embedded && !used[n] {
			used[n] = true
			keep[i] = true
		}
	}
	var moved map[int]int
	for i := range streams {
		if keep[i] {
			streams[i].Index = was[i]
			continue
		}
		for used[next] {
			next++
		}
		streams[i].Index = next
		used[next] = true
		if was[i] >= 0 {
			if moved == nil {
				moved = map[int]int{}
			}
			moved[was[i]] = next
		}
		next++
	}
	return streams, moved
}

// sidecarKey identifies a sidecar track across ingests: its file, and for a track inside
// a .mks its stream number there.
func sidecarKey(st model.MediaStream) string {
	if st.ExternalIndex != nil {
		return fmt.Sprintf("%s#%d", st.ExternalPath, *st.ExternalIndex)
	}
	return st.ExternalPath
}

// expandSubtitleContainers replaces each .mks sidecar (a Matroska file holding only
// subtitles, often several) with one stream per text subtitle track in it, with that
// track's language, title and flags (the file name's tags fill in what a track lacks).
// Image tracks (PGS, VobSub) inside one are left out: they cannot be served as files. A
// .mks that cannot be probed is left out and logged. The streams are renumbered in order
// from the first one's number (numberSidecars then keeps earlier numbers).
func (l *Library) expandSubtitleContainers(ctx context.Context, streams []model.MediaStream, known map[string]bool) ([]model.MediaStream, error) {
	if len(streams) == 0 || l.ff == nil {
		return streams, nil
	}
	start := streams[0].Index
	var out []model.MediaStream
	for _, st := range streams {
		if !strings.EqualFold(filepath.Ext(st.ExternalPath), ".mks") {
			out = append(out, st)
			continue
		}
		res, err := probe.File(ctx, l.ff, st.ExternalPath)
		if err != nil {
			if known[st.ExternalPath] {
				// Its tracks are recorded: like any sidecar that cannot be read, the ingest
				// fails and the stored record (the tracks, their numbers) stays. Dropped, a
				// passing failure lost the numbers for good, and saved choices with them.
				return nil, fmt.Errorf("reading subtitle file %s: %w", st.ExternalPath, err)
			}
			slog.Warn("subtitle file not read", "path", st.ExternalPath, "err", err)
			continue
		}
		for _, in := range res.Streams {
			if in.Type != model.StreamSubtitle {
				continue
			}
			if !model.IsTextSubtitleCodec(in.Codec) {
				slog.Info("image subtitle track in a .mks left out", "path", st.ExternalPath, "stream", in.Index, "codec", in.Codec)
				continue
			}
			t := st
			n := in.Index
			t.ExternalIndex = &n
			t.Codec, t.IsTextFormat = in.Codec, true
			if code, _, ok := subtitleLanguage(in.Language); ok {
				t.Language = code
			}
			if in.Title != "" {
				t.Title = in.Title
			}
			t.IsForced = st.IsForced || in.IsForced
			t.IsDefault = st.IsDefault || in.IsDefault
			out = append(out, t)
		}
	}
	for i := range out {
		out[i].Index = start + i
	}
	return out, nil
}

// sidecarTags is what a subtitle file's name says after the video's name.
type sidecarTags struct {
	language, title       string
	forced, hi, isDefault bool
}

// parseSidecarTags reads the dot-separated tags between a video's name and a subtitle's
// extension ("en", "English.forced", "forced.en", "[Group].eng", "pt-BR.sdh"), as
// Jellyfin does: from the right, flags in any order (default; forced or foreign; sdh, cc
// or hi for hearing impaired), the first language found (a code, an English or native
// name, a regional tag, which is also kept in the title), and everything else is the
// title, in its original order. The language used to have to be the first tag and a
// bare 2-3 letter code, so "Show.English.ass" or "Show.forced.en.srt" lost it.
func parseSidecarTags(tokens []string) sidecarTags {
	var t sidecarTags
	var title []string
	for i := len(tokens) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(tokens[i])
		tok := strings.Trim(raw, "[]() ")
		switch lt := strings.ToLower(tok); {
		case lt == "":
		case lt == "default":
			t.isDefault = true
		case lt == "forced" || lt == "foreign":
			t.forced = true
		case lt == "sdh" || lt == "cc" || lt == "hi" || lt == "hearing impaired":
			t.hi = true
		default:
			if t.language == "" {
				if code, region, ok := subtitleLanguage(tok); ok {
					t.language = code
					if region != "" {
						title = append([]string{region}, title...)
					}
					continue
				}
			}
			title = append([]string{tok}, title...)
		}
	}
	t.title = strings.Join(title, " ")
	return t
}

// idxLanguage reads the language of a VobSub from its .idx ("id: en, index: 0"), as an
// ISO 639-2 code; "" when there is none.
func idxLanguage(path string) string {
	b, err := readHead(path, 64<<10)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "id:") {
			continue
		}
		v := strings.TrimSpace(line[3:])
		if i := strings.IndexByte(v, ','); i >= 0 {
			v = v[:i]
		}
		if code, _, ok := subtitleLanguage(v); ok {
			return code
		}
	}
	return ""
}

// sidecarFiles is the sidecar subtitle files of videoPath among its folder's entries, in
// the folder's order: the files sidecarSubtitles makes streams of. A sidecar is named
// <video name>.<anything>.<ext>: the video's name must be followed by a dot, so
// "Show - 10.en.ass" is not taken for "Show - 1.mkv" (a bare prefix match gave episode 1
// episode 10's subtitles). When several videos in the folder fit ("Show - 1.mkv" and
// "Show - 1.5.mkv" for "Show - 1.5.en.ass"), the one with the longest name has it. A .idx
// is left out (the .sub carries the data), and so is a symlink out of the library root or
// to nothing. Any other failure to check a sidecar is an error, never "no such sidecar":
// the caller would otherwise drop a subtitle track that is really there.
func sidecarFiles(videoPath, root string, entries []os.DirEntry) ([]string, error) {
	dir := filepath.Dir(videoPath)
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath)))
	// The other videos here whose names are longer than this one's: a sidecar fitting one
	// of them belongs to it.
	var longer []string
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && videoExts[strings.ToLower(filepath.Ext(n))] {
			// Only a name that itself starts with this one's plus a dot can compete for the
			// same sidecar ("Show - 1.5" against "Show - 1").
			if stem := strings.ToLower(strings.TrimSuffix(n, filepath.Ext(n))); sidecarOf(base, stem) {
				longer = append(longer, stem)
			}
		}
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		lower := strings.ToLower(name)
		if !subtitleExts[ext] || ext == ".idx" || !sidecarOf(base, lower) {
			continue
		}
		taken := false
		for _, stem := range longer {
			if sidecarOf(stem, lower) {
				taken = true
				break
			}
		}
		if taken {
			continue
		}
		full := filepath.Join(dir, name)
		if _, err := safefs.ResolveUnder(full, root); err != nil {
			switch {
			case errors.Is(err, safefs.ErrEscapes):
				slog.Warn("skipping sidecar outside the library", "path", full, "err", err)
			case errors.Is(err, fs.ErrNotExist):
				// Gone since the listing, or a symlink to nothing: not a subtitle.
			default:
				return nil, fmt.Errorf("checking subtitle %s: %w", full, err)
			}
			continue
		}
		out = append(out, full)
	}
	return out, nil
}

// sidecarOf reports whether the file name (lower case) is named after the video stem
// (lower case): the stem, then a dot.
func sidecarOf(stem, name string) bool {
	return len(name) > len(stem) && strings.HasPrefix(name, stem) && name[len(stem)] == '.'
}

func stableID(kind, path string) string {
	// Deterministic per path so re-scans keep the same source/part ids (and thus URLs).
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(kind+":"+filepath.ToSlash(path))).String()
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(s)))
	return hex.EncodeToString(sum[:8])
}

// readHead reads at most n bytes from the start of a file.
func readHead(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}
