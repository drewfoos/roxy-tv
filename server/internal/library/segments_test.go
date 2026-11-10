package library

import (
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
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

// A release with segment linking keeps the OP and ED in files of their own. Chapter list
// of a real episode: a 56 s cold open, the OP (in the other file), the episode, a 75 s
// epilogue and the preview. ffprobe listed the OP as this file's 0:00 to 1:31, and "Skip
// intro" jumped over the cold open and half a minute of the episode.
func TestSegmentsFromChapters_LinkedOpeningIsNotAnIntro(t *testing.T) {
	ch := []probe.Chapter{
		{StartMs: 0, EndMs: 55_931, Title: "Intro"},
		{StartMs: 55_931, EndMs: 55_931, Title: "Opening", Linked: true},
		{StartMs: 55_973, EndMs: 1_260_845, Title: "Episode"},
		{StartMs: 1_260_845, EndMs: 1_335_920, Title: "Epilogue"},
		{StartMs: 1_335_920, EndMs: 1_345_971, Title: "Preview"},
	}
	segs := segmentsFromChapters(ch, 1_346_000)
	for _, s := range segs {
		if s.Type == "intro" {
			// Not the linked OP, and not the cold open either, though it is 56 s long
			// and comes first: the OP is known to be elsewhere.
			t.Fatalf("no part of this file is an intro, got %+v", s)
		}
	}
	// The file's own chapters at the end still count.
	if len(segs) != 2 || segs[0].Type != "outro" || segs[0].StartMs != 1_260_845 || segs[1].Type != "preview" {
		t.Fatalf("want the outro and the preview, got %+v", segs)
	}
}

// Both linked: cold open, [OP], parts A and B, [ED], preview. Nothing in the file is an
// intro or an outro, and the 62 s cold open and the 90 s part are not taken for one. The
// preview is still the preview.
func TestSegmentsFromChapters_LinkedOpeningAndEnding(t *testing.T) {
	ch := []probe.Chapter{
		{StartMs: 0, EndMs: 62_000, Title: "Prologue"},
		{StartMs: 62_000, EndMs: 62_000, Title: "Opening", Linked: true},
		{StartMs: 62_000, EndMs: 660_000, Title: "Part A"},
		{StartMs: 660_000, EndMs: 1_190_000, Title: "Part B"},
		{StartMs: 1_190_000, EndMs: 1_280_000, Title: "Part C"}, // 90 s, in the last 5 minutes
		{StartMs: 1_280_000, EndMs: 1_280_000, Title: "Ending", Linked: true},
		{StartMs: 1_280_000, EndMs: 1_300_000, Title: "Preview"},
	}
	segs := segmentsFromChapters(ch, 1_300_000)
	if len(segs) != 1 || segs[0] != (model.MediaSegment{Type: "preview", StartMs: 1_280_000, EndMs: 1_300_000, Origin: "chapters"}) {
		t.Fatalf("OP and ED are in other files: want the preview and nothing else, got %+v", segs)
	}
}

// After a linked ED the last chapter is the preview only when its title says so: it is as
// often a scene that follows the credits (a 42 s "Epilogue" in one real episode), which
// must not get a skip button, and a title like "Chapter 7" says nothing either way.
func TestSegmentsFromChapters_AfterALinkedEnding(t *testing.T) {
	for _, tc := range []struct {
		title   string
		length  int64
		preview bool
	}{
		{"Preview", 11_000, true},
		{"Next Episode", 30_000, true},
		{"次回予告", 15_000, true},
		{"Epilogue", 42_000, false},
		{"Chapter 7", 15_000, false},
		{"Preview", 70_000, false}, // too long to be only a preview
	} {
		const end = 1_300_000
		ch := []probe.Chapter{
			{StartMs: 0, EndMs: 62_000, Title: "Prologue"},
			{StartMs: 62_000, EndMs: 62_000, Title: "Opening", Linked: true},
			{StartMs: 62_000, EndMs: 1_180_000, Title: "Episode"},
			{StartMs: 1_180_000, EndMs: 1_180_000, Title: "Ending", Linked: true},
			{StartMs: 1_180_000, EndMs: end - tc.length, Title: "Sanpo-clip"}, // about a minute of episode
			{StartMs: end - tc.length, EndMs: end, Title: tc.title},
		}
		segs := segmentsFromChapters(ch, end)
		switch {
		case tc.preview && (len(segs) != 1 || segs[0] != (model.MediaSegment{Type: "preview", StartMs: end - tc.length, EndMs: end, Origin: "chapters"})):
			t.Errorf("%q, %d s: want it as the preview and nothing else, got %+v", tc.title, tc.length/1000, segs)
		case !tc.preview && len(segs) != 0:
			t.Errorf("%q, %d s: want no segments, got %+v", tc.title, tc.length/1000, segs)
		}
	}
}

// Only the ED is linked: the OP in the file is still the intro.
func TestSegmentsFromChapters_LinkedEndingKeepsTheIntro(t *testing.T) {
	ch := []probe.Chapter{
		{StartMs: 0, EndMs: 80_000, Title: "Prologue"},
		{StartMs: 80_000, EndMs: 170_000, Title: "Opening"},
		{StartMs: 170_000, EndMs: 1_320_000, Title: "Episode"},
		{StartMs: 1_320_000, EndMs: 1_320_000, Title: "Ending", Linked: true},
		{StartMs: 1_320_000, EndMs: 1_330_000, Title: "Preview"},
	}
	segs := segmentsFromChapters(ch, 1_330_000)
	if len(segs) != 2 || segs[0].Type != "intro" || segs[0].StartMs != 80_000 || segs[0].EndMs != 170_000 || segs[1].Type != "preview" {
		t.Fatalf("want the file's own OP as the intro, the preview, and no outro, got %+v", segs)
	}
}

// A linked chapter is not one of the file's own: a prologue, the episode and an OP that
// plays from another file at the end are two chapters, too few to call the 103 s
// prologue an OP (the layout of a real episode, which got "Skip intro" over its start).
func TestSegmentsFromChapters_LinkedChapterDoesNotMakeThree(t *testing.T) {
	ch := []probe.Chapter{
		{StartMs: 0, EndMs: 102_977, Title: "Prologue"},
		{StartMs: 103_019, EndMs: 1_330_037, Title: "Episode"},
		{StartMs: 1_330_037, EndMs: 1_330_037, Title: "Opening", Linked: true},
	}
	if segs := segmentsFromChapters(ch, 1_330_044); len(segs) != 0 {
		t.Fatalf("want no segments, got %+v", segs)
	}
}
