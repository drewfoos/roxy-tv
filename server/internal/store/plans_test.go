package store

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A title's audio languages are those of its own file, its children's and its
// grandchildren's, each language once, and of nobody else's.
func TestAudioLanguagesFor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	lib := &model.Library{Name: "Anime", Kind: "series", Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	p := func(v string) *string { return &v }
	items := []*model.Item{
		{ID: "show", Type: model.TypeSeries},
		{ID: "season", Type: model.TypeSeason, ParentID: p("show")},
		{ID: "ep1", Type: model.TypeEpisode, ParentID: p("season")},
		{ID: "ep2", Type: model.TypeEpisode, ParentID: p("season")},
		{ID: "flat", Type: model.TypeEpisode, ParentID: p("show")}, // a show without season folders
		{ID: "film", Type: model.TypeMovie},
		{ID: "other", Type: model.TypeSeries},
		{ID: "other-ep", Type: model.TypeEpisode, ParentID: p("other")},
		{ID: "silent", Type: model.TypeMovie},
	}
	for _, it := range items {
		it.GUID, it.LibraryID, it.Title = "g:"+it.ID, lib.ID, it.ID
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for id, langs := range map[string][]string{
		"ep1": {"jpn", "ENG"}, "ep2": {"jpn"}, "flat": {"fre"}, "film": {"eng", "eng"}, "other-ep": {"ger"}, "silent": {""},
	} {
		src := &model.MediaSource{ID: "src-" + id, ItemID: id, IsPrimary: true, Parts: []model.MediaPart{{ID: "part-" + id, Path: "/m/" + id + ".mkv"}}}
		for i, l := range langs {
			src.Streams = append(src.Streams, model.MediaStream{SourceID: src.ID, Index: i, Type: model.StreamAudio, Codec: "aac", Language: l})
		}
		if err := s.ReplaceSource(ctx, src, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.AudioLanguagesFor(ctx, []string{"show", "film", "silent", "nobody"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"show": {"eng", "fre", "jpn"}, "film": {"eng"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The queries a scan and every title page run look their rows up by parent. Left to
// SQLite (which has no statistics here) they read every real episode for every
// placeholder (0.7 s per library scanned, as a write that everything else waits
// behind) and every episode in the library for one series' list.
func TestQueriesLookUpByParent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	plan := func(q string, args ...any) string {
		rows, err := s.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var lines []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, detail)
		}
		return strings.Join(lines, "\n")
	}
	if p := plan(`DELETE FROM items WHERE ` + shadowedPlaceholders); !strings.Contains(p, "SEARCH r USING INDEX items_parent") {
		t.Errorf("the real episodes of a placeholder's season are not found by parent:\n%s", p)
	}
	if p := plan(`SELECT COUNT(*) FROM `+itemsByParent+` WHERE type IN (?) AND `+underParent, "episode", "x", "x", "x"); !strings.Contains(p, "SEARCH items USING INDEX items_parent") {
		t.Errorf("a series' episodes are not found by parent:\n%s", p)
	}
	if p := plan(`SELECT i.id FROM `+seriesEpisodes+` WHERE se.parent_id = ? AND i.type = 'episode'`, "x"); !strings.Contains(p, "SEARCH se USING INDEX items_parent") || !strings.Contains(p, "SEARCH i USING INDEX items_parent") {
		t.Errorf("a show's next episode is not looked for among its own episodes:\n%s", p)
	}
}
