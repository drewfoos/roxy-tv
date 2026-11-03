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
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
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
	// Force makes the next scan re-probe files whose size and mtime are unchanged
	// (after a probe/segment change). Reset to false when the scan completes.
	Force bool

	scanMu   sync.Mutex
	scanning bool
	lastScan time.Time
}

// ScanStatus reports whether a full scan is running and when the last one finished.
func (l *Library) ScanStatus() (running bool, last time.Time) {
	l.scanMu.Lock()
	defer l.scanMu.Unlock()
	return l.scanning, l.lastScan
}

func New(s *store.Store, ff *ffmpeg.FFmpeg, cfg *config.Config) *Library {
	return &Library{store: s, ff: ff, cfg: cfg}
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

func (l *Library) ScanAll(ctx context.Context) error {
	l.scanMu.Lock()
	if l.scanning {
		l.scanMu.Unlock()
		return nil // one at a time; the running scan covers it
	}
	l.scanning = true
	l.scanMu.Unlock()
	defer func() {
		l.scanMu.Lock()
		l.scanning, l.lastScan = false, time.Now()
		l.scanMu.Unlock()
	}()
	libs, err := l.store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		if err := l.Scan(ctx, lib); err != nil {
			slog.Error("scan failed", "library", lib.Name, "err", err)
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
	return nil
}

// Scan walks a library root, adds new/changed files and removes vanished ones.
func (l *Library) Scan(ctx context.Context, lib model.Library) error {
	start := time.Now()
	known, err := l.store.ListPartPaths(ctx, lib.ID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var added, updated, skipped int

	err = filepath.WalkDir(lib.Root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			slog.Warn("walk error", "path", path, "err", walkErr)
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
		seen[path] = true
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if _, ok := known[path]; ok {
			part, err := l.store.GetPartByPath(ctx, path)
			if !l.Force && err == nil && part != nil && part.Size == info.Size() && part.ModTime.Equal(info.ModTime().UTC().Truncate(time.Millisecond)) {
				skipped++
				return nil
			}
			updated++
		} else {
			added++
		}
		if err := l.addFileSafe(ctx, lib, path); err != nil {
			slog.Error("ingest failed", "path", path, "err", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	removed := 0
	for path, sourceID := range known {
		if seen[path] {
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
	slog.Info("scan complete", "library", lib.Name, "added", added, "updated", updated, "unchanged", skipped, "removed", removed, "forced", l.Force, "took", time.Since(start).Round(time.Millisecond))
	l.Force = false
	return nil
}

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

	// Eager attachment extraction (fonts).
	if len(src.Attachments) > 0 {
		dir := filepath.Join(l.cfg.AttachmentsDir(), sourceID)
		if err := probe.ExtractAttachments(ctx, l.ff, path, dir, src.Attachments); err != nil {
			slog.Warn("attachment extraction failed", "path", path, "err", err)
		}
	}

	// Sidecar subtitles next to the file (Bazarr naming).
	src.Streams = append(src.Streams, sidecarSubtitles(path, len(src.Streams)+len(src.Attachments)+1)...)

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
				if err := probe.ExtractTextSubtitle(ctx, l.ff, path, st.Index, st.Codec, textPath); err != nil {
					slog.Warn("subtitle extraction failed", "file", filepath.Base(path), "stream", st.Index, "err", err)
					continue
				}
			}
		}
		b, err := os.ReadFile(textPath)
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
		title, year, ids := ParseMovieName(stem)
		if dir != "" {
			if t, y, i := ParseMovieName(dir); t != "" {
				title, ids = t, i
				if y != nil {
					year = y
				}
			}
		}
		if t := parsed.CleanTitle; title == "" && t != "" {
			title = t
		}
		guid := guidFor(ids, "movie", lib.Root, parts[0])
		it := &model.Item{ID: uuid.NewString(), GUID: guid, LibraryID: lib.ID, Type: model.TypeMovie, Title: title, Year: year, ProviderIDs: ids}
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
func sidecarSubtitles(videoPath string, startIndex int) []model.MediaStream {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []model.MediaStream
	idx := startIndex
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !subtitleExts[ext] || !strings.HasPrefix(strings.ToLower(name), strings.ToLower(base)) {
			continue
		}
		rest := strings.TrimSuffix(name[len(base):], filepath.Ext(name))
		tokens := strings.Split(strings.Trim(rest, "."), ".")
		st := model.MediaStream{Index: idx, Type: model.StreamSubtitle, IsExternal: true, ExternalPath: filepath.Join(dir, name)}
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
		if ext == ".idx" {
			continue // the .sub carries the data; .idx is its index
		}
		out = append(out, st)
		idx++
	}
	return out
}

func stableID(kind, path string) string {
	// Deterministic per path so re-scans keep the same source/part ids (and thus URLs).
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(kind+":"+filepath.ToSlash(path))).String()
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(s)))
	return hex.EncodeToString(sum[:8])
}
