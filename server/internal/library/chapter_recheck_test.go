package library

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// ebml encodes one Matroska element: id, size, data. The test files are written by hand
// because ffmpeg cannot write a chapter that names another file's segment.
func ebml(id uint32, data ...[]byte) []byte {
	body := bytes.Join(data, nil)
	var out []byte
	for shift := 24; shift >= 0; shift -= 8 {
		if b := byte(id >> shift); b != 0 || len(out) > 0 {
			out = append(out, b)
		}
	}
	out = append(out, 0x10|byte(len(body)>>24), byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	return append(out, body...)
}

func ebmlNum(v uint64) []byte {
	b := make([]byte, 8)
	for i := range b {
		b[i] = byte(v >> (56 - 8*i))
	}
	return b
}

type testChapter struct {
	start, end int64 // ms
	title      string
	elsewhere  bool // the chapter plays from another file
}

// mkvWithChapters is the head of a Matroska file with one edition of chapters, ordered
// (a playlist, whose chapters may be in other files) or plain.
func mkvWithChapters(ordered bool, chapters ...testChapter) []byte {
	flag := []byte{0}
	if ordered {
		flag = []byte{1}
	}
	edition := [][]byte{ebml(0x45DB, []byte{1}), ebml(0x45DD, flag)}
	for i, c := range chapters {
		atom := [][]byte{
			ebml(0x73C4, ebmlNum(uint64(i+1))),
			ebml(0x91, ebmlNum(uint64(c.start)*1e6)),
			ebml(0x92, ebmlNum(uint64(c.end)*1e6)),
		}
		if c.elsewhere {
			atom = append(atom, ebml(0x6E67, bytes.Repeat([]byte{0xEE}, 16)))
		}
		atom = append(atom, ebml(0x80, ebml(0x85, []byte(c.title))))
		edition = append(edition, ebml(0xB6, atom...))
	}
	head := ebml(0x1A45DFA3, ebml(0x4282, []byte("matroska")))
	info := ebml(0x1549A966, ebml(0x73A4, bytes.Repeat([]byte{0x11}, 16)))
	return append(head, ebml(0x18538067, info, ebml(0x1043A770, ebml(0x45B9, edition...)), ebml(0x1F43B675, make([]byte, 64)))...)
}

// An older build read a chapter that plays from another file (an opening the release
// keeps in a file of its own) as this file's, and stored it as the intro: 0:00 to 1:31
// of an episode whose first 56 seconds are the cold open and whose opening is not in the
// file at all, so "Skip intro" jumped over the start of the episode. A normal scan does
// not read an unchanged file again, so the stored marker stayed.
func TestRecheckChapterSegments(t *testing.T) {
	l, st, _, paths := recordedLibrary(t)
	ctx := context.Background()
	const duration = 1_345_971

	seg := func(kind string, start, end int64, origin string) model.MediaSegment {
		return model.MediaSegment{Type: kind, StartMs: start, EndMs: end, Origin: origin}
	}
	// stored gives the file at path the segments an older build's scan stored for it.
	stored := func(path string, body []byte, segs ...model.MediaSegment) string {
		t.Helper()
		if body != nil {
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		part, err := st.GetPartByPath(ctx, path)
		if err != nil || part == nil {
			t.Fatalf("part: %v", err)
		}
		src, err := st.GetSource(ctx, part.SourceID)
		if err != nil || src == nil {
			t.Fatalf("source: %v", err)
		}
		src.DurationMs, src.Segments = duration, segs
		if err := st.ReplaceSource(ctx, src, "{}"); err != nil {
			t.Fatal(err)
		}
		return src.ID
	}
	segments := func(id string) []model.MediaSegment {
		t.Helper()
		src, err := st.GetSource(ctx, id)
		if err != nil || src == nil {
			t.Fatalf("source: %v", err)
		}
		return src.Segments
	}
	want := func(what, id string, segs ...model.MediaSegment) {
		t.Helper()
		if got := segments(id); !sameSegments(got, segs) {
			t.Errorf("%s: segments are %+v, want %+v", what, got, segs)
		}
	}
	recheck := func(wantChanged int) {
		t.Helper()
		changed, err := l.RecheckChapterSegments(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if changed != wantChanged {
			t.Errorf("recheck changed %d files, want %d", changed, wantChanged)
		}
	}

	wrongIntro := seg("intro", 0, 91_098, "chapters")
	outro := seg("outro", 1_260_845, 1_335_920, "chapters")
	preview := seg("preview", 1_335_920, 1_345_971, "chapters")
	recap := seg("recap", 60_000, 80_000, "fingerprint")

	// The opening is in another file.
	linked := stored(paths[0], mkvWithChapters(true,
		testChapter{0, 55_931, "Intro", false},
		testChapter{0, 91_098, "Opening", true},
		testChapter{55_973, 1_260_845, "Episode", false},
		testChapter{1_260_845, 1_335_920, "Epilogue", false},
		testChapter{1_335_920, 1_345_971, "Preview", false},
	), wrongIntro, outro, preview, recap)
	// The opening is in the file: its intro is right.
	intro := seg("intro", 60_000, 150_000, "chapters")
	plain := stored(paths[1], mkvWithChapters(false,
		testChapter{0, 60_000, "Cold open", false},
		testChapter{60_000, 150_000, "Opening", false},
		testChapter{150_000, 1_345_971, "Episode", false},
	), intro)
	// Not a Matroska file at all.
	other := stored(paths[2], nil, intro)
	// A file with nothing from its chapters so far (its opening was found by its sound),
	// whose ending is in another file: the chapter it ends on is its preview.
	sound := seg("intro", 0, 89_000, "fingerprint")
	nextTime := seg("preview", 1_335_920, 1_345_971, "chapters")
	bySound := stored(paths[3], mkvWithChapters(true,
		testChapter{0, 89_000, "Opening", false},
		testChapter{89_000, 1_335_920, "Episode", false},
		testChapter{0, 90_000, "Ending", true},
		testChapter{1_335_920, 1_345_971, "Preview", false},
	), sound)

	// With a file not there (an unmounted drive, most likely) nothing is concluded about
	// it, and the pass is not counted as run.
	away := paths[0] + ".away"
	if err := os.Rename(paths[0], away); err != nil {
		t.Fatal(err)
	}
	recheck(1)
	want("file not there", linked, wrongIntro, outro, preview, recap)
	want("ending in another file", bySound, sound, nextTime)
	if err := os.Rename(away, paths[0]); err != nil {
		t.Fatal(err)
	}

	recheck(1)
	want("opening in another file", linked, outro, preview, recap)
	want("opening in the file", plain, intro)
	want("not Matroska", other, intro)
	want("ending in another file", bySound, sound, nextTime)

	// Once is enough: the pass does not read the library again at every start.
	if err := st.ReplaceSegmentsByOrigin(ctx, linked, "chapters", []model.MediaSegment{wrongIntro}); err != nil {
		t.Fatal(err)
	}
	recheck(0)
	want("second run", linked, wrongIntro, recap)
}
