package probe

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// A file ffmpeg wrote, probed by the real ffprobe: first as it is (its chapters are its
// own and stay as ffprobe lists them), then with its second chapter turned into one that
// plays another file, the way releases with a shared opening are made. ffprobe goes on
// listing that chapter as the file's own; File must not.
func TestLinkedChaptersOfARealFile(t *testing.T) {
	ff := needFFmpeg(t)
	base := t.TempDir()
	const linkedTitle = "Opening kept in another file"
	meta := ";FFMETADATA1\n" +
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=2000\ntitle=Cold open\n" +
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=2000\nEND=5000\ntitle=" + linkedTitle + "\n" +
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=5000\nEND=8000\ntitle=Episode\n"
	metaPath := filepath.Join(base, "meta.txt")
	if err := os.WriteFile(metaPath, []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(base, "plain.mkv")
	out, err := exec.Command(ff.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=duration=8:size=160x120:rate=5",
		"-i", metaPath, "-map_metadata", "1", "-map_chapters", "1", "-c:v", "mpeg4", plain).CombinedOutput()
	if err != nil {
		t.Fatalf("making the video: %v\n%s", err, out)
	}

	own := []Chapter{
		{StartMs: 0, EndMs: 2000, Title: "Cold open"},
		{StartMs: 2000, EndMs: 5000, Title: linkedTitle},
		{StartMs: 5000, EndMs: 8000, Title: "Episode"},
	}
	res, err := File(context.Background(), ff, plain)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Chapters, own) {
		t.Fatalf("a file with only its own chapters: %+v\nwant %+v", res.Chapters, own)
	}
	// The reader reads what ffmpeg wrote, and agrees with ffprobe on it.
	eds, err := matroskaEditionsOfFile(plain)
	if err != nil || len(eds) != 1 || len(eds[0].Atoms) != 3 || eds[0].Ordered {
		t.Fatalf("editions of the plain file: %+v, err %v", eds, err)
	}
	for i, a := range eds[0].Atoms {
		if a.Title != own[i].Title || a.StartNs/1e6 != own[i].StartMs || a.EndNs/1e6 != own[i].EndMs || a.Linked {
			t.Fatalf("chapter %d read as %+v, ffprobe says %+v", i, a, own[i])
		}
	}

	// The same bytes with two changes, each the size of what it replaces: the edition is
	// ordered, and the second chapter names another file's segment.
	b, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	flag := []byte{0x45, 0xDB, 0x81, 0x01} // EditionFlagDefault 1
	if bytes.Count(b, flag) != 1 {
		t.Fatalf("this ffmpeg writes the edition differently (%d default flags): the test needs another way to mark it ordered", bytes.Count(b, flag))
	}
	b = bytes.Replace(b, flag, []byte{0x45, 0xDD, 0x81, 0x01}, 1) // EditionFlagOrdered 1
	title := append([]byte{0x85, 0x80 | byte(len(linkedTitle))}, linkedTitle...)
	at := bytes.Index(b, title) - 2 // the ChapterDisplay holding the title: id 0x80, one size byte
	if at < 0 || b[at] != 0x80 || b[at+1]&0x80 == 0 {
		t.Fatal("this ffmpeg writes chapter titles differently: the test needs another way to link the chapter")
	}
	display := 2 + int(b[at+1]&0x7F)
	link := append([]byte{0x6E, 0x67, 0x90}, bytes.Repeat([]byte{0xEE}, 16)...) // ChapterSegmentUID
	if rest := display - len(link); rest < 2 {
		t.Fatalf("the chapter's title element (%d bytes) is too small to hold a segment UID", display)
	} else {
		link = append(link, 0xEC, 0x80|byte(rest-2)) // a Void over what is left
		link = append(link, make([]byte, rest-2)...)
	}
	copy(b[at:], link)
	linked := filepath.Join(base, "linked.mkv")
	if err := os.WriteFile(linked, b, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err = File(context.Background(), ff, linked)
	if err != nil {
		t.Fatal(err)
	}
	want := []Chapter{
		{StartMs: 0, EndMs: 2000, Title: "Cold open"},
		{StartMs: 2000, EndMs: 2000, Linked: true}, // a mark where the other file would play
		{StartMs: 5000, EndMs: 8000, Title: "Episode"},
	}
	if !reflect.DeepEqual(res.Chapters, want) {
		t.Fatalf("a file whose second chapter plays another file: %+v\nwant %+v", res.Chapters, want)
	}
}
