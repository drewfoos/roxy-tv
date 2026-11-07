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
	"strings"
	"sync"
	"time"

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
}

var subtitleExts = map[string]bool{
	".ass": true, ".ssa": true, ".srt": true, ".vtt": true, ".sub": true, ".idx": true, ".sup": true, ".mks": true,
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
	// scanOne scans one library; nil means l.scan. Tests replace it to watch passes.
	scanOne func(ctx context.Context, lib model.Library, force bool) error
}

// ScanStatus reports whether a full scan is running, when the last one finished, and what
// it failed on (nil when every library was scanned).
func (l *Library) ScanStatus() (running bool, last time.Time, lastErr error) {
	l.scanMu.Lock()
	defer l.scanMu.Unlock()
	return l.scanning, l.lastScan, l.lastScanErr
}

func New(s *store.Store, ff *ffmpeg.FFmpeg, cfg *config.Config) *Library {
	return &Library{store: s, ff: ff, cfg: cfg, forcedNext: make(chan struct{}, 1)}
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
		if force {
			select {
			case l.forcedNext <- struct{}{}:
			default: // one is queued already
			}
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
		again := false
		select {
		case <-l.forcedNext:
			// A library that failed does not hold back the forced pass for the others.
			again = ctx.Err() == nil
		default:
		}
		if !again {
			l.scanning, l.lastScan, l.lastScanErr = false, time.Now(), err
			l.scanMu.Unlock()
			return err
		}
		l.scanMu.Unlock()
		slog.Info("a forced scan was requested during the scan: scanning again, forced")
		force = true
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

func (l *Library) scan(ctx context.Context, lib model.Library, force bool) error {
	start := time.Now()
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
		current, err := sidecarFiles(path, lib.Root, ls.entries)
		if err != nil {
			return false, err
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
	// What the walk could not read. Any of it makes this scan a failed one for removals:
	// nothing recorded is deleted until a scan that read everything shows a file is
	// really gone (a read error can mean a failing disk or a half-mounted drive, so what
	// looks missing elsewhere is not trusted either). Walk errors used to be logged and
	// skipped, and everything unseen deleted, so a folder that lost its permissions, or a
	// library root that could not be read at all, emptied the library.
	var unreadable []string
	var rootErr error
	// Files that could not be added or updated (their details unreadable, or a probe or
	// parse failure):
	// the rest of the scan goes on, and the count is reported as a partial failure. They
	// used to be logged only, so such a scan still reported success.
	ingestFailed := 0
	var ingestErrs []string // the first few, "name (reason)"

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
			// Download folders, extras, recycle bins, .ignore'd folders: see skip.go.
			if path != lib.Root && SkipDirReason(path) != "" {
				return filepath.SkipDir
			}
			return nil
		}
		if !videoExts[strings.ToLower(filepath.Ext(path))] || SkipFileReason(d.Name()) != "" {
			return nil
		}
		if _, err := safefs.ResolveUnder(path, lib.Root); err != nil {
			// A symlink out of the library: never ingested, and dropped if it was before.
			slog.Warn("skipping file outside the library", "path", path, "err", err)
			return nil
		}
		seen[path] = true
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
		if err := l.addFileSafe(ctx, lib, path); err != nil {
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
	if rootErr != nil {
		return fmt.Errorf("the library folder %s cannot be read (%v); nothing was removed", lib.Root, rootErr)
	}
	// Files are recorded but the walk found none at all: the drive is most likely not
	// mounted, and what is there is the empty folder Docker creates for a missing bind
	// mount. Removing everything would empty the library, so nothing is.
	if len(seen) == 0 && len(known) > 0 {
		return fmt.Errorf("the library folder %s has no media files but %d are recorded; nothing was removed (is the drive mounted?)", lib.Root, len(known))
	}

	removed := 0
	for path, sourceID := range known {
		if seen[path] || len(unreadable) > 0 {
			continue
		}
		if err := l.store.DeleteSource(ctx, sourceID); err != nil {
			slog.Warn("removing vanished source", "path", path, "err", err)
			continue
		}
		removed++
	}
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
	item, err := l.resolveItemHinted(ctx, lib, path, hint)
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
	found, err := sidecarSubtitles(path, lib.Root, len(src.Streams)+len(src.Attachments)+1)
	if err != nil {
		return err
	}
	sidecars, err := l.keepSidecarNumbers(ctx, sourceID, found)
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

	if err := l.store.ReplaceSource(ctx, src, string(res.Raw)); err != nil {
		return err
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
	if err != nil || part == nil {
		return
	}
	if part.Size == size && part.ModTime.Equal(modTime.UTC().Truncate(time.Millisecond)) {
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
		if st.IsExternal {
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
	parsed := ParseEpisodeFile(fileName)
	// A name with no episode number at all: take the arr app's numbers, or keep the ones
	// this file already has (a hint from an earlier import), so a rescan of a name the
	// parser cannot read does not turn a numbered episode back into a bare filename.
	if lib.Kind != model.KindMovies && parsed.Episode == nil && parsed.Absolute == nil {
		if h := l.episodeHintFor(ctx, path, hint); h != nil {
			parsed.applyHint(h)
		}
	}

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
		it := &model.Item{ID: uuid.NewString(), GUID: m.guid, LibraryID: lib.ID, Type: model.TypeMovie, Title: m.title, Year: m.year, ProviderIDs: m.ids}
		if err := l.store.UpsertItem(ctx, it); err != nil {
			return nil, err
		}
		return it, nil
	}

	// Series library: <root>/<Series dir>/[Season NN/]<file>
	seriesDir := parts[0]
	if len(parts) == 1 {
		seriesDir = parsed.CleanTitle
		if seriesDir == "" {
			seriesDir = strings.TrimSuffix(fileName, filepath.Ext(fileName))
		}
	}
	title, year, ids := ParseSeriesDir(seriesDir)
	series := &model.Item{ID: uuid.NewString(), GUID: guidFor(ids, "series", lib.Root, seriesDir), LibraryID: lib.ID, Type: model.TypeSeries, Title: title, Year: year, ProviderIDs: ids}
	if err := l.store.UpsertItem(ctx, series); err != nil {
		return nil, err
	}

	seasonNum := 1
	if parsed.Season != nil {
		seasonNum = *parsed.Season
	}
	if len(parts) > 2 {
		for _, dir := range parts[1 : len(parts)-1] {
			if n, ok := ParseSeasonDir(dir); ok {
				seasonNum = n
			}
		}
	}
	season := &model.Item{
		ID: uuid.NewString(), GUID: fmt.Sprintf("%s:season:%d", series.GUID, seasonNum), LibraryID: lib.ID, Type: model.TypeSeason,
		ParentID: &series.ID, Title: seasonTitle(seasonNum), IndexAired: &seasonNum,
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
		rest := strings.TrimSuffix(name[len(base):], filepath.Ext(name))
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
		default:
			st.Codec = strings.TrimPrefix(ext, ".")
		}
		st.IsTextFormat = model.IsTextSubtitleCodec(st.Codec)
		for i, t := range tokens {
			switch lt := strings.ToLower(t); {
			case lt == "":
			case lt == "forced":
				st.IsForced = true
			case lt == "hi" || lt == "cc" || lt == "sdh":
				st.IsHearingImpaired = true
			case i == 0 && len(lt) >= 2 && len(lt) <= 3:
				st.Language = lt
			case i == 0 && len(lt) > 3 && strings.Contains(lt, "-"):
				st.Language = strings.SplitN(lt, "-", 2)[0]
			default:
				if st.Title == "" {
					st.Title = t
				} else {
					st.Title += " " + t
				}
			}
		}
		out = append(out, st)
		idx++
	}
	return out, nil
}

// keepSidecarNumbers numbers a source's sidecar streams for a (re-)ingest: each keeps the
// number it had at the last ingest, and new ones go above the source's high-water mark
// (see numberSidecars).
func (l *Library) keepSidecarNumbers(ctx context.Context, sourceID string, streams []model.MediaStream) ([]model.MediaStream, error) {
	floor, err := l.store.SidecarNextIndex(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	var prev map[string]int
	if old, err := l.store.GetSource(ctx, sourceID); err == nil && old != nil {
		prev = map[string]int{}
		for _, st := range old.Streams {
			if st.IsExternal && st.ExternalPath != "" {
				prev[st.ExternalPath] = st.Index
			}
		}
	}
	return numberSidecars(streams, prev, floor), nil
}

// numberSidecars gives each sidecar stream the number it had before (prev: path ->
// number, from the last ingest), and new sidecars numbers above every number in prev, in
// use, and below floor (the source's high-water mark, store.SidecarNextIndex: every
// number a sidecar of it has ever had), so no number is ever handed to a different file
// and a saved subtitle choice never moves. Streams arrive numbered from the first number
// after the embedded streams (sidecarSubtitles); a previous number below that (the video
// itself changed and has more streams now) is not kept.
func numberSidecars(streams []model.MediaStream, prev map[string]int, floor int) []model.MediaStream {
	if len(streams) == 0 || (len(prev) == 0 && floor <= streams[0].Index) {
		return streams
	}
	start := streams[0].Index
	next := start
	if floor > next {
		next = floor
	}
	for _, n := range prev {
		if n >= next {
			next = n + 1
		}
	}
	used := map[int]bool{}
	keep := make([]bool, len(streams))
	for i, st := range streams {
		if n, ok := prev[st.ExternalPath]; ok && n >= start && !used[n] {
			used[n] = true
			keep[i] = true
		}
	}
	for i := range streams {
		if keep[i] {
			streams[i].Index = prev[streams[i].ExternalPath]
			continue
		}
		for used[next] {
			next++
		}
		streams[i].Index = next
		used[next] = true
		next++
	}
	return streams
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
