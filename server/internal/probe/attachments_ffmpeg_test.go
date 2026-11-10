package probe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// The real ffmpeg, where there is one (CI's integration job requires it).
func needFFmpeg(t *testing.T) *ffmpeg.FFmpeg {
	t.Helper()
	ff, err := ffmpeg.New("", "")
	if err == nil {
		return ff
	}
	if os.Getenv("ROXY_REQUIRE_INTEGRATION") != "" {
		t.Fatalf("ROXY_REQUIRE_INTEGRATION is set but ffmpeg is not usable: %v", err)
	}
	t.Skip("needs ffmpeg")
	return nil
}

// A Matroska file whose fonts ffmpeg does not write when asked for them by name: one
// whose name has folders in it (ffmpeg calls it unsafe, skips it and still exits 0) and
// two that share a name (the second was written over the first). All three must come
// out, each with its own bytes, and nothing may be written outside the cache folder.
func TestAttachmentsExtractedByRealFFmpeg(t *testing.T) {
	ff := needFFmpeg(t)
	base := t.TempDir()
	data := filepath.Join(base, "config")
	safefs.Init(data)
	fonts := []struct{ file, name, bytes string }{
		{"one.ttf", "../../Escaped.ttf", "the first font"},
		{"two.ttf", "Family.ttf", "the regular cut"},
		{"three.ttf", "Family.ttf", "the bold cut, a little longer"},
	}
	video := filepath.Join(base, "x.mkv")
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=duration=1:size=320x240:rate=5"}
	meta := []string{}
	for i, f := range fonts {
		if err := os.WriteFile(filepath.Join(base, f.file), []byte(f.bytes), 0o644); err != nil {
			t.Fatal(err)
		}
		n := string(rune('0' + i))
		args = append(args, "-attach", filepath.Join(base, f.file))
		meta = append(meta, "-metadata:s:t:"+n, "filename="+f.name, "-metadata:s:t:"+n, "mimetype=font/ttf")
	}
	args = append(append(args, meta...), "-c:v", "mpeg4", video)
	if out, err := exec.Command(ff.FFmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("making the video: %v\n%s", err, out)
	}

	res, err := File(context.Background(), ff, video)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Attachments) != len(fonts) {
		t.Fatalf("probe found %d attachments, want %d", len(res.Attachments), len(fonts))
	}
	for i, a := range res.Attachments {
		if a.Filename != fonts[i].name {
			t.Fatalf("attachment %d is named %q, want %q", i, a.Filename, fonts[i].name)
		}
	}

	dir := filepath.Join(data, "cache", "attachments", "src1")
	atts := res.Attachments
	if err := ExtractAttachments(context.Background(), ff, video, dir, atts); err != nil {
		t.Fatalf("extraction: %v", err)
	}
	seen := map[string]bool{}
	for i, a := range atts {
		if a.ExtractedPath == "" || filepath.Dir(a.ExtractedPath) != dir || seen[a.ExtractedPath] {
			t.Fatalf("%q is kept at %q, want a file of its own in %s", a.Filename, a.ExtractedPath, dir)
		}
		seen[a.ExtractedPath] = true
		if got := fileText(t, a.ExtractedPath); got != fonts[i].bytes {
			t.Fatalf("%q (stream %d) holds %q, want %q", a.Filename, a.Index, got, fonts[i].bytes)
		}
	}
	if len(dirEntries(t, dir)) != len(fonts) {
		t.Fatalf("the folder holds %q, want the three fonts and nothing else", dirNames(t, dir))
	}
	for _, outside := range []string{filepath.Join(dir, "..", "..", "Escaped.ttf"), filepath.Join(base, "Escaped.ttf")} {
		if _, err := os.Stat(outside); err == nil {
			t.Fatalf("a font was written outside the cache folder: %s", outside)
		}
	}

	// A later request finds all three and runs nothing.
	again, err := File(context.Background(), ff, video)
	if err != nil {
		t.Fatal(err)
	}
	gone := &ffmpeg.FFmpeg{FFmpeg: filepath.Join(base, "no-ffmpeg-here")}
	if err := EnsureAttachments(context.Background(), gone, video, dir, again.Attachments); err != nil {
		t.Fatalf("a later request: %v", err)
	}
	for i, a := range again.Attachments {
		if a.ExtractedPath != atts[i].ExtractedPath {
			t.Fatalf("a later request finds %q at %q, want %q", a.Filename, a.ExtractedPath, atts[i].ExtractedPath)
		}
	}
}
