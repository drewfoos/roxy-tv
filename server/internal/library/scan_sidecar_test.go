package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// The tests' library has no prober, so a file the scan decides to re-ingest fails and is
// reported by name: that report is how these tests see the scan choosing to re-ingest.
func reingested(t *testing.T, err error, path string) bool {
	t.Helper()
	if err == nil {
		return false
	}
	if !strings.Contains(err.Error(), "could not be added") || !strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("unexpected scan error: %v", err)
	}
	return true
}

// recordSidecars stores sidecar streams for the source of video, as an ingest would.
func recordSidecars(t *testing.T, l *Library, video string, sidecars ...string) {
	t.Helper()
	ctx := context.Background()
	part, err := l.store.GetPartByPath(ctx, video)
	if err != nil || part == nil {
		t.Fatalf("part: %v %v", part, err)
	}
	src, err := l.store.GetSource(ctx, part.SourceID)
	if err != nil || src == nil {
		t.Fatalf("source: %v %v", src, err)
	}
	src.Streams = nil
	for i, p := range sidecars {
		src.Streams = append(src.Streams, model.MediaStream{SourceID: src.ID, Index: i + 1, Type: model.StreamSubtitle, Codec: "ass", IsExternal: true, ExternalPath: p, IsTextFormat: true})
	}
	if err := l.store.ReplaceSource(ctx, src, "{}"); err != nil {
		t.Fatal(err)
	}
}

// A sidecar subtitle added, removed or renamed beside a video that did not change makes a
// normal scan re-ingest that video (it used to be skipped on its size and mtime alone, so
// the track never appeared or never went away). Videos whose sidecars match are skipped.
func TestScanNoticesSidecarChanges(t *testing.T) {
	ctx := context.Background()
	l, _, lib, paths := recordedLibrary(t)
	video := paths[0] // A/one.mkv
	dir := filepath.Dir(video)
	sub := filepath.Join(dir, "one.en.ass")

	// Nothing changed: a clean scan.
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatalf("unchanged library: %v", err)
	}
	// Added.
	if err := os.WriteFile(sub, []byte("[Script Info]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !reingested(t, l.Scan(ctx, lib), video) {
		t.Fatal("a new sidecar did not make the scan re-ingest the video")
	}
	// Recorded (as the ingest would have): skipped again.
	recordSidecars(t, l, video, sub)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatalf("recorded sidecar: %v", err)
	}
	// Renamed.
	renamed := filepath.Join(dir, "one.eng.ass")
	if err := os.Rename(sub, renamed); err != nil {
		t.Fatal(err)
	}
	if !reingested(t, l.Scan(ctx, lib), video) {
		t.Fatal("a renamed sidecar did not make the scan re-ingest the video")
	}
	// Removed.
	recordSidecars(t, l, video, renamed)
	if err := os.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	if !reingested(t, l.Scan(ctx, lib), video) {
		t.Fatal("a removed sidecar did not make the scan re-ingest the video")
	}
	recordSidecars(t, l, video)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatalf("after removal recorded: %v", err)
	}
	// Files that are not this video's sidecars change nothing: another video's subtitle, a
	// .idx, a non-subtitle file, and a sidecar symlinked out of the library (never ingested).
	for _, name := range []string{"two.en.ass", "one.en.idx", "one.en.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret.ass")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "one.de.ass")); err != nil {
		t.Fatal(err)
	}
	err := l.Scan(ctx, lib)
	if err != nil && strings.Contains(err.Error(), "one.mkv") {
		t.Fatalf("one.mkv re-ingested for files that are not its sidecars: %v", err)
	}
	// two.en.ass is new for two.mkv, so that one is re-ingested: right.
	if !reingested(t, err, paths[1]) {
		t.Fatal("two.mkv's new sidecar was not noticed")
	}
}
