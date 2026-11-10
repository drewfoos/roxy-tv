package store

import (
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Before each attachment was kept under a name of its own, two of one name shared one
// cache file (the second written over the first). The upgrade forgets where such a
// file's fonts were extracted, so they are extracted again; other files are left alone.
func TestUpgradeForgetsSameNamedAttachments(t *testing.T) {
	s, lib, ctx := openTest(t)
	at := time.Now().UTC()
	for _, id := range []string{"twins", "plain"} {
		item := &model.Item{ID: "i-" + id, GUID: "g:" + id, LibraryID: lib.ID, Type: model.TypeMovie, Title: id}
		if err := s.UpsertItem(ctx, item); err != nil {
			t.Fatal(err)
		}
		second := "Other.ttf"
		if id == "twins" {
			second = "Font.ttf"
		}
		src := &model.MediaSource{
			ID: id, ItemID: item.ID, Name: id + ".mkv", Container: "mkv", IsPrimary: true,
			Parts: []model.MediaPart{{ID: "p-" + id, SourceID: id, Path: "/media/" + id + ".mkv"}},
			Attachments: []model.Attachment{
				{SourceID: id, Index: 3, Filename: "Font.ttf", ExtractedPath: "/cache/" + id + "/Font.ttf", ExtractedAt: &at},
				{SourceID: id, Index: 4, Filename: second, ExtractedPath: "/cache/" + id + "/" + second, ExtractedAt: &at},
				{SourceID: id, Index: 5, Filename: "Third.ttf", ExtractedPath: "/cache/" + id + "/Third.ttf", ExtractedAt: &at},
			},
		}
		if err := s.ReplaceSource(ctx, src, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = '0045_reextract_same_named_attachments.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	for id, extracted := range map[string]bool{"twins": false, "plain": true} {
		src, err := s.GetSource(ctx, id)
		if err != nil || src == nil || len(src.Attachments) != 3 {
			t.Fatalf("%s: %v %+v", id, err, src)
		}
		for _, a := range src.Attachments {
			if (a.ExtractedPath != "") != extracted || (a.ExtractedAt != nil) != extracted {
				t.Errorf("%s stream %d (%s): extracted at %q, want extracted=%v", id, a.Index, a.Filename, a.ExtractedPath, extracted)
			}
		}
	}
}
