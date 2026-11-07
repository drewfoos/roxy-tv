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

// A re-ingest keeps each sidecar's stream number, which is what a profile's saved subtitle
// choice holds. A new sidecar that sorts first used to take the old first one's number,
// shifting every choice by one; it now gets a number of its own.
func TestSidecarNumbersStayPut(t *testing.T) {
	// First ingest: en.ass is 5 (after four embedded streams).
	first := numberSidecars(sidecars(5, "ep.en.ass"), nil, 0)
	if got := numbers(first); got != "map[ep.en.ass:5]" {
		t.Fatalf("first ingest: %s", got)
	}
	// de.srt added; it sorts first, so it arrives as 5 and en.ass as 6.
	prev := map[string]int{"ep.en.ass": 5}
	got := numbers(numberSidecars(sidecars(5, "ep.de.srt", "ep.en.ass"), prev, 0))
	if got != "map[ep.de.srt:6 ep.en.ass:5]" {
		t.Fatalf("added: %s", got)
	}
	// en.ass removed: de.srt keeps 6, and 5 is not handed to anything.
	prev = map[string]int{"ep.de.srt": 6, "ep.en.ass": 5}
	if got := numbers(numberSidecars(sidecars(5, "ep.de.srt"), prev, 0)); got != "map[ep.de.srt:6]" {
		t.Fatalf("removed: %s", got)
	}
	// Removed and another added in the same scan: the new one gets a number above both.
	prev = map[string]int{"ep.de.srt": 6, "ep.en.ass": 5}
	if got := numbers(numberSidecars(sidecars(5, "ep.de.srt", "ep.fr.ass"), prev, 0)); got != "map[ep.de.srt:6 ep.fr.ass:7]" {
		t.Fatalf("replaced: %s", got)
	}
	// The video changed and now has more embedded streams (sidecars start at 8): an old
	// number inside the embedded range is not kept, one above it is.
	prev = map[string]int{"ep.de.srt": 6, "ep.en.ass": 9}
	if got := numbers(numberSidecars(sidecars(8, "ep.de.srt", "ep.en.ass"), prev, 0)); got != "map[ep.de.srt:10 ep.en.ass:9]" {
		t.Fatalf("video grew: %s", got)
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
		got, err := l.keepSidecarNumbers(ctx, part.SourceID, sidecars(5, files...))
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
	if m := ingest("ep.de.srt", "ep.en.ass"); m["ep.de.srt"] != 5 || m["ep.en.ass"] != 6 {
		t.Fatalf("first ingest: %v", m)
	}
	// en.ass (6) removed: the next scan records de.srt alone.
	if m := ingest("ep.de.srt"); m["ep.de.srt"] != 5 {
		t.Fatalf("after removal: %v", m)
	}
	// A later scan finds fr.ass: it must not get 6, which was en.ass's.
	if m := ingest("ep.de.srt", "ep.fr.ass"); m["ep.de.srt"] != 5 || m["ep.fr.ass"] != 7 {
		t.Fatalf("number reused: %v", m)
	}
	if n, err := st.SidecarNextIndex(ctx, part.SourceID); err != nil || n != 8 {
		t.Fatalf("mark %d %v, want 8", n, err)
	}
}
