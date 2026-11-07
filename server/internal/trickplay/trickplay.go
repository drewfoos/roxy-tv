// Package trickplay generates seek-preview thumbnails ("trickplay", "BIF", "scrubbing
// thumbnails"): one small frame every IntervalMs tiled into JPEG sheets plus a manifest,
// per media source. Clients show the tile for the hovered time above the seek bar.
//
// Generation decodes keyframes only (-skip_frame nokey), so a 25-minute HEVC episode
// takes seconds, not minutes. One worker, lowest priority: it runs after ingest and a
// missing set is requested lazily by the API.
package trickplay

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"image"
	_ "image/jpeg"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
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
				if err := s.generate(ctx, id); err != nil {
					slog.Warn("trickplay: generation failed", "source", id, "err", err)
					s.mu.Lock()
					s.failed[id] = time.Now()
					s.mu.Unlock()
				}
			}
		}
	}()
}

// Enqueue schedules generation for a source unless it already has a manifest, is
// queued, or failed within the last hour.
func (s *Service) Enqueue(sourceID string) {
	if sourceID == "" || s.Manifest(sourceID) != nil {
		return
	}
	s.mu.Lock()
	if s.pending[sourceID] {
		s.mu.Unlock()
		return
	}
	if t, ok := s.failed[sourceID]; ok && time.Since(t) < time.Hour {
		s.mu.Unlock()
		return
	}
	s.pending[sourceID] = true
	s.mu.Unlock()
	select {
	case s.queue <- sourceID:
	default:
		s.mu.Lock()
		delete(s.pending, sourceID)
		s.mu.Unlock()
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
	safefs.RemoveAll(s.dir(sourceID))
	s.Enqueue(sourceID)
}

// Pending reports whether generation is queued or running for the source.
func (s *Service) Pending(sourceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending[sourceID]
}

// SweepMissing enqueues every source without a manifest (startup, after scans).
func (s *Service) SweepMissing(ctx context.Context) {
	ids, err := s.store.AllSourceIDs(ctx)
	if err != nil {
		return
	}
	n := 0
	for _, id := range ids {
		if s.Manifest(id) == nil {
			s.Enqueue(id)
			n++
		}
	}
	if n > 0 {
		slog.Info("trickplay: queued", "sources", n)
	}
}

func (s *Service) generate(ctx context.Context, sourceID string) error {
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
	start := time.Now()
	// Keyframes only, one tile per interval, 10x10 tiles per sheet. The fps filter picks
	// the keyframe nearest each 10 s mark; scale keeps the aspect ratio (height even).
	// Full-range 4:2:0 before the JPEG encoder: it refuses limited-range input it cannot
	// map itself (a DVD menu clip failed with "Non full-range YUV is non-standard").
	vf := fmt.Sprintf("fps=1/%d,scale=%d:-2:out_range=full,format=yuvj420p,tile=%dx%d", IntervalMs/1000, TileWidth, Columns, Rows)
	_, err = s.ff.Run(ctx, tmp, "-y", "-v", "error", "-skip_frame", "nokey", "-i", path, "-an", "-sn", "-dn",
		"-vf", vf, "-qscale:v", "5", "-f", "image2", "sheet_%03d.jpg")
	if err != nil {
		safefs.RemoveAll(tmp)
		return err
	}
	entries, _ := os.ReadDir(tmp)
	var sheets []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sheet_") && strings.HasSuffix(e.Name(), ".jpg") {
			sheets = append(sheets, e.Name())
		}
	}
	sort.Strings(sheets)
	if len(sheets) == 0 {
		safefs.RemoveAll(tmp)
		return fmt.Errorf("ffmpeg produced no sheets")
	}
	f, err := os.Open(filepath.Join(tmp, sheets[0]))
	if err != nil {
		return err
	}
	cfgImg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil {
		safefs.RemoveAll(tmp)
		return err
	}
	m := Manifest{
		IntervalMs: IntervalMs,
		Width:      cfgImg.Width / Columns,
		Height:     cfgImg.Height / Rows,
		Columns:    Columns,
		Rows:       Rows,
		Count:      int(math.Ceil(float64(src.DurationMs) / float64(IntervalMs))),
		Sheets:     len(sheets),
		Generated:  time.Now().UTC().Format(time.RFC3339),
	}
	if max := m.Sheets * Columns * Rows; m.Count > max || m.Count == 0 {
		m.Count = max
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := safefs.WriteFile(filepath.Join(tmp, "manifest.json"), b, 0o644); err != nil {
		return err
	}
	safefs.RemoveAll(dir)
	if err := safefs.Rename(tmp, dir); err != nil {
		return err
	}
	slog.Info("trickplay: generated", "source", sourceID, "file", filepath.Base(path), "tiles", m.Count, "sheets", m.Sheets, "tile", fmt.Sprintf("%dx%d", m.Width, m.Height), "took", time.Since(start).Round(time.Millisecond))
	return nil
}
