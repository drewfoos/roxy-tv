package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// An extra with a file and no picture offers a thumb (made from a frame when asked):
// apps only ask for the images an item lists, so extras' rows had no thumbnail. An extra
// with a picture keeps its own; one with no file offers none; episodes are unchanged.
func TestExtrasOfferAFrameThumb(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	lib := &model.Library{Name: "A", Kind: model.KindSeries, Root: "/a"}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	show := "show"
	for _, it := range []*model.Item{
		{ID: "show", GUID: "g-show", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"},
		{ID: "op", GUID: "extra:op", LibraryID: lib.ID, Type: model.TypeExtra, Title: "Opening", ParentID: &show},
		{ID: "pic", GUID: "extra:pic", LibraryID: lib.ID, Type: model.TypeExtra, Title: "Trailer", ParentID: &show},
		{ID: "gone", GUID: "extra:gone", LibraryID: lib.ID, Type: model.TypeExtra, Title: "Gone", ParentID: &show},
	} {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"op", "pic"} {
		if err := s.ReplaceSource(ctx, &model.MediaSource{ID: "s-" + id, ItemID: id, Parts: []model.MediaPart{{ID: "p-" + id, Path: "/a/Show/extras/" + id + ".mkv"}}}, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetItemImage(ctx, ItemImage{ItemID: "pic", Type: "thumb", Path: "/x.jpg", FetchedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	extras, err := s.Children(ctx, "show", model.TypeExtra)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, x := range extras {
		got[x.ID] = x.Images["thumb"]
	}
	if got["op"] != ImageURL("op", "thumb") {
		t.Errorf("an extra with a file and no picture offers %q, want the frame thumb", got["op"])
	}
	if got["pic"] == "" || got["pic"] == ImageURL("pic", "thumb") {
		t.Errorf("an extra with its own picture: %q, want its stored image's URL", got["pic"])
	}
	if got["gone"] != "" {
		t.Errorf("an extra with no file offers %q", got["gone"])
	}
}
