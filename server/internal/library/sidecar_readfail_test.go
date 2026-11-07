package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The walk sees the video, but listing its folder for sidecars then fails (a passing
// disk or network error). The scan must not read that as "every sidecar removed": the
// video is not re-ingested, its subtitle tracks stay, nothing is removed, and the scan
// reports the folder. It used to treat the failed listing as an empty folder.
func TestSidecarListingFailureKeepsTracks(t *testing.T) {
	ctx := context.Background()
	l, st, lib, paths := recordedLibrary(t)
	video := paths[0]
	dir := filepath.Dir(video)
	var subs []string
	for _, name := range []string{"one.en.ass", "one.de.srt", "one.fr.ass"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		subs = append(subs, p)
	}
	recordSidecars(t, l, video, subs...)
	if err := l.Scan(ctx, lib); err != nil { // records their details; clean
		t.Fatal(err)
	}
	if err := os.Remove(paths[3]); err != nil { // B/two.mkv really deleted meanwhile
		t.Fatal(err)
	}

	readDir = func(name string) ([]os.DirEntry, error) {
		if name == dir {
			return nil, errors.New("input/output error")
		}
		return os.ReadDir(name)
	}
	t.Cleanup(func() { readDir = os.ReadDir })
	err := l.Scan(ctx, lib)
	if err == nil || !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "nothing was removed") {
		t.Fatalf("scan error: %v", err)
	}
	if strings.Contains(err.Error(), "could not be added") {
		t.Fatalf("the video was re-ingested: %v", err)
	}
	part, _ := st.GetPartByPath(ctx, video)
	src, err := st.GetSource(ctx, part.SourceID)
	if err != nil || src == nil {
		t.Fatal(src, err)
	}
	n := 0
	for _, s := range src.Streams {
		if s.IsExternal {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("%d subtitle tracks left, want 3", n)
	}
	wantRecorded(t, st, lib, paths) // B/two.mkv kept too: no removals on a failed scan

	// The ingest side: a listing that fails is an error, not "no subtitles".
	if got, err := sidecarSubtitles(video, lib.Root, 1); err == nil {
		t.Fatalf("sidecarSubtitles on an unreadable folder: %v, nil error", got)
	}
}
