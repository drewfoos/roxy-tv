package library

import (
	"context"
	"fmt"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

func sidecars(start int, paths ...string) []model.MediaStream {
	var out []model.MediaStream
	for i, p := range paths {
		out = append(out, model.MediaStream{Index: start + i, Type: model.StreamSubtitle, IsExternal: true, ExternalPath: p})
	}
	return out
}

func numbers(streams []model.MediaStream) string {
	m := map[string]int{}
	for _, st := range streams {
		m[st.ExternalPath] = st.Index
	}
	return fmt.Sprint(m)
}

// prevOf is a previous ingest's sidecars (path -> number).
func prevOf(m map[string]int) []model.MediaStream {
	var out []model.MediaStream
	for p, n := range m {
		out = append(out, model.MediaStream{Index: n, Type: model.StreamSubtitle, IsExternal: true, ExternalPath: p})
	}
	return out
}

// A re-ingest keeps each sidecar's stream number, which is what a profile's saved subtitle
// choice holds. A new sidecar that sorts first used to take the old first one's number,
// shifting every choice by one; it now gets a number of its own. New numbers start at
// sidecarBase, above every number a file's own streams use.
func TestSidecarNumbersStayPut(t *testing.T) {
	// First ingest (four embedded streams).
	first, _ := numberSidecars(sidecars(sidecarBase, "ep.en.ass"), nil, 0, 4)
	if got := numbers(first); got != "map[ep.en.ass:1000]" {
		t.Fatalf("first ingest: %s", got)
	}
	// de.srt added; it sorts first, so it arrives first.
	got, _ := numberSidecars(sidecars(sidecarBase, "ep.de.srt", "ep.en.ass"), prevOf(map[string]int{"ep.en.ass": 1000}), 0, 4)
	if numbers(got) != "map[ep.de.srt:1001 ep.en.ass:1000]" {
		t.Fatalf("added: %s", numbers(got))
	}
	// en.ass removed: de.srt keeps 1001, and 1000 is not handed to anything.
	prev := prevOf(map[string]int{"ep.de.srt": 1001, "ep.en.ass": 1000})
	if got, _ := numberSidecars(sidecars(sidecarBase, "ep.de.srt"), prev, 0, 4); numbers(got) != "map[ep.de.srt:1001]" {
		t.Fatalf("removed: %s", numbers(got))
	}
	// Removed and another added in the same scan: the new one gets a number above both.
	if got, _ := numberSidecars(sidecars(sidecarBase, "ep.de.srt", "ep.fr.ass"), prev, 0, 4); numbers(got) != "map[ep.de.srt:1001 ep.fr.ass:1002]" {
		t.Fatalf("replaced: %s", numbers(got))
	}
	// Numbers from before sidecarBase are kept, unless the video, re-encoded, now has a
	// stream of its own there (eight streams: 0-7): that one moves, and says so, so the
	// saved choices for it can follow (they used to land on the embedded track).
	got, moved := numberSidecars(sidecars(sidecarBase, "ep.de.srt", "ep.en.ass"), prevOf(map[string]int{"ep.de.srt": 6, "ep.en.ass": 9}), 0, 8)
	if numbers(got) != "map[ep.de.srt:1000 ep.en.ass:9]" || fmt.Sprint(moved) != "map[6:1000]" {
		t.Fatalf("video grew: %s moved %v", numbers(got), moved)
	}
}

// A .mks remuxed with a track removed: the tracks after it shift down inside the file. A
// track keeps its number only while it is the same track, so a choice saved for the
// removed English track does not become the Spanish one that now sits in its place; the
// Spanish track keeps its own number. Found by an audit.
func TestMKSTrackNumbersFollowTheTrack(t *testing.T) {
	track := func(inner, n int, lang string) model.MediaStream {
		i := inner
		return model.MediaStream{Index: n, Type: model.StreamSubtitle, IsExternal: true, ExternalPath: "ep.mks", ExternalIndex: &i, Language: lang, Codec: "subrip"}
	}
	prev := []model.MediaStream{track(0, 1000, "eng"), track(1, 1001, "spa")}
	got, moved := numberSidecars([]model.MediaStream{track(0, sidecarBase, "spa")}, prev, 1002, 2)
	if len(got) != 1 || got[0].Index != 1001 || moved != nil {
		t.Fatalf("spa after eng was removed: %d (moved %v), want 1001", got[0].Index, moved)
	}
}

// The high-water mark: a sidecar removed in one scan and another added in a later scan.
// Without it, the removed one's number dropped out of the record and the newcomer took it,
// so a choice saved for the removed file pointed at the new one. Runs the ingest's own
// numbering against a real store.
func TestSidecarNumberNeverReused(t *testing.T) {
	l, st, _, paths := recordedLibrary(t)
	ctx := context.Background()
	part, err := st.GetPartByPath(ctx, paths[0])
	if err != nil || part == nil {
		t.Fatal(part, err)
	}
	ingest := func(files ...string) map[string]int {
		t.Helper()
		got, _, err := l.keepSidecarNumbers(ctx, part.SourceID, sidecars(sidecarBase, files...), 4)
		if err != nil {
			t.Fatal(err)
		}
		src, err := st.GetSource(ctx, part.SourceID)
		if err != nil || src == nil {
			t.Fatal(src, err)
		}
		src.Streams = got
		if err := st.ReplaceSource(ctx, src, "{}"); err != nil {
			t.Fatal(err)
		}
		m := map[string]int{}
		for _, s := range got {
			m[s.ExternalPath] = s.Index
		}
		return m
	}
	if m := ingest("ep.de.srt", "ep.en.ass"); m["ep.de.srt"] != 1000 || m["ep.en.ass"] != 1001 {
		t.Fatalf("first ingest: %v", m)
	}
	// en.ass (1001) removed: the next scan records de.srt alone.
	if m := ingest("ep.de.srt"); m["ep.de.srt"] != 1000 {
		t.Fatalf("after removal: %v", m)
	}
	// A later scan finds fr.ass: it must not get 1001, which was en.ass's.
	if m := ingest("ep.de.srt", "ep.fr.ass"); m["ep.de.srt"] != 1000 || m["ep.fr.ass"] != 1002 {
		t.Fatalf("number reused: %v", m)
	}
	if n, err := st.SidecarNextIndex(ctx, part.SourceID); err != nil || n != 1003 {
		t.Fatalf("mark %d %v, want 1003", n, err)
	}
}
