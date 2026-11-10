package probe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// Matroska chapters, read straight from the file.
//
// ffprobe prints a Matroska file's chapters as one flat list, and for a file that uses
// segment linking ("ordered chapters") that list is wrong. Such a release keeps the
// opening and the ending in files of their own, and every episode's chapter list names
// them by segment UID: a chapter like that is not in the episode's file at all, and its
// times count in the other file. ffprobe lists the ones that happen to start at 0:00 as if
// they were the file's own ("Opening", 0:00 to 1:30, on a file whose first 90 seconds are
// the story) and leaves the later ones out without a word.
//
// So the chapter list of such a file is read here. Only three small elements are looked
// at (SeekHead, Info, Chapters), every size is checked before anything is read, and
// nothing is believed without a bound: the file is whatever somebody downloaded.

const (
	idEBML           = 0x1A45DFA3
	idSegment        = 0x18538067
	idSeekHead       = 0x114D9B74
	idSeek           = 0x4DBB
	idSeekID         = 0x53AB
	idSeekPosition   = 0x53AC
	idInfo           = 0x1549A966
	idSegmentUID     = 0x73A4
	idCluster        = 0x1F43B675
	idChapters       = 0x1043A770
	idEditionEntry   = 0x45B9
	idEditionDefault = 0x45DB
	idEditionOrdered = 0x45DD
	idChapterAtom    = 0xB6
	idChapterUID     = 0x73C4
	idChapterStart   = 0x91
	idChapterEnd     = 0x92
	idChapterSegment = 0x6E67
	idChapterDisplay = 0x80
	idChapString     = 0x85

	// mkvMaxElement is the largest SeekHead, Info or Chapters element read into memory.
	// Real ones are a few kilobytes; a file claiming more is not read.
	mkvMaxElement = 4 << 20
	// mkvMaxTopLevel is how many elements of the segment are stepped over while looking
	// for those three before the first cluster. Real files have under a dozen.
	mkvMaxTopLevel = 64
	// mkvMaxAtoms is how many chapters of one file are kept.
	mkvMaxAtoms = 2048
	// mkvUnknownSize is an element whose size field says "until something else begins".
	mkvUnknownSize = -1
)

// errNotMatroska is a file that does not begin with an EBML header.
var errNotMatroska = errors.New("not a Matroska file")

// mkvEdition is one edition of a file's chapters: one way of playing it.
type mkvEdition struct {
	Default bool // the edition a player picks
	Ordered bool // the chapters are a playlist, played in their listed order
	Atoms   []mkvAtom
}

// mkvAtom is one chapter of an edition.
type mkvAtom struct {
	UID     uint64
	StartNs int64
	EndNs   int64 // -1: none given
	Title   string
	// Linked: in an ordered edition, the chapter names a segment that is not this file's:
	// what it plays is in another file, and StartNs/EndNs are times in that file.
	Linked bool
}

// matroskaEditionsOfFile reads the chapter editions of the Matroska file at path.
func matroskaEditionsOfFile(path string) ([]mkvEdition, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return matroskaEditions(f, fi.Size())
}

// matroskaEditions reads the chapter editions of a Matroska file of the given size. A file
// without chapters gives none; one that is not Matroska gives errNotMatroska.
func matroskaEditions(r io.ReaderAt, size int64) ([]mkvEdition, error) {
	e := ebmlFile{r: r, size: size}
	id, data, n, err := e.header(0)
	if err != nil || id != idEBML || n == mkvUnknownSize {
		return nil, errNotMatroska
	}
	segData, segEnd, err := e.segment(data + n)
	if err != nil {
		return nil, err
	}

	// The three elements may sit before the first cluster (found by stepping), or after
	// the last one (found through the SeekHead, whose positions count from segData).
	var info, chapters []byte
	seek := map[uint32]int64{}
	off := segData
scan:
	for i := 0; i < mkvMaxTopLevel && off < segEnd; i++ {
		id, data, n, err := e.header(off)
		if err != nil || n == mkvUnknownSize || id == idCluster {
			break
		}
		switch id {
		case idSeekHead:
			b, err := e.read(data, n)
			if err != nil {
				break scan
			}
			readSeekHead(b, seek)
		case idInfo:
			if info, err = e.read(data, n); err != nil {
				return nil, err
			}
		case idChapters:
			if chapters, err = e.read(data, n); err != nil {
				return nil, err
			}
		}
		off = data + n
	}
	if chapters == nil {
		if chapters, err = e.sought(segData, segEnd, seek, idChapters); err != nil {
			return nil, err
		}
	}
	if chapters == nil {
		return nil, nil
	}
	if info == nil {
		// Without it every chapter that names a segment counts as linked: right for all
		// but a file that names itself.
		info, _ = e.sought(segData, segEnd, seek, idInfo)
	}
	var own []byte
	_ = children(info, func(id uint32, data []byte) error {
		if id == idSegmentUID {
			own = data
		}
		return nil
	})
	return readChapters(chapters, own)
}

// ebmlFile reads EBML elements from a file by position.
type ebmlFile struct {
	r    io.ReaderAt
	size int64
}

// header reads the element header at off: the element's id, where its data begins and
// how long the data is (mkvUnknownSize when the header does not say).
func (e ebmlFile) header(off int64) (id uint32, data, n int64, err error) {
	if off < 0 || off >= e.size {
		return 0, 0, 0, io.ErrUnexpectedEOF
	}
	var buf [12]byte // an id is at most 4 bytes, a size at most 8
	got, err := e.r.ReadAt(buf[:], off)
	if got == 0 && err != nil {
		return 0, 0, 0, err
	}
	id, idLen, n, nLen, err := elementHeader(buf[:got])
	if err != nil {
		return 0, 0, 0, err
	}
	data = off + int64(idLen+nLen)
	if n != mkvUnknownSize && n > e.size-data {
		return 0, 0, 0, fmt.Errorf("element %X at %d is longer than the file", id, off)
	}
	return id, data, n, nil
}

// segment finds the Segment element at or after off (a Void may come before it) and
// returns where its data begins and ends.
func (e ebmlFile) segment(off int64) (data, end int64, err error) {
	for i := 0; i < 4; i++ {
		id, data, n, err := e.header(off)
		if err != nil {
			return 0, 0, err
		}
		if id == idSegment {
			if n == mkvUnknownSize {
				return data, e.size, nil
			}
			return data, data + n, nil
		}
		if n == mkvUnknownSize {
			break
		}
		off = data + n
	}
	return 0, 0, errors.New("no Matroska segment")
}

// read returns the n bytes of an element's data, refusing one too large to be what is
// looked for here.
func (e ebmlFile) read(data, n int64) ([]byte, error) {
	if n < 0 || n > mkvMaxElement {
		return nil, fmt.Errorf("element of %d bytes is not read", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(e.r, data, n), b); err != nil {
		return nil, err
	}
	return b, nil
}

// sought reads the element with the given id from the place the SeekHead gives for it;
// nil when the SeekHead does not list it or something else is there.
func (e ebmlFile) sought(segData, segEnd int64, seek map[uint32]int64, want uint32) ([]byte, error) {
	pos, ok := seek[want]
	if !ok || pos < 0 || pos >= segEnd-segData {
		return nil, nil
	}
	id, data, n, err := e.header(segData + pos)
	if err != nil || id != want || n == mkvUnknownSize {
		return nil, nil
	}
	return e.read(data, n)
}

// elementHeader decodes an element id and size from the start of b. The id keeps its
// length marker (as ids are written everywhere); the size does not.
func elementHeader(b []byte) (id uint32, idLen int, n int64, nLen int, err error) {
	if len(b) == 0 {
		return 0, 0, 0, 0, io.ErrUnexpectedEOF
	}
	idLen = vintLen(b[0])
	if idLen == 0 || idLen > 4 || len(b) < idLen {
		return 0, 0, 0, 0, errors.New("bad element id")
	}
	for _, c := range b[:idLen] {
		id = id<<8 | uint32(c)
	}
	b = b[idLen:]
	if len(b) == 0 {
		return 0, 0, 0, 0, io.ErrUnexpectedEOF
	}
	nLen = vintLen(b[0])
	if nLen == 0 || len(b) < nLen {
		return 0, 0, 0, 0, errors.New("bad element size")
	}
	v := uint64(b[0]) & (0xFF >> nLen)
	ones := v == uint64(0xFF>>nLen)
	for _, c := range b[1:nLen] {
		v = v<<8 | uint64(c)
		ones = ones && c == 0xFF
	}
	if ones {
		return id, idLen, mkvUnknownSize, nLen, nil
	}
	if v > 1<<62 {
		return 0, 0, 0, 0, errors.New("bad element size")
	}
	return id, idLen, int64(v), nLen, nil
}

// vintLen is the length in bytes of the variable-length integer whose first byte is c:
// one more than its leading zero bits; 0 when c is 0, which starts none.
func vintLen(c byte) int {
	for i := 0; i < 8; i++ {
		if c&(0x80>>i) != 0 {
			return i + 1
		}
	}
	return 0
}

// children calls fn for each element directly inside the master element whose data is b.
// It stops with an error at the first element that does not fit.
func children(b []byte, fn func(id uint32, data []byte) error) error {
	for len(b) > 0 {
		id, idLen, n, nLen, err := elementHeader(b)
		if err != nil {
			return err
		}
		b = b[idLen+nLen:]
		if n == mkvUnknownSize || n > int64(len(b)) {
			return errors.New("element does not fit its parent")
		}
		if err := fn(id, b[:n]); err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

// ebmlUint decodes an unsigned integer element (1 to 8 bytes, big-endian).
func ebmlUint(b []byte) uint64 {
	if len(b) > 8 {
		b = b[len(b)-8:]
	}
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// readSeekHead notes where a SeekHead says each top-level element is, first mention wins.
func readSeekHead(b []byte, seek map[uint32]int64) {
	_ = children(b, func(id uint32, entry []byte) error {
		if id != idSeek {
			return nil
		}
		var target uint32
		pos := int64(-1)
		_ = children(entry, func(id uint32, data []byte) error {
			switch id {
			case idSeekID:
				if len(data) <= 4 {
					target = uint32(ebmlUint(data))
				}
			case idSeekPosition:
				if v := ebmlUint(data); v < 1<<62 {
					pos = int64(v)
				}
			}
			return nil
		})
		if _, seen := seek[target]; target != 0 && pos >= 0 && !seen {
			seek[target] = pos
		}
		return nil
	})
}

// readChapters decodes a Chapters element. own is the file's own segment UID: a chapter
// of an ordered edition that names any other segment is linked.
func readChapters(b, own []byte) ([]mkvEdition, error) {
	var eds []mkvEdition
	atoms := 0
	err := children(b, func(id uint32, edition []byte) error {
		if id != idEditionEntry {
			return nil
		}
		var ed mkvEdition
		var named []bool // per atom: it names a segment that is not this file's
		err := children(edition, func(id uint32, data []byte) error {
			switch id {
			case idEditionDefault:
				ed.Default = ebmlUint(data) != 0
			case idEditionOrdered:
				ed.Ordered = ebmlUint(data) != 0
			case idChapterAtom:
				if atoms++; atoms > mkvMaxAtoms {
					return errors.New("too many chapters")
				}
				a, other, err := readAtom(data, own)
				if err != nil {
					return err
				}
				ed.Atoms = append(ed.Atoms, a)
				named = append(named, other)
			}
			return nil
		})
		if err != nil {
			return err
		}
		// Only an ordered edition plays other files: elsewhere the field means nothing
		// (the opening's own file carries it too, naming itself or its series).
		for i := range ed.Atoms {
			ed.Atoms[i].Linked = ed.Ordered && named[i]
		}
		eds = append(eds, ed)
		return nil
	})
	return eds, err
}

// readAtom decodes one ChapterAtom; other reports that it names a segment UID that is
// not own. Chapters nested inside it are left out.
func readAtom(b, own []byte) (a mkvAtom, other bool, err error) {
	a.EndNs = -1
	titled := false
	err = children(b, func(id uint32, data []byte) error {
		switch id {
		case idChapterUID:
			a.UID = ebmlUint(data)
		case idChapterStart:
			if v := ebmlUint(data); v < 1<<62 {
				a.StartNs = int64(v)
			}
		case idChapterEnd:
			if v := ebmlUint(data); v < 1<<62 {
				a.EndNs = int64(v)
			}
		case idChapterSegment:
			other = len(data) > 0 && !bytes.Equal(data, own)
		case idChapterDisplay:
			if titled {
				return nil // the first display is the title; others are translations
			}
			return children(data, func(id uint32, s []byte) error {
				if id == idChapString && !titled {
					if len(s) > 256 {
						s = s[:256]
					}
					a.Title, titled = string(bytes.ToValidUTF8(s, nil)), true
				}
				return nil
			})
		}
		return nil
	})
	return a, other, err
}

// chaptersOfEditions turns a Matroska file's editions into its chapter list, for a file
// whose played edition (the one flagged default, else the first) is ordered and links to
// other files: ok is false for every other file, whose ffprobe list is right.
//
// The file's own chapters keep their times. A linked chapter becomes a mark of no length
// at the place in this file where the other file's part would play, flagged Linked.
func chaptersOfEditions(eds []mkvEdition) (chapters []Chapter, ok bool) {
	if len(eds) == 0 {
		return nil, false
	}
	played := eds[0]
	for _, ed := range eds {
		if ed.Default {
			played = ed
			break
		}
	}
	linked := false
	for _, a := range played.Atoms {
		linked = linked || a.Linked
	}
	if !played.Ordered || !linked {
		return nil, false
	}
	var at int64 // where in this file the edition has got to, in ms
	for _, a := range played.Atoms {
		if a.Linked {
			chapters = append(chapters, Chapter{StartMs: at, EndMs: at, Title: a.Title, Linked: true})
			continue
		}
		c := Chapter{StartMs: a.StartNs / 1e6, EndMs: a.EndNs / 1e6, Title: a.Title}
		if c.EndMs < c.StartMs {
			c.EndMs = c.StartMs
		}
		chapters = append(chapters, c)
		at = c.EndMs
	}
	return chapters, true
}

// linkedChapterUIDs lists the UIDs of every linked chapter in the editions: chapters
// that must never be taken for the file's own, whichever edition they are in.
func linkedChapterUIDs(eds []mkvEdition) map[uint64]bool {
	var uids map[uint64]bool
	for _, ed := range eds {
		for _, a := range ed.Atoms {
			if a.Linked {
				if uids == nil {
					uids = map[uint64]bool{}
				}
				uids[a.UID] = true
			}
		}
	}
	return uids
}

// LinkedChapters is the chapter list of a Matroska file whose played edition keeps parts
// in other files, as File gives it (see chaptersOfEditions). ok is false for every other
// file, Matroska or not: its chapters are all its own. It reads only the file's chapter
// list, without ffprobe, so it is cheap enough to ask of a whole library.
func LinkedChapters(path string) (chapters []Chapter, ok bool, err error) {
	eds, err := matroskaEditionsOfFile(path)
	if errors.Is(err, errNotMatroska) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	chapters, ok = chaptersOfEditions(eds)
	return chapters, ok, nil
}
