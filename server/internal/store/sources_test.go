package store

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/roxy-media/roxy/server/internal/model"
)

// The batched lookup a listing page uses returns each item's own sources, primary first,
// each with its own parts, streams and attachments (checked against what was inserted,
// so sources or children landing on the wrong item would show), across more items than
// fit in one IN list.
func TestSourcesForItems(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "L", Kind: model.KindMovies, Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	type want struct{ srcID, path string }
	expect := map[string][]want{} // item -> sources in the order they must come back
	var ids []string
	for i := 0; i < 520; i++ { // past one 500-id chunk
		it := &model.Item{ID: uuid.NewString(), GUID: fmt.Sprintf("test:%d", i), LibraryID: lib.ID, Type: model.TypeMovie, Title: fmt.Sprint(i)}
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
		if i%7 == 0 {
			continue // some items have no source
		}
		var mine []want
		for v := 0; v < 1+i%2; v++ { // some have two versions; the second is primary
			src := &model.MediaSource{ID: uuid.NewString(), ItemID: it.ID, Name: fmt.Sprintf("v%d", v), Container: "mkv", Size: 100, IsPrimary: v == 1 || i%2 == 0,
				Parts:       []model.MediaPart{{ID: uuid.NewString(), Path: fmt.Sprintf("/m/%d-%d.mkv", i, v), Size: 100, ModTime: time.Now()}},
				Streams:     []model.MediaStream{{Index: 0, Type: model.StreamVideo, Codec: "hevc"}, {Index: 1, Type: model.StreamAudio, Codec: "aac", Language: fmt.Sprintf("l%d", i)}},
				Attachments: []model.Attachment{{Index: 2, Filename: fmt.Sprintf("font%d-%d.ttf", i, v), Mime: "font/ttf"}},
			}
			if err := s.ReplaceSource(ctx, src, "{}"); err != nil {
				t.Fatal(err)
			}
			mine = append(mine, want{src.ID, src.Parts[0].Path})
		}
		if len(mine) == 2 {
			mine[0], mine[1] = mine[1], mine[0] // primary first
		}
		expect[it.ID] = mine
	}
	got, err := s.SourcesForItems(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		srcs := got[id]
		if len(srcs) != len(expect[id]) {
			t.Fatalf("item %d: %d sources, want %d", i, len(srcs), len(expect[id]))
		}
		for k, src := range srcs {
			w := expect[id][k]
			if src.ID != w.srcID || src.ItemID != id {
				t.Fatalf("item %d source %d: got %s (item %s), want %s", i, k, src.ID, src.ItemID, w.srcID)
			}
			if len(src.Parts) != 1 || src.Parts[0].Path != w.path || src.Parts[0].SourceID != src.ID {
				t.Fatalf("item %d source %d: parts %+v, want %s", i, k, src.Parts, w.path)
			}
			if len(src.Streams) != 2 || src.Streams[1].Language != fmt.Sprintf("l%d", i) || src.Streams[0].SourceID != src.ID {
				t.Fatalf("item %d source %d: streams %+v", i, k, src.Streams)
			}
			if len(src.Attachments) != 1 || src.Attachments[0].SourceID != src.ID || !strings.HasPrefix(src.Attachments[0].Filename, fmt.Sprintf("font%d-", i)) {
				t.Fatalf("item %d source %d: attachments %+v", i, k, src.Attachments)
			}
		}
	}
	// The per-item lookup and GetSource go through the same code and agree with it.
	one, err := s.SourcesForItem(ctx, ids[1])
	if err != nil || !reflect.DeepEqual(one, got[ids[1]]) {
		t.Fatalf("SourcesForItem: %+v %v", one, err)
	}
	g, err := s.GetSource(ctx, got[ids[1]][0].ID)
	if err != nil || g == nil || !reflect.DeepEqual(*g, got[ids[1]][0]) {
		t.Fatalf("GetSource: %+v %v", g, err)
	}
}

// The audio fingerprints belong to the file's sound. A re-read of the same file keeps
// them (they take a decode of sixteen minutes of audio to make); another file at the
// same path (a re-download, an upgrade) drops them, or intros and credits are matched
// from the audio of the file that is gone.
func TestFingerprintsFollowTheFile(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertItem(ctx, &model.Item{ID: "m", GUID: "g-m", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"}); err != nil {
		t.Fatal(err)
	}
	file := func(size int64, mtime time.Time) *model.MediaSource {
		return &model.MediaSource{ID: "src", ItemID: "m", IsPrimary: true,
			Parts: []model.MediaPart{{ID: "part", Path: filepath.Join(lib.Root, "Film.mkv"), Size: size, ModTime: mtime}}}
	}
	at := time.Unix(1700000000, 0)
	prints := func() int {
		n := 0
		for _, kind := range []string{"head", "tail"} {
			if fp, err := s.GetFingerprint(ctx, "src", kind); err != nil {
				t.Fatal(err)
			} else if fp != nil {
				n++
			}
		}
		return n
	}
	put := func() {
		for _, kind := range []string{"head", "tail"} {
			if err := s.PutFingerprint(ctx, &Fingerprint{SourceID: "src", Kind: kind, LengthMs: 1000, Points: []uint32{1, 2, 3}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.ReplaceSource(ctx, file(1000, at), "{}"); err != nil {
		t.Fatal(err)
	}
	put()
	// The same file read in again (a forced scan, a new subtitle next to it).
	if err := s.ReplaceSource(ctx, file(1000, at), "{}"); err != nil {
		t.Fatal(err)
	}
	if prints() != 2 {
		t.Fatalf("a re-read of the same file dropped its fingerprints: %d left", prints())
	}
	// Another file under the same name: by size, then by time alone.
	if err := s.ReplaceSource(ctx, file(2500, at), "{}"); err != nil {
		t.Fatal(err)
	}
	if prints() != 0 {
		t.Fatalf("a bigger file at the same path kept the old file's fingerprints: %d", prints())
	}
	put()
	if err := s.ReplaceSource(ctx, file(2500, at.Add(time.Hour)), "{}"); err != nil {
		t.Fatal(err)
	}
	if prints() != 0 {
		t.Fatalf("a newer file of the same size kept the old file's fingerprints: %d", prints())
	}
}
