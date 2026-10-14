package library

import (
	"testing"

	"github.com/roxy-media/roxy/server/internal/probe"
)

func TestSegmentsFromChapters_KonoSubaShape(t *testing.T) {
	// Real chapter layout of a Cerberus KonoSuba S2 episode: cold open, OP, A, B, ED.
	ch := []probe.Chapter{
		{StartMs: 0, EndMs: 18_018},
		{StartMs: 18_018, EndMs: 108_483},
		{StartMs: 108_483, EndMs: 630_505},
		{StartMs: 630_505, EndMs: 1_415_497},
		{StartMs: 1_415_497, EndMs: 1_510_509},
	}
	segs := segmentsFromChapters(ch, 1_520_000)
	if len(segs) != 2 {
		t.Fatalf("want intro+outro, got %+v", segs)
	}
	if segs[0].Type != "intro" || segs[0].StartMs != 18_018 || segs[0].EndMs != 108_483 {
		t.Errorf("intro = %+v", segs[0])
	}
	if segs[1].Type != "outro" || segs[1].StartMs != 1_415_497 {
		t.Errorf("outro = %+v", segs[1])
	}
}

func TestSegmentsFromChapters_ColdOpenBeforeOP(t *testing.T) {
	// S02E01: a 67 s cold open, then the 90 s OP. "First chapter that fits" would take
	// the cold open; the OP is the one closest to 90 s.
	ch := []probe.Chapter{
		{StartMs: 0, EndMs: 66_900},
		{StartMs: 66_900, EndMs: 157_400},
		{StartMs: 157_400, EndMs: 860_400},
		{StartMs: 860_400, EndMs: 1_412_400},
		{StartMs: 1_412_400, EndMs: 1_502_400},
		{StartMs: 1_502_400, EndMs: 1_512_900},
	}
	segs := segmentsFromChapters(ch, 1_512_900)
	if len(segs) != 3 || segs[0].Type != "intro" || segs[0].StartMs != 66_900 || segs[0].EndMs != 157_400 {
		t.Fatalf("intro should be the OP chapter, got %+v", segs)
	}
	if segs[1].Type != "outro" || segs[1].StartMs != 1_412_400 || segs[2].Type != "preview" {
		t.Fatalf("outro/preview wrong: %+v", segs)
	}
}

func TestSegmentsFromChapters_WithPreviewAndNoIntro(t *testing.T) {
	ch := []probe.Chapter{
		{StartMs: 0, EndMs: 600_000},
		{StartMs: 600_000, EndMs: 1_300_000},
		{StartMs: 1_300_000, EndMs: 1_390_000}, // ED 90 s
		{StartMs: 1_390_000, EndMs: 1_420_000}, // preview 30 s
	}
	segs := segmentsFromChapters(ch, 1_420_000)
	if len(segs) != 2 || segs[0].Type != "outro" || segs[1].Type != "preview" {
		t.Fatalf("got %+v", segs)
	}
}

func TestSegmentsFromChapters_NoneForMovies(t *testing.T) {
	ch := []probe.Chapter{{StartMs: 0, EndMs: 900_000}, {StartMs: 900_000, EndMs: 2_400_000}, {StartMs: 2_400_000, EndMs: 5_400_000}}
	if segs := segmentsFromChapters(ch, 5_400_000); len(segs) != 0 {
		t.Fatalf("movie chapters must not yield segments: %+v", segs)
	}
}
