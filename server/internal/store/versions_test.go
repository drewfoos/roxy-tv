package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Two versions of a film: after ranking, the 2160p one is first and the only primary
// (the apps play the primary); they used to be both primary, in name order (1080p first).
func TestRankVersions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "M", Kind: model.KindMovies, Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertItem(ctx, &model.Item{ID: "f", GUID: "f", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		id, name string
		h        int
	}{{"a", "Film - 1080p.mkv", 1080}, {"b", "Film - 2160p.mkv", 2160}} {
		if err := s.ReplaceSource(ctx, &model.MediaSource{ID: v.id, ItemID: "f", Name: v.name, IsPrimary: true,
			Parts:   []model.MediaPart{{ID: "p" + v.id, Path: "/m/" + v.name}},
			Streams: []model.MediaStream{{Index: 0, Type: model.StreamVideo, Codec: "hevc", Height: v.h}}}, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RankVersions(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	srcs, err := s.SourcesForItem(ctx, "f")
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 || srcs[0].ID != "b" || !srcs[0].IsPrimary || srcs[1].IsPrimary {
		t.Fatalf("order: %s(%v) %s(%v)", srcs[0].ID, srcs[0].IsPrimary, srcs[1].ID, srcs[1].IsPrimary)
	}
}
