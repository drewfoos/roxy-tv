package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// fontFFmpeg stands in for ffmpeg dumping attachments: it notes the call, writes
// "font-<stream>" into each file a "-dump_attachment:<stream> <file>" pair names (but
// not for the streams in skip, the way ffmpeg writes nothing for a stream it has nothing
// for) and then runs tail (a sleep, an exit code).
func fontFFmpeg(t *testing.T, calls, skip, tail string) *ffmpeg.FFmpeg {
	t.Helper()
	script := filepath.Join(t.TempDir(), "ffmpeg")
	src := `#!/bin/sh
echo x >> '` + calls + `'
while [ $# -gt 0 ]; do
	case "$1" in -dump_attachment:*)
		n="${1#-dump_attachment:}"
		shift
		case " ` + skip + ` " in *" $n "*) ;; *) printf 'font-%s' "$n" > "$1"; sleep 0.05 ;; esac
	esac
	shift
done
` + tail + "\n"
	if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	return &ffmpeg.FFmpeg{FFmpeg: script}
}

func twoFonts() []model.Attachment {
	return []model.Attachment{{Index: 3, Filename: "A.ttf"}, {Index: 4, Filename: "B.otf"}}
}

func threeFonts() []model.Attachment {
	return append(twoFonts(), model.Attachment{Index: 5, Filename: "C.ttf"})
}

func fileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A file with three fonts of which ffmpeg produces two: that is not a success. The two
// are kept and filled in, and the error names the third (the caller used to be told all
// was well, and the styled subtitles were drawn in a fallback font with nothing logged).
func TestAttachmentNotExtractedIsAnError(t *testing.T) {
	data := t.TempDir()
	safefs.Init(data)
	calls := filepath.Join(t.TempDir(), "calls")
	dir := filepath.Join(data, "cache", "attachments", "src1")

	atts := threeFonts()
	err := ExtractAttachments(context.Background(), fontFFmpeg(t, calls, "4", "exit 0"), "/media/x.mkv", dir, atts)
	var missing *MissingAttachments
	if !errors.As(err, &missing) {
		t.Fatalf("an extraction that left a font out returned %v, want a *MissingAttachments", err)
	}
	if strings.Join(missing.Names, " ") != "B.otf" {
		t.Fatalf("missing = %q, want B.otf", missing.Names)
	}
	if atts[1].ExtractedPath != "" || atts[1].ExtractedAt != nil {
		t.Fatalf("the font that did not come out is recorded as extracted: %+v", atts[1])
	}
	for _, i := range []int{0, 2} {
		if atts[i].ExtractedPath != filepath.Join(dir, atts[i].Filename) || atts[i].ExtractedAt == nil {
			t.Fatalf("%s came out but is not filled in: %+v", atts[i].Filename, atts[i])
		}
	}
	if names := dirNames(t, dir); names != "A.ttf C.ttf" {
		t.Fatalf("the folder holds %q, want the two fonts that came out", names)
	}

	// ffmpeg failing as well, after writing some of them: the same, with what it said.
	atts = threeFonts()
	err = ExtractAttachments(context.Background(), fontFFmpeg(t, calls, "5", "echo 'stream 5: broken' >&2; exit 1"), "/media/x.mkv", dir, atts)
	if !errors.As(err, &missing) || strings.Join(missing.Names, " ") != "C.ttf" || missing.Err == nil {
		t.Fatalf("a failed run that wrote two of three returned %v", err)
	}
	if atts[0].ExtractedPath == "" || atts[1].ExtractedPath == "" || atts[2].ExtractedPath != "" {
		t.Fatalf("after a failed run that wrote two of three: %+v", atts)
	}

	// Nothing at all came out: a plain failure.
	atts = threeFonts()
	err = ExtractAttachments(context.Background(), fontFFmpeg(t, calls, "3 4 5", "exit 1"), "/media/x.mkv", t.TempDir(), atts)
	if err == nil || errors.As(err, &missing) {
		t.Fatalf("a run that wrote nothing returned %v, want a plain error", err)
	}
}

// A font ffmpeg does not produce is asked for again by every player that opens the
// subtitles. The first request finds that out; the later ones are answered from that,
// not by running ffmpeg over the whole video again each time. A changed video is tried
// afresh, and so is every ingest.
func TestAttachmentThatCannotBeExtractedIsTriedOnce(t *testing.T) {
	data := t.TempDir()
	safefs.Init(data)
	calls := filepath.Join(t.TempDir(), "calls")
	dir := filepath.Join(data, "cache", "attachments", "src1")
	video := filepath.Join(t.TempDir(), "x.mkv")
	if err := os.WriteFile(video, []byte("not really a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	ff := fontFFmpeg(t, calls, "4", "exit 0")

	var missing *MissingAttachments
	for i := 1; i <= 3; i++ {
		atts := threeFonts()
		err := EnsureAttachments(context.Background(), ff, video, dir, atts)
		if !errors.As(err, &missing) || strings.Join(missing.Names, " ") != "B.otf" {
			t.Fatalf("request %d: %v, want B.otf missing", i, err)
		}
		if atts[0].ExtractedPath == "" || atts[2].ExtractedPath == "" || atts[1].ExtractedPath != "" {
			t.Fatalf("request %d: %+v", i, atts)
		}
		if n := callCount(t, calls); n != 1 {
			t.Fatalf("ffmpeg ran %d times after %d requests, want 1", n, i)
		}
	}

	// Ingest does not go by what an earlier run found.
	if err := ExtractAttachments(context.Background(), ff, video, dir, threeFonts()); !errors.As(err, &missing) {
		t.Fatalf("ingest: %v", err)
	}
	if n := callCount(t, calls); n != 2 {
		t.Fatalf("ffmpeg ran %d times after an ingest, want 2", n)
	}

	// The video was replaced: what the old one lacked says nothing about this one.
	if err := os.WriteFile(video, []byte("another video altogether"), 0o644); err != nil {
		t.Fatal(err)
	}
	atts := threeFonts()
	if err := EnsureAttachments(context.Background(), fontFFmpeg(t, calls, "", "exit 0"), video, dir, atts); err != nil {
		t.Fatal(err)
	}
	if n := callCount(t, calls); n != 3 {
		t.Fatalf("ffmpeg ran %d times after the video changed, want 3", n)
	}
	if names := dirNames(t, dir); names != "A.ttf B.otf C.ttf" {
		t.Fatalf("the folder holds %q, want the three fonts", names)
	}

	// A run that failed outright is not remembered: the next request tries again.
	dir2 := filepath.Join(data, "cache", "attachments", "src2")
	for i := 1; i <= 2; i++ {
		if err := EnsureAttachments(context.Background(), fontFFmpeg(t, calls, "3 4 5", "exit 1"), video, dir2, threeFonts()); err == nil {
			t.Fatal("a failed run reported success")
		}
		if n := callCount(t, calls); n != 3+i {
			t.Fatalf("ffmpeg ran %d times after %d failed runs, want %d", n, i, 3+i)
		}
	}
}

// Two attachments may carry one name (two cuts of a font family, both "font.ttf"), and a
// name may come with a folder. Each is kept, under a name of its own inside the folder.
func TestAttachmentsWithOneNameAreAllKept(t *testing.T) {
	data := t.TempDir()
	safefs.Init(data)
	calls := filepath.Join(t.TempDir(), "calls")
	dir := filepath.Join(data, "cache", "attachments", "src1")
	fonts := func() []model.Attachment {
		return []model.Attachment{
			{Index: 3, Filename: "Font.ttf"}, {Index: 4, Filename: "Font.ttf"},
			{Index: 5, Filename: "../../fonts/Font.ttf"}, {Index: 6, Filename: ""}, {Index: 7, Filename: ".."},
		}
	}
	want := []string{"Font.ttf", "4-Font.ttf", "5-Font.ttf", "attachment", "7-attachment"}

	atts := fonts()
	if err := ExtractAttachments(context.Background(), fontFFmpeg(t, calls, "", "exit 0"), "/media/x.mkv", dir, atts); err != nil {
		t.Fatal(err)
	}
	for i, a := range atts {
		if a.ExtractedPath != filepath.Join(dir, want[i]) {
			t.Fatalf("stream %d is kept as %q, want %q", a.Index, a.ExtractedPath, want[i])
		}
		if got := fileText(t, a.ExtractedPath); got != "font-"+strconv.Itoa(a.Index) {
			t.Fatalf("%s holds %q, want stream %d's font", want[i], got, a.Index)
		}
	}
	if names := dirNames(t, dir); names != "4-Font.ttf 5-Font.ttf 7-attachment Font.ttf attachment" {
		t.Fatalf("the folder holds %q", names)
	}

	// A request finds them all there under the same names, and runs nothing.
	atts = fonts()
	if err := EnsureAttachments(context.Background(), fontFFmpeg(t, calls, "", "exit 0"), "/media/x.mkv", dir, atts); err != nil {
		t.Fatal(err)
	}
	for i, a := range atts {
		if a.ExtractedPath != filepath.Join(dir, want[i]) {
			t.Fatalf("found again: stream %d is %q, want %q", a.Index, a.ExtractedPath, want[i])
		}
	}
	if n := callCount(t, calls); n != 1 {
		t.Fatalf("ffmpeg ran %d times, want 1", n)
	}
}

// A subtitle track's fonts are all asked for at once: one ffmpeg run serves them all, and
// the folder holds the fonts and nothing else afterwards.
func TestEnsureAttachmentsRunsOnce(t *testing.T) {
	data := t.TempDir()
	safefs.Init(data)
	calls := filepath.Join(t.TempDir(), "calls")
	dir := filepath.Join(data, "cache", "attachments", "src1")
	ff := fontFFmpeg(t, calls, "", "exit 0")

	var wg sync.WaitGroup
	got := make([][]model.Attachment, 8)
	errs := make([]error, len(got))
	for i := range got {
		got[i] = twoFonts()
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = EnsureAttachments(context.Background(), ff, "/media/x.mkv", dir, got[i])
		}(i)
	}
	wg.Wait()
	for i := range got {
		if errs[i] != nil {
			t.Fatalf("call %d: %v", i, errs[i])
		}
		for _, a := range got[i] {
			if a.ExtractedPath != filepath.Join(dir, a.Filename) || a.ExtractedAt == nil {
				t.Fatalf("call %d: %s not filled in: %+v", i, a.Filename, a)
			}
		}
	}
	if n := callCount(t, calls); n != 1 {
		t.Fatalf("ffmpeg ran %d times, want 1", n)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "B.otf")); string(b) != "font-4" {
		t.Fatalf("B.otf = %q", b)
	}
	if names := dirNames(t, dir); names != "A.ttf B.otf" {
		t.Fatalf("the folder holds %q, want only the two fonts", names)
	}

	// Ingest always extracts again (the file may have changed); it leaves no litter either.
	again := twoFonts()
	if err := ExtractAttachments(context.Background(), ff, "/media/x.mkv", dir, again); err != nil {
		t.Fatal(err)
	}
	if n := callCount(t, calls); n != 2 {
		t.Fatalf("ffmpeg ran %d times after a second ingest, want 2", n)
	}
	if names := dirNames(t, dir); names != "A.ttf B.otf" {
		t.Fatalf("the folder holds %q after a second ingest", names)
	}
}

// An extraction stopped half way (the request ended) leaves no font behind: a later
// request would have taken the half-written file for the font.
func TestStoppedAttachmentExtractionKeepsNothing(t *testing.T) {
	data := t.TempDir()
	safefs.Init(data)
	calls := filepath.Join(t.TempDir(), "calls")
	dir := filepath.Join(data, "cache", "attachments", "src1")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	atts := twoFonts()
	if err := EnsureAttachments(ctx, fontFFmpeg(t, calls, "", "exec sleep 30"), "/media/x.mkv", dir, atts); err == nil {
		t.Fatal("a stopped extraction reported success")
	}
	if names := dirNames(t, dir); names != "" {
		t.Fatalf("a stopped extraction left %q", names)
	}
	for _, a := range atts {
		if a.ExtractedPath != "" {
			t.Fatalf("%s is recorded as extracted", a.Filename)
		}
	}

	atts = twoFonts()
	if err := EnsureAttachments(context.Background(), fontFFmpeg(t, calls, "", "exit 0"), "/media/x.mkv", dir, atts); err != nil {
		t.Fatal(err)
	}
	if names := dirNames(t, dir); names != "A.ttf B.otf" {
		t.Fatalf("the folder holds %q, want the two fonts", names)
	}
}

func dirNames(t *testing.T, dir string) string {
	t.Helper()
	return strings.Join(dirEntries(t, dir), " ")
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
