package store

import (
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A folder delete from an arr app removes that folder only: not a folder whose name
// differs in case, nor one that merely starts with the same characters, nor names that
// LIKE would treat as wildcards.
func TestDeleteSourcesUnderIsExactAndCaseSensitive(t *testing.T) {
	s, lib, ctx := openTest(t)
	paths := map[string]string{
		"target":  "/m/TV/Avatar/S01/e1.mkv",
		"direct":  "/m/TV/Avatar/e2.mkv",
		"lower":   "/m/TV/avatar/S01/e1.mkv",
		"longer":  "/m/TV/Avatar2/e1.mkv",
		"under_":  "/m/TV/AvatarX/e1.mkv",
		"windows": `D:\TV\Avatar\e1.mkv`,
		"winLow":  `D:\TV\avatar\e1.mkv`,
	}
	for name, p := range paths {
		it := &model.Item{ID: "i-" + name, GUID: "g-" + name, LibraryID: lib.ID, Type: model.TypeMovie, Title: name}
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		src := &model.MediaSource{ID: "s-" + name, ItemID: it.ID, Container: "mkv", IsPrimary: true,
			Parts: []model.MediaPart{{ID: "p-" + name, Path: p}}}
		if err := s.ReplaceSource(ctx, src, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.CountSourcesUnder(ctx, "/m/TV/Avatar_"); err != nil || n != 0 {
		t.Fatalf("Avatar_ (a LIKE wildcard) matched %d, %v", n, err)
	}
	if n, err := s.CountSourcesUnder(ctx, "/m/TV/Avatar"); err != nil || n != 2 {
		t.Fatalf("count under /m/TV/Avatar = %d, %v; want 2", n, err)
	}
	n, err := s.DeleteSourcesUnder(ctx, "/m/TV/Avatar/")
	if err != nil || n != 2 {
		t.Fatalf("deleted %d, %v; want 2", n, err)
	}
	if n, err := s.DeleteSourcesUnder(ctx, `D:\TV\Avatar`); err != nil || n != 1 {
		t.Fatalf("windows: deleted %d, %v; want 1", n, err)
	}
	for _, name := range []string{"lower", "longer", "under_", "winLow"} {
		if src, err := s.GetSource(ctx, "s-"+name); err != nil || src == nil {
			t.Errorf("%s (%s) was deleted", name, paths[name])
		}
	}
	for _, name := range []string{"target", "direct", "windows"} {
		if src, _ := s.GetSource(ctx, "s-"+name); src != nil {
			t.Errorf("%s (%s) was kept", name, paths[name])
		}
	}
	if _, err := s.DeleteSourcesUnder(ctx, "/"); err == nil {
		t.Error("deleting under / was allowed")
	}
}
