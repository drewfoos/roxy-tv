package library

import (
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
	first := numberSidecars(sidecars(5, "ep.en.ass"), nil)
	if got := numbers(first); got != "map[ep.en.ass:5]" {
		t.Fatalf("first ingest: %s", got)
	}
	// de.srt added; it sorts first, so it arrives as 5 and en.ass as 6.
	prev := map[string]int{"ep.en.ass": 5}
	got := numbers(numberSidecars(sidecars(5, "ep.de.srt", "ep.en.ass"), prev))
	if got != "map[ep.de.srt:6 ep.en.ass:5]" {
		t.Fatalf("added: %s", got)
	}
	// en.ass removed: de.srt keeps 6, and 5 is not handed to anything.
	prev = map[string]int{"ep.de.srt": 6, "ep.en.ass": 5}
	if got := numbers(numberSidecars(sidecars(5, "ep.de.srt"), prev)); got != "map[ep.de.srt:6]" {
		t.Fatalf("removed: %s", got)
	}
	// Removed and another added in the same scan: the new one gets a number above both.
	prev = map[string]int{"ep.de.srt": 6, "ep.en.ass": 5}
	if got := numbers(numberSidecars(sidecars(5, "ep.de.srt", "ep.fr.ass"), prev)); got != "map[ep.de.srt:6 ep.fr.ass:7]" {
		t.Fatalf("replaced: %s", got)
	}
	// The video changed and now has more embedded streams (sidecars start at 8): an old
	// number inside the embedded range is not kept, one above it is.
	prev = map[string]int{"ep.de.srt": 6, "ep.en.ass": 9}
	if got := numbers(numberSidecars(sidecars(8, "ep.de.srt", "ep.en.ass"), prev)); got != "map[ep.de.srt:10 ep.en.ass:9]" {
		t.Fatalf("video grew: %s", got)
	}
}
