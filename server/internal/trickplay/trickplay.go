// Package trickplay generates seek-preview thumbnails ("trickplay", "BIF", "scrubbing
// thumbnails"): one small frame every IntervalMs tiled into JPEG sheets plus a manifest,
// per media source. Clients show the tile for the hovered time above the seek bar.
//
// Generation decodes keyframes only (-skip_frame nokey), so a 25-minute HEVC episode
// takes seconds, not minutes. ffmpeg writes one small picture per keyframe with its time
// and the sheets are put together here, because a decoder fed keyframes alone hands
// them out in the wrong order (see keyframes). One worker, lowest priority: it runs
// after ingest and a missing set is requested lazily by the API.
package trickplay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/store"
)

const (
	IntervalMs = 10_000
	TileWidth  = 320
	Columns    = 10
	Rows       = 10
	queueSize  = 4096

	// generatorVersion goes into the note left for a file whose previews could not be
	// made. Raise it when generation changes in a way that could make such a file work:
	// notes from another version are ignored, so every one of them is tried again.
	generatorVersion = 2
	// failureAttempts is how many failed tries (an hour or a restart apart) it takes
	// before a file is left alone: one failure may be this machine's (a full disk).
	failureAttempts = 3
	// failureLife is how long that note stops new attempts while the file stays as it is.
	failureLife = 30 * 24 * time.Hour
	// fillBudget and slowGrab bound the single frames fetched by seeking (see fillGaps):
	// all of them together, and one that says seeking in this file is slow.
	fillBudget = time.Minute
	slowGrab   = 3 * time.Second
)

// Manifest describes the sheets of one source. Served as JSON.
type Manifest struct {
	IntervalMs int    `json:"intervalMs"`
	Width      int    `json:"width"`  // tile width
	Height     int    `json:"height"` // tile height
	Columns    int    `json:"columns"`
	Rows       int    `json:"rows"`
	Count      int    `json:"count"`  // number of tiles overall
	Sheets     int    `json:"sheets"` // number of sheet images (0-based: /trickplay/{n})
	Generated  string `json:"generatedAt"`
}

type Service struct {
	store *store.Store
	ff    *ffmpeg.FFmpeg
	cfg   *config.Config

	queue   chan string
	mu      sync.Mutex
	pending map[string]bool
	failed  map[string]time.Time
}

func New(st *store.Store, ff *ffmpeg.FFmpeg, cfg *config.Config) *Service {
	return &Service{store: st, ff: ff, cfg: cfg, queue: make(chan string, queueSize), pending: map[string]bool{}, failed: map[string]time.Time{}}
}

func (s *Service) dir(sourceID string) string {
	return filepath.Join(s.cfg.TrickplayDir(), sourceID)
}

func (s *Service) manifestPath(sourceID string) string {
	return filepath.Join(s.dir(sourceID), "manifest.json")
}

// SheetPath is the file for sheet n (0-based) of a source.
func (s *Service) SheetPath(sourceID string, n int) string {
	return filepath.Join(s.dir(sourceID), fmt.Sprintf("sheet_%03d.jpg", n+1))
}

// Manifest returns the generated manifest, or nil when none exists yet.
func (s *Service) Manifest(sourceID string) *Manifest {
	b, err := os.ReadFile(s.manifestPath(sourceID))
	if err != nil {
		return nil
	}
	var m Manifest
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return &m
}

func (s *Service) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case id := <-s.queue:
				s.mu.Lock()
				delete(s.pending, id)
				s.mu.Unlock()
				s.attempt(ctx, id)
			}
		}
	}()
}

// attempt generates one source's thumbnails and remembers a failure: for an hour in
// memory whatever it was, and in a note on disk when it was the file's own.
func (s *Service) attempt(ctx context.Context, id string) {
	err := s.generate(ctx, id)
	if err == nil || ctx.Err() != nil { // stopping the server is not the file's fault
		return
	}
	slog.Warn("trickplay: generation failed", "source", id, "err", err)
	s.mu.Lock()
	s.failed[id] = time.Now()
	s.mu.Unlock()
	var bad *sourceError
	if errors.As(err, &bad) {
		s.noteFailure(id, bad)
	}
}

// sourceError is a failure that is the media file's own (ffmpeg could not read it, or
// got no picture out of it), as opposed to one of this server's (a full disk, a
// database error). Only these are remembered across restarts.
type sourceError struct {
	path string
	err  error
}

func (e *sourceError) Error() string { return e.err.Error() }
func (e *sourceError) Unwrap() error { return e.err }

// failure is the note kept beside the sheets' folders for a file whose previews could
// not be made, so it is not decoded again at every start: the list in memory was all
// there was, and a file like that was retried by every restart's sweep, forever. After
// failureAttempts tries the file is left alone until it changes, the generator does
// (Version), or the note grows old.
type failure struct {
	Version  int    `json:"version"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"` // unix nanoseconds
	Attempts int    `json:"attempts"`
	Reason   string `json:"reason"`
	At       string `json:"at"` // the last attempt
}

func (s *Service) failurePath(sourceID string) string {
	return FailureNote(s.cfg, sourceID)
}

// FailureNote is where the note about a source's failed previews is kept. The library
// removes it with the source's other caches when the file is replaced.
func FailureNote(cfg *config.Config, sourceID string) string {
	return filepath.Join(cfg.TrickplayDir(), sourceID+".failed")
}

// readFailure returns the note about a source if it still applies: written by this
// generator, about the file as it is now, not too long ago. One that does not is removed.
func (s *Service) readFailure(sourceID string) *failure {
	p := s.failurePath(sourceID)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var f failure
	if json.Unmarshal(b, &f) == nil && f.Version == generatorVersion {
		at, _ := time.Parse(time.RFC3339, f.At)
		if fi, err := os.Stat(f.Path); err == nil && fi.Size() == f.Size && fi.ModTime().UnixNano() == f.ModTime && time.Since(at) < failureLife {
			return &f
		}
	}
	safefs.Remove(p)
	return nil
}

func (s *Service) noteFailure(sourceID string, e *sourceError) {
	fi, err := os.Stat(e.path)
	if err != nil {
		return // the file is not there (an unmounted drive): nothing was learnt about it
	}
	attempts := 1
	if old := s.readFailure(sourceID); old != nil && old.Path == e.path {
		attempts = old.Attempts + 1
	}
	reason := e.Error()
	if len(reason) > 500 {
		reason = reason[:500]
	}
	b, _ := json.MarshalIndent(failure{Version: generatorVersion, Path: e.path, Size: fi.Size(), ModTime: fi.ModTime().UnixNano(),
		Attempts: attempts, Reason: reason, At: time.Now().UTC().Format(time.RFC3339)}, "", "  ")
	if err := safefs.MkdirAll(s.cfg.TrickplayDir(), 0o755); err != nil {
		return
	}
	if err := safefs.WriteFile(s.failurePath(sourceID), b, 0o644); err != nil {
		slog.Warn("trickplay: could not note the failure", "source", sourceID, "err", err)
	}
}

// failedBefore reports whether this very file has failed with this generator often
// enough to be left alone.
func (s *Service) failedBefore(sourceID string) bool {
	f := s.readFailure(sourceID)
	return f != nil && f.Attempts >= failureAttempts
}

// Failed reports whether generating a source's thumbnails failed: within the last hour,
// or earlier for the file as it still is (Enqueue will not try again).
func (s *Service) Failed(sourceID string) bool {
	s.mu.Lock()
	t, ok := s.failed[sourceID]
	s.mu.Unlock()
	return (ok && time.Since(t) < time.Hour) || s.failedBefore(sourceID)
}

// Enqueue schedules generation for a source unless it already has a manifest, is
// queued, or failed (within the last hour, or before as the same file). It reports
// whether the source was queued.
func (s *Service) Enqueue(sourceID string) bool {
	if sourceID == "" || s.Manifest(sourceID) != nil || s.failedBefore(sourceID) {
		return false
	}
	s.mu.Lock()
	if s.pending[sourceID] {
		s.mu.Unlock()
		return false
	}
	if t, ok := s.failed[sourceID]; ok && time.Since(t) < time.Hour {
		s.mu.Unlock()
		return false
	}
	s.pending[sourceID] = true
	s.mu.Unlock()
	select {
	case s.queue <- sourceID:
		return true
	default:
		s.mu.Lock()
		delete(s.pending, sourceID)
		s.mu.Unlock()
		return false
	}
}

// Regenerate drops a source's thumbnails and queues them again (Library > Refresh >
// Trickplay), e.g. after the file was replaced or the sheets came out wrong.
func (s *Service) Regenerate(sourceID string) {
	if sourceID == "" {
		return
	}
	s.mu.Lock()
	running := s.pending[sourceID]
	delete(s.failed, sourceID)
	s.mu.Unlock()
	if running {
		return // already being made
	}
	safefs.Remove(s.failurePath(sourceID))
	safefs.RemoveAll(s.dir(sourceID))
	s.Enqueue(sourceID)
}

// Pending reports whether generation is queued or running for the source.
func (s *Service) Pending(sourceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending[sourceID]
}

// SweepMissing enqueues every source without a manifest (startup, after scans). Sources
// that are not tried again (they failed as the file they still are) are not counted.
func (s *Service) SweepMissing(ctx context.Context) {
	ids, err := s.store.AllSourceIDs(ctx)
	if err != nil {
		return
	}
	n := 0
	for _, id := range ids {
		if s.Enqueue(id) {
			n++
		}
	}
	if n > 0 {
		slog.Info("trickplay: queued", "sources", n)
	}
}

func (s *Service) generate(ctx context.Context, sourceID string) (err error) {
	src, err := s.store.GetSource(ctx, sourceID)
	if err != nil {
		return err
	}
	if src == nil || len(src.Parts) == 0 {
		return fmt.Errorf("source has no file")
	}
	path, err := safefs.MediaPath(src.Parts[0].Path) // never decode a file a symlink leads out of media
	if err != nil {
		return err
	}
	dir := s.dir(sourceID)
	tmp := dir + ".tmp"
	safefs.RemoveAll(tmp)
	if err := safefs.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	// Whatever goes wrong, nothing half-made stays behind (three of the ways out used to
	// leave the .tmp folder).
	defer func() {
		if err != nil {
			safefs.RemoveAll(tmp)
		}
	}()
	start := time.Now()
	frames, err := s.keyframes(ctx, tmp, path, true)
	if err == nil && len(frames) == 0 {
		// No frame the decoder calls a keyframe (some streams flag none): decode everything
		// and keep one frame per interval. Slow, and rare.
		frames, err = s.keyframes(ctx, tmp, path, false)
	}
	if err != nil {
		return &sourceError{path, err}
	}
	if len(frames) == 0 {
		return &sourceError{path, errors.New("ffmpeg produced no frames")}
	}
	interval := float64(IntervalMs) / 1000
	count := int(math.Ceil(float64(src.DurationMs) / float64(IntervalMs)))
	if count <= 0 { // no known length: as far as the pictures go
		last := 0.0
		for _, f := range frames {
			last = math.Max(last, f.at)
		}
		count = int(last/interval) + 1
	}
	picks := pickTiles(frames, count, interval)
	frames, picks = s.fillGaps(ctx, tmp, path, frames, picks, interval)
	if err := ctx.Err(); err != nil {
		return err
	}
	width, height, sheets, err := writeSheets(tmp, frames, picks)
	if err != nil {
		return err
	}
	if sheets == 0 {
		return &sourceError{path, errors.New("no frame ffmpeg wrote could be read")}
	}
	if err := safefs.RemoveAll(filepath.Join(tmp, framesDir)); err != nil {
		return err
	}
	m := Manifest{
		IntervalMs: IntervalMs,
		Width:      width,
		Height:     height,
		Columns:    Columns,
		Rows:       Rows,
		Count:      count,
		Sheets:     sheets,
		Generated:  time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := safefs.WriteFile(filepath.Join(tmp, "manifest.json"), b, 0o644); err != nil {
		return err
	}
	safefs.RemoveAll(dir)
	if err := safefs.Rename(tmp, dir); err != nil {
		return err
	}
	safefs.Remove(s.failurePath(sourceID))
	slog.Info("trickplay: generated", "source", sourceID, "file", filepath.Base(path), "tiles", m.Count, "sheets", m.Sheets, "tile", fmt.Sprintf("%dx%d", m.Width, m.Height), "took", time.Since(start).Round(time.Millisecond))
	return nil
}

// framesDir (inside the work folder) holds the single pictures ffmpeg writes and the
// list of their times.
const (
	framesDir = "frames"
	timesFile = "times.txt"
)

// frame is one picture ffmpeg wrote: its file and its time in the video, in seconds.
type frame struct {
	file string
	at   float64
}

// keyframes has ffmpeg write a small picture for each keyframe of the video (keysOnly),
// or for one frame per interval of a full decode, and returns them with their times, in
// the order ffmpeg wrote them.
//
// That order is not the order of time. A decoder that is fed keyframes alone loses count
// of the frames between them, and HEVC's picture numbers wrap every few hundred frames,
// so it hands a later keyframe out before an earlier one (and drops some as duplicates):
// about one in seven on an ordinary episode. The fps and tile filters this used to run
// through need rising times: previews came out a keyframe off, and a short clip whose
// second keyframe came out first got none at all. So each picture's own time is written
// to a list (the metadata filter), the encoder is given a plain count for a time, and
// the caller sorts.
//
// The scale keeps the picture's shape as it is shown: by display aspect, so a video with
// non-square pixels is not squeezed. Full-range 4:2:0 before the JPEG encoder: it
// refuses limited-range input it cannot map itself.
func (s *Service) keyframes(ctx context.Context, tmp, path string, keysOnly bool) ([]frame, error) {
	dir := filepath.Join(tmp, framesDir)
	safefs.RemoveAll(dir)
	if err := safefs.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	vf := fmt.Sprintf("metadata=mode=add:key=roxy:value=1,metadata=mode=print:file=%s,setpts=N/TB,scale=%s:out_range=full,setsar=1,format=yuvj420p", timesFile, ScaleToWidth(TileWidth))
	args := []string{"-y", "-v", "error"}
	if keysOnly {
		args = append(args, "-skip_frame", "nokey")
	} else {
		vf = fmt.Sprintf("fps=1/%d,", IntervalMs/1000) + vf
	}
	args = append(args, "-i", path, "-an", "-sn", "-dn", "-fps_mode", "passthrough", "-vf", vf, "-qscale:v", "5", "-f", "image2", "kf_%07d.jpg")
	if _, err := s.ff.Run(ctx, dir, args...); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, timesFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	times, err := readTimes(f)
	if err != nil {
		return nil, err
	}
	var frames []frame
	for i, at := range times {
		file := filepath.Join(dir, fmt.Sprintf("kf_%07d.jpg", i+1)) // image2 numbers from 1
		if _, err := os.Stat(file); err != nil {
			continue
		}
		if !math.IsNaN(at) {
			frames = append(frames, frame{file, at})
		}
	}
	return frames, nil
}

// staleTiles lists the tiles whose frame says little about them: it is from more than
// an interval before the tile's middle, or (the earliest there is) from after it.
func staleTiles(frames []frame, picks []int, interval float64) []int {
	var stale []int
	for n, pick := range picks {
		middle := float64(n)*interval + interval/2
		if at := frames[pick].at; at > middle || at <= middle-interval {
			stale = append(stale, n)
		}
	}
	return stale
}

// fillGaps fetches a frame of their own for the stale tiles: the keyframe before the
// tile's middle, found by seeking and decoded as a player would. Usually there is none
// nearer (a long still shot) and the same picture comes back; but the decoder of the
// keyframe pass also drops a keyframe now and then (it takes its confused number for a
// duplicate), and with evenly spaced keyframes far apart it drops many. Seeking may be
// slow (a file with no index is read from its start each time), so this is given up
// when one fetch takes long or all of them do: the tiles keep the frame they had.
func (s *Service) fillGaps(ctx context.Context, tmp, path string, frames []frame, picks []int, interval float64) ([]frame, []int) {
	dir := filepath.Join(tmp, framesDir)
	began := time.Now()
	vf := fmt.Sprintf("scale=%s:out_range=full,setsar=1,format=yuvj420p", ScaleToWidth(TileWidth))
	for _, n := range staleTiles(frames, picks, interval) {
		if ctx.Err() != nil || time.Since(began) > fillBudget {
			break
		}
		middle := float64(n)*interval + interval/2
		name := fmt.Sprintf("fill_%07d.jpg", n)
		one := time.Now()
		_, err := s.ff.Run(ctx, dir, "-y", "-v", "error", "-noaccurate_seek", "-ss", strconv.FormatFloat(middle, 'f', 3, 64), "-i", path,
			"-an", "-sn", "-dn", "-frames:v", "1", "-vf", vf, "-qscale:v", "5", "-f", "image2", name)
		if err == nil {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				frames = append(frames, frame{filepath.Join(dir, name), middle})
				picks[n] = len(frames) - 1
			}
		}
		if time.Since(one) > slowGrab {
			break
		}
	}
	return frames, picks
}

// ScaleToWidth is the arguments of ffmpeg's scale filter for a picture w wide in the
// shape the video is shown in (its display aspect, height even), to be followed by
// setsar=1. "scale=w:-2" keeps the stored shape instead, which is squeezed for video
// with non-square pixels (DVDs, some 1440x1080 broadcasts).
func ScaleToWidth(w int) string {
	return fmt.Sprintf("%d:trunc(%d/dar/2)*2", w, w)
}

// readTimes reads the list ffmpeg's metadata filter prints, one "frame:N pts:P
// pts_time:T" line per frame (and a line per metadata key between them), into the time
// of each frame in the order written. A time that cannot be read is NaN, so the count
// still matches the pictures.
func readTimes(r io.Reader) ([]float64, error) {
	var times []float64
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "frame:") {
			continue
		}
		at := math.NaN()
		for _, field := range strings.Fields(line) {
			if v, ok := strings.CutPrefix(field, "pts_time:"); ok {
				if t, err := strconv.ParseFloat(v, 64); err == nil {
					at = t
				}
			}
		}
		times = append(times, at)
	}
	return times, sc.Err()
}

// pickTiles chooses the frame for each of count tiles: tile n covers the interval
// starting at n*interval and shows the last frame at or before its middle (what the
// video shows there, as far as keyframes can say), or the earliest frame when none comes
// that early. The result holds an index into frames per tile.
func pickTiles(frames []frame, count int, interval float64) []int {
	if len(frames) == 0 || count <= 0 {
		return nil
	}
	order := make([]int, len(frames))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return frames[order[a]].at < frames[order[b]].at })
	picks := make([]int, count)
	next := 0 // the first frame, by time, that is after the tile's middle
	for n := range picks {
		middle := float64(n)*interval + interval/2
		for next < len(order) && frames[order[next]].at <= middle {
			next++
		}
		if next == 0 {
			picks[n] = order[0]
		} else {
			picks[n] = order[next-1]
		}
	}
	return picks
}

// writeSheets puts the picked frames together, Columns x Rows tiles to a sheet, as
// sheet_001.jpg... in dir. Every sheet is full size (unused tiles black), as the tile
// filter made them. It returns the size of a tile and how many sheets it wrote; a frame
// that cannot be read leaves its tile black.
func writeSheets(dir string, frames []frame, picks []int) (width, height, sheets int, err error) {
	var canvas *image.RGBA
	flush := func() error {
		if canvas == nil {
			return nil
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: 72}); err != nil {
			return err
		}
		if err := safefs.WriteFile(filepath.Join(dir, fmt.Sprintf("sheet_%03d.jpg", sheets+1)), buf.Bytes(), 0o644); err != nil {
			return err
		}
		sheets++
		canvas = nil
		return nil
	}
	perSheet := Columns * Rows
	var last image.Image
	lastPick := -1
	for n, pick := range picks {
		if pick != lastPick { // a long shot between keyframes fills several tiles with one frame
			last, lastPick = nil, pick
			if f, err := os.Open(frames[pick].file); err == nil {
				last, _ = jpeg.Decode(f)
				f.Close()
			}
		}
		if last != nil && width == 0 {
			width, height = last.Bounds().Dx(), last.Bounds().Dy()
		}
		if width == 0 {
			continue // nothing readable yet: the size of a tile is not known
		}
		if canvas == nil {
			canvas = image.NewRGBA(image.Rect(0, 0, width*Columns, height*Rows))
			draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
		}
		if last != nil {
			at := image.Pt((n%perSheet%Columns)*width, (n%perSheet/Columns)*height)
			draw.Draw(canvas, image.Rectangle{Min: at, Max: at.Add(image.Pt(width, height))}, last, last.Bounds().Min, draw.Src)
		}
		if n%perSheet == perSheet-1 {
			if err := flush(); err != nil {
				return 0, 0, 0, err
			}
		}
	}
	if err := flush(); err != nil {
		return 0, 0, 0, err
	}
	return width, height, sheets, nil
}
