package probe

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

// Test files are built by hand, element by element: ffmpeg cannot write a chapter that
// names another file's segment.

// el encodes one EBML element: id, size, data.
func el(id uint32, data ...[]byte) []byte {
	body := bytes.Join(data, nil)
	var out []byte
	for shift := 24; shift >= 0; shift -= 8 {
		if b := byte(id >> shift); b != 0 || len(out) > 0 {
			out = append(out, b)
		}
	}
	n := len(body)
	switch {
	case n < 0x7F:
		out = append(out, 0x80|byte(n))
	case n < 0x3FFF:
		out = append(out, 0x40|byte(n>>8), byte(n))
	default:
		out = append(out, 0x10|byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	return append(out, body...)
}

// num encodes an unsigned integer in 8 bytes (a fixed length, so positions can be
// filled in after the layout is known).
func num(v uint64) []byte {
	b := make([]byte, 8)
	for i := range b {
		b[i] = byte(v >> (56 - 8*i))
	}
	return b
}

type testAtom struct {
	uid        uint64
	start, end int64 // ms
	title      string
	segment    []byte // ChapterSegmentUID, nil for none
}

func atom(a testAtom) []byte {
	parts := [][]byte{
		el(idChapterUID, num(a.uid)),
		el(idChapterStart, num(uint64(a.start)*1e6)),
		el(idChapterEnd, num(uint64(a.end)*1e6)),
	}
	if a.segment != nil {
		parts = append(parts, el(idChapterSegment, a.segment))
	}
	parts = append(parts, el(idChapterDisplay, el(idChapString, []byte(a.title)), el(0x437C, []byte("eng"))))
	return el(idChapterAtom, parts...)
}

func edition(ordered, def bool, atoms ...testAtom) []byte {
	flag := func(on bool) []byte {
		if on {
			return []byte{1}
		}
		return []byte{0}
	}
	parts := [][]byte{el(idEditionDefault, flag(def)), el(idEditionOrdered, flag(ordered))}
	for _, a := range atoms {
		parts = append(parts, atom(a))
	}
	return el(idEditionEntry, parts...)
}

var (
	ownUID   = bytes.Repeat([]byte{0x11}, 16)
	otherUID = bytes.Repeat([]byte{0xEE}, 16)
	ebmlHead = el(idEBML, el(0x4282, []byte("matroska")))
	infoEl   = el(idInfo, el(idSegmentUID, ownUID), el(0x2AD7B1, []byte{0x0F, 0x42, 0x40}))
	cluster  = el(idCluster, make([]byte, 5000))
)

// The chapters of a real episode whose opening is in a file of its own.
var linkedOpening = []testAtom{
	{uid: 1649421879695464, start: 0, end: 55_931, title: "Intro"},
	{uid: 16494218806955917, start: 0, end: 91_098, title: "Opening", segment: otherUID},
	{uid: 16494218816956182, start: 55_973, end: 1_260_845, title: "Episode"},
	{uid: 16494218826956952, start: 1_260_845, end: 1_335_920, title: "Epilogue"},
	{uid: 16494218836957657, start: 1_335_920, end: 1_345_971, title: "Preview"},
}

// mkvFile lays a file out as mkvmerge does by default: everything before the cluster.
func mkvFile(chapters []byte) []byte {
	return append(append([]byte{}, ebmlHead...), el(idSegment, infoEl, chapters, cluster)...)
}

// mkvFileChaptersLast puts the chapters after the cluster, where only the SeekHead says
// they are (listed: whether it does).
func mkvFileChaptersLast(chapters []byte, listed bool) []byte {
	seekHead := func(pos uint64) []byte {
		entries := [][]byte{el(idSeek, el(idSeekID, []byte{0x15, 0x49, 0xA9, 0x66}), el(idSeekPosition, num(0)))}
		if listed {
			entries = append(entries, el(idSeek, el(idSeekID, []byte{0x10, 0x43, 0xA7, 0x70}), el(idSeekPosition, num(pos))))
		}
		return el(idSeekHead, entries...)
	}
	// Positions count from the start of the segment's data.
	pos := uint64(len(seekHead(0)) + len(infoEl) + len(cluster))
	return append(append([]byte{}, ebmlHead...), el(idSegment, seekHead(pos), infoEl, cluster, chapters)...)
}

func readEditions(t *testing.T, file []byte) []mkvEdition {
	t.Helper()
	eds, err := matroskaEditions(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("reading the chapters: %v", err)
	}
	return eds
}

func TestMatroskaChaptersNameTheLinkedOnes(t *testing.T) {
	chapters := el(idChapters, edition(true, true, linkedOpening...))
	for name, file := range map[string][]byte{
		"before the cluster":              mkvFile(chapters),
		"after it, through the seek head": mkvFileChaptersLast(chapters, true),
	} {
		eds := readEditions(t, file)
		if len(eds) != 1 || !eds[0].Ordered || !eds[0].Default || len(eds[0].Atoms) != 5 {
			t.Fatalf("%s: want one ordered default edition of 5 chapters, got %+v", name, eds)
		}
		for i, a := range eds[0].Atoms {
			if a.Linked != (i == 1) {
				t.Errorf("%s: chapter %d (%s): linked = %v", name, i, a.Title, a.Linked)
			}
		}
		op := eds[0].Atoms[1]
		if op.UID != 16494218806955917 || op.Title != "Opening" || op.StartNs != 0 || op.EndNs != 91_098_000_000 {
			t.Errorf("%s: the linked chapter = %+v", name, op)
		}

		// As the file's chapter list: its own chapters at their own times, and the
		// opening as a mark where the cold open ends.
		got, ok := chaptersOfEditions(eds)
		want := []Chapter{
			{StartMs: 0, EndMs: 55_931, Title: "Intro"},
			{StartMs: 55_931, EndMs: 55_931, Title: "Opening", Linked: true},
			{StartMs: 55_973, EndMs: 1_260_845, Title: "Episode"},
			{StartMs: 1_260_845, EndMs: 1_335_920, Title: "Epilogue"},
			{StartMs: 1_335_920, EndMs: 1_345_971, Title: "Preview"},
		}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: chapters = %+v (ok %v)\nwant %+v", name, got, ok, want)
		}
	}

	// Chapters nothing points at are not found, and that is no error.
	if eds := readEditions(t, mkvFileChaptersLast(chapters, false)); eds != nil {
		t.Errorf("chapters after the cluster that the seek head does not list: got %+v", eds)
	}
	// A segment of unknown size (a file written as a stream) is read to the file's end.
	stream := append(append([]byte{}, ebmlHead...), 0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	stream = append(append(append(stream, infoEl...), chapters...), cluster...)
	if eds := readEditions(t, stream); len(eds) != 1 || !eds[0].Atoms[1].Linked {
		t.Errorf("segment of unknown size: got %+v", eds)
	}
}

func TestMatroskaChaptersThatOnlyLookLinked(t *testing.T) {
	// A chapter naming the file's own segment plays this file.
	self := []testAtom{
		{uid: 1, start: 0, end: 60_000, title: "A", segment: ownUID},
		{uid: 2, start: 60_000, end: 150_000, title: "B", segment: ownUID},
	}
	eds := readEditions(t, mkvFile(el(idChapters, edition(true, true, self...))))
	if len(eds) != 1 || eds[0].Atoms[0].Linked || eds[0].Atoms[1].Linked {
		t.Errorf("chapters naming the file itself were taken for linked: %+v", eds)
	}
	if _, ok := chaptersOfEditions(eds); ok {
		t.Error("a file that links to nothing keeps ffprobe's chapter list")
	}

	// Outside an ordered edition the field plays nothing: the opening's own file has one
	// chapter carrying it, and that chapter is the file's own.
	extra := []testAtom{{uid: 3, start: 0, end: 90_131, title: "Opening", segment: otherUID}}
	eds = readEditions(t, mkvFile(el(idChapters, edition(false, false, extra...))))
	if len(eds) != 1 || eds[0].Atoms[0].Linked {
		t.Errorf("a plain edition's chapter was taken for linked: %+v", eds)
	}
}

func TestOwnChapters(t *testing.T) {
	listed := []Chapter{
		{StartMs: 0, EndMs: 55_931, Title: "Intro"},
		{StartMs: 0, EndMs: 91_098, Title: "Opening"}, // as ffprobe prints the linked one
		{StartMs: 55_973, EndMs: 1_260_845, Title: "Episode"},
	}
	uids := []uint64{10, 0xF000000000000001, 12} // ffprobe prints the second as a negative id

	// No editions read (not found, or the reader failed): ffprobe's list stands.
	if got := ownChapters(nil, listed, uids); !reflect.DeepEqual(got, listed) {
		t.Errorf("without editions: %+v", got)
	}

	// The played edition links to another file: the list is the edition's.
	ordered := mkvEdition{Ordered: true, Default: true, Atoms: []mkvAtom{
		{UID: 10, StartNs: 0, EndNs: 55_931e6, Title: "Intro"},
		{UID: 0xF000000000000001, StartNs: 0, EndNs: 91_098e6, Title: "Opening", Linked: true},
		{UID: 12, StartNs: 55_973e6, EndNs: 1_260_845e6, Title: "Episode"},
	}}
	got := ownChapters([]mkvEdition{ordered}, listed, uids)
	if len(got) != 3 || !got[1].Linked || got[1].StartMs != 55_931 || got[1].EndMs != 55_931 || got[0].Linked || got[2].Linked {
		t.Errorf("ordered edition: %+v", got)
	}

	// The played edition is a plain one and a second, ordered one links out: ffprobe
	// lists the chapters of both, and the linked one is taken out by its id.
	plain := mkvEdition{Default: true, Atoms: []mkvAtom{{UID: 10, EndNs: 55_931e6}, {UID: 12, StartNs: 55_973e6, EndNs: 1_260_845e6}}}
	ordered.Default = false
	got = ownChapters([]mkvEdition{plain, ordered}, listed, uids)
	if len(got) != 2 || got[0].Title != "Intro" || got[1].Title != "Episode" {
		t.Errorf("linked chapter of an edition that is not played: %+v", got)
	}
}

// A damaged or hostile file gives an error or nothing: never a panic, and never a read
// or an allocation the size of what the file claims.
func TestMatroskaChaptersOfDamagedFiles(t *testing.T) {
	chapters := el(idChapters, edition(true, true, linkedOpening...))
	whole := mkvFileChaptersLast(chapters, true)
	for n := 0; n < len(whole); n += 7 {
		_, _ = matroskaEditions(bytes.NewReader(whole[:n]), int64(n))
	}

	if _, err := matroskaEditions(bytes.NewReader([]byte("\x00\x00\x00\x20ftypisom")), 12); !errors.Is(err, errNotMatroska) {
		t.Errorf("an MP4: err = %v, want errNotMatroska", err)
	}

	// A Chapters element that claims a terabyte, in a file that claims to be that long:
	// refused before anything is allocated.
	huge := append([]byte{}, ebmlHead...)
	huge = append(huge, 0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	huge = append(huge, 0x10, 0x43, 0xA7, 0x70, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00)
	if eds, err := matroskaEditions(bytes.NewReader(huge), 1<<41); err == nil {
		t.Errorf("a terabyte of chapters was accepted: %+v", eds)
	}

	// A chapter whose size runs past its edition.
	bad := el(idEditionEntry, []byte{0xB6, 0x90, 0x73, 0xC4, 0x81, 0x01})
	if eds, err := matroskaEditions(bytes.NewReader(mkvFile(el(idChapters, bad))), int64(len(mkvFile(el(idChapters, bad))))); err == nil {
		t.Errorf("a chapter longer than its edition was accepted: %+v", eds)
	}

	// More chapters than any real file has.
	many := make([]testAtom, mkvMaxAtoms+1)
	for i := range many {
		many[i] = testAtom{uid: uint64(i + 1), start: int64(i), end: int64(i + 1), title: "c"}
	}
	file := mkvFile(el(idChapters, edition(false, true, many...)))
	if _, err := matroskaEditions(bytes.NewReader(file), int64(len(file))); err == nil {
		t.Error("an endless chapter list was accepted")
	}
}

func FuzzMatroskaEditions(f *testing.F) {
	chapters := el(idChapters, edition(true, true, linkedOpening...))
	f.Add(mkvFile(chapters))
	f.Add(mkvFileChaptersLast(chapters, true))
	f.Add([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x80, 0x18, 0x53, 0x80, 0x67, 0xFF})
	f.Fuzz(func(t *testing.T, b []byte) {
		eds, _ := matroskaEditions(bytes.NewReader(b), int64(len(b)))
		chaptersOfEditions(eds)
		linkedChapterUIDs(eds)
	})
}
