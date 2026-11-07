package library

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

type fakeStream struct {
	Type, Codec, Lang string
}

// writeProbe writes the fake ffprobe answer for path.
func writeProbe(t *testing.T, path string, streams ...fakeStream) {
	t.Helper()
	var ss []map[string]any
	for i, s := range streams {
		m := map[string]any{"index": i, "codec_type": s.Type, "codec_name": s.Codec, "tags": map[string]string{}}
		if s.Lang != "" {
			m["tags"] = map[string]string{"language": s.Lang}
		}
		ss = append(ss, m)
	}
	b, _ := json.Marshal(map[string]any{"format": map[string]string{"format_name": "matroska,webm", "duration": "10"}, "streams": ss})
	if err := os.WriteFile(path+".json", b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeProbeLib(t *testing.T) (*Library, *store.Store, model.Library, string, *config.Config) {
	t.Helper()
	// A fake ffprobe (prints "<input>.json") and ffmpeg (copies "<input>.s<N>" out as
	// stream N), so the whole ingest runs without real media.
	bin := t.TempDir()
	for name, script := range map[string]string{
		"ffprobe": `#!/bin/sh
while [ $# -gt 0 ]; do if [ "$1" = "-i" ]; then in="$2"; fi; shift; done
[ -f "$in.json" ] || { echo "Invalid data found" >&2; exit 1; }
cat "$in.json"
`,
		"ffmpeg": `#!/bin/sh
if [ "$1" = "-version" ]; then echo "ffmpeg version fake"; exit 0; fi
map=""; for a in "$@"; do last="$a"; done
while [ $# -gt 0 ]; do case "$1" in -i) in="$2";; -map) map="$2";; esac; shift; done
n=${map#0:}
[ -f "$in.s$n" ] || { echo "no stream" >&2; exit 1; }
cp "$in.s$n" "$last"
`,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ff, err := ffmpeg.New(filepath.Join(bin, "ffmpeg"), filepath.Join(bin, "ffprobe"))
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	safefs.Init(data)
	st, err := store.Open(filepath.Join(data, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	root := t.TempDir()
	safefs.SetMediaRoots([]string{root})
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: root}
	if err := st.CreateLibrary(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DataDir: data}
	// A second film so removing one never looks like an emptied library.
	other := filepath.Join(root, "Other (2001)", "Other (2001).mkv")
	os.MkdirAll(filepath.Dir(other), 0o755)
	os.WriteFile(other, []byte("other"), 0o644)
	writeProbe(t, other, fakeStream{"video", "h264", ""})
	return New(st, ff, cfg), st, *lib, root, cfg
}

func sourceFor(t *testing.T, st *store.Store, path string) *model.MediaSource {
	t.Helper()
	ctx := context.Background()
	part, err := st.GetPartByPath(ctx, path)
	if err != nil || part == nil {
		t.Fatalf("part for %s: %v %v", path, part, err)
	}
	src, err := st.GetSource(ctx, part.SourceID)
	if err != nil || src == nil {
		t.Fatalf("source: %v %v", src, err)
	}
	return src
}

// Found by an audit: A file deleted (record removed) and a different file later put at the same path is
// served the OLD file's extracted subtitle: caches are keyed by the path-derived source id
// and only dropped when a still-recorded part changed.
func TestStaleSubtitleCacheAfterDeleteAndReadd(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, cfg := fakeProbeLib(t)
	film := filepath.Join(root, "Film (2020)", "Film (2020).mkv")
	os.MkdirAll(filepath.Dir(film), 0o755)
	os.WriteFile(film, []byte("old file"), 0o644)
	writeProbe(t, film, fakeStream{"video", "h264", ""}, fakeStream{"subtitle", "subrip", "eng"})
	os.WriteFile(film+".s1", []byte("1\n00:00:01,000 --> 00:00:02,000\nOLD FILE'S SUBTITLE\n"), 0o644)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	srcID := sourceFor(t, st, film).ID
	cache := filepath.Join(cfg.SubtitlesDir(), srcID, "1.srt") // what getSubtitle serves
	// Gone (Sonarr deletes the old file for an upgrade), a scan removes the record...
	os.Remove(film)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.GetPartByPath(ctx, film); p != nil {
		t.Fatal("record not removed")
	}
	// ...and the new file arrives under the same name.
	os.WriteFile(film, []byte("the new, different file"), 0o644)
	os.WriteFile(film+".s1", []byte("1\n00:00:01,000 --> 00:00:02,000\nNEW FILE'S SUBTITLE\n"), 0o644)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "NEW") {
		t.Fatalf("new file at %s is served the old file's subtitle: %q", filepath.Base(film), b)
	}
}

// Found by an audit: A .mks that cannot be read (or holds only image tracks) is left out of the stored
// streams, but sidecarsChanged lists it as a sidecar, so every scan re-ingests the video.
func TestUnreadableMKSDoesNotReingestEveryScan(t *testing.T) {
	ctx := context.Background()
	l, _, lib, root, _ := fakeProbeLib(t)
	film := filepath.Join(root, "Film (2020)", "Film (2020).mkv")
	os.MkdirAll(filepath.Dir(film), 0o755)
	os.WriteFile(film, []byte("video"), 0o644)
	writeProbe(t, film, fakeStream{"video", "h264", ""})
	os.WriteFile(filepath.Join(filepath.Dir(film), "Film (2020).en.mks"), []byte("garbage"), 0o644)
	var n atomic.Int32
	l.ingestOne = func(ctx context.Context, lib model.Library, path string) error {
		if path == film {
			n.Add(1)
		}
		return l.addFileSafe(ctx, lib, path)
	}
	for i := 0; i < 4; i++ {
		if err := l.Scan(ctx, lib); err != nil {
			t.Fatal(err)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("film ingested %d times in 4 scans of an unchanged folder", n.Load())
	}
}

// Found by an audit: Sidecar numbering starts at len(streams)+len(attachments)+1, but stream numbers are
// ffprobe's indexes and streams Roxy skips (data, cover art) still use them: the first
// sidecar can get an embedded stream's number and the source cannot be stored.
func TestSidecarNumberNeverCollidesWithEmbedded(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, _ := fakeProbeLib(t)
	film := filepath.Join(root, "Film (2020)", "Film (2020).mp4")
	os.MkdirAll(filepath.Dir(film), 0o755)
	os.WriteFile(film, []byte("video"), 0o644)
	// e.g. a QuickTime/TS file: video, two data tracks (timecode, chapters), audio, subtitle.
	writeProbe(t, film, fakeStream{"video", "h264", ""}, fakeStream{"data", "bin_data", ""}, fakeStream{"data", "bin_data", ""},
		fakeStream{"audio", "aac", "eng"}, fakeStream{"subtitle", "mov_text", "spa"})
	os.WriteFile(filepath.Join(filepath.Dir(film), "Film (2020).en.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatalf("scan: %v", err)
	}
	src := sourceFor(t, st, film)
	seen := map[int]bool{}
	for _, s := range src.Streams {
		if seen[s.Index] {
			t.Fatalf("duplicate stream number %d", s.Index)
		}
		seen[s.Index] = true
	}
}

// Found by an audit: A .mks keyed by "path#trackIndex": remux it without its first track and the saved
// stream number now plays a different language.
func TestMKSTrackRemovedKeepsChoice(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, _ := fakeProbeLib(t)
	film := filepath.Join(root, "Film (2020)", "Film (2020).mkv")
	os.MkdirAll(filepath.Dir(film), 0o755)
	os.WriteFile(film, []byte("video"), 0o644)
	writeProbe(t, film, fakeStream{"video", "h264", ""})
	mks := filepath.Join(filepath.Dir(film), "Film (2020).mks")
	os.WriteFile(mks, []byte("mks v1"), 0o644)
	writeProbe(t, mks, fakeStream{"subtitle", "ass", "eng"}, fakeStream{"subtitle", "subrip", "spa"})
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	chosen := -1
	for _, s := range sourceFor(t, st, film).Streams {
		if s.IsExternal && s.Language == "eng" {
			chosen = s.Index // the profile saves this number
		}
	}
	// English track dropped from the .mks; the Spanish one is now track 0.
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(mks, []byte("mks v2 smaller?"), 0o644)
	writeProbe(t, mks, fakeStream{"subtitle", "subrip", "spa"})
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	for _, s := range sourceFor(t, st, film).Streams {
		if s.Index == chosen && s.Language != "eng" {
			t.Fatalf("saved choice %d (English) now plays %q codec %s", chosen, s.Language, s.Codec)
		}
	}
}

// Found by an audit: The video re-encoded with more streams: a sidecar numbered by an older release (just
// after the embedded streams) has its number taken by a new embedded track. It used to be
// renumbered while the saved choice kept the old number, which now named the embedded
// Spanish track; the choice now follows the sidecar to its new number.
func TestVideoReencodeKeepsSidecarChoice(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, _ := fakeProbeLib(t)
	film := filepath.Join(root, "Film (2020)", "Film (2020).mkv")
	os.MkdirAll(filepath.Dir(film), 0o755)
	os.WriteFile(film, []byte("video"), 0o644)
	writeProbe(t, film, fakeStream{"video", "h264", ""}, fakeStream{"audio", "aac", "jpn"})
	srt := filepath.Join(filepath.Dir(film), "Film (2020).en.srt")
	os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	src := sourceFor(t, st, film)
	// As an older release numbered it: 2, right after the two embedded streams.
	if _, err := st.DB().ExecContext(ctx, `UPDATE media_streams SET stream_index = 2 WHERE source_id = ? AND is_external = 1`, src.ID); err != nil {
		t.Fatal(err)
	}
	it, err := st.ItemForPath(ctx, film)
	if err != nil || it == nil {
		t.Fatal(it, err)
	}
	u := &model.User{ID: "u1", Name: "drew"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	scope, err := st.DefaultProfileID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	two := 2
	if err := st.SetProgress(ctx, scope, it.GUID, 60_000, nil, &two); err != nil {
		t.Fatal(err)
	}
	// Upgraded release: two audio and two embedded subtitle tracks (numbers 0-4).
	os.WriteFile(film, []byte("bigger video file"), 0o644)
	writeProbe(t, film, fakeStream{"video", "h264", ""}, fakeStream{"audio", "aac", "jpn"}, fakeStream{"audio", "aac", "eng"},
		fakeStream{"subtitle", "ass", "spa"}, fakeStream{"subtitle", "ass", "fre"})
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetUserItemData(ctx, scope, it.GUID)
	if err != nil || d == nil || d.SubtitleStreamIndex == nil {
		t.Fatalf("saved choice: %+v %v", d, err)
	}
	for _, s := range sourceFor(t, st, film).Streams {
		if s.Index == *d.SubtitleStreamIndex {
			if !s.IsExternal || s.ExternalPath != srt {
				t.Fatalf("saved choice %d now names %s %s (external %v), not the English .srt", s.Index, s.Language, s.Type, s.IsExternal)
			}
			return
		}
	}
	t.Fatalf("saved choice %d names no stream", *d.SubtitleStreamIndex)
}

// Found by an audit: A case difference between video and sidecar names where lower-casing changes the
// byte length: the name is sliced at the wrong place (here: a panic, so the video is never
// added).
func TestSidecarNameUnicodeCase(t *testing.T) {
	root := t.TempDir()
	safefs.SetMediaRoots([]string{root})
	video := filepath.Join(root, "KKKKK.mkv") // KELVIN SIGN, lower-cases to "k"
	os.WriteFile(video, []byte("v"), 0o644)
	os.WriteFile(filepath.Join(root, "kkkkk.srt"), []byte("1\n"), 0o644)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	if _, err := sidecarSubtitles(video, root, 1); err != nil {
		t.Fatal(err)
	}
}

// A .sub alone is a text subtitle (MicroDVD); with an .idx beside it, a VobSub's pictures.
// Found by an audit: a lone .sub used to be offered as an image track that could not be
// shown.
func TestLoneSubIsText(t *testing.T) {
	root := t.TempDir()
	video := filepath.Join(root, "Film.mkv")
	os.WriteFile(video, []byte("v"), 0o644)
	os.WriteFile(filepath.Join(root, "Film.en.sub"), []byte("{1}{25}Hello\n"), 0o644)
	got, err := sidecarSubtitles(video, root, sidecarBase)
	if err != nil || len(got) != 1 || got[0].Codec != "microdvd" || !got[0].IsTextFormat {
		t.Fatalf("lone .sub: %+v %v", got, err)
	}
	os.WriteFile(filepath.Join(root, "Film.en.idx"), []byte("id: en, index: 0\n"), 0o644)
	got, err = sidecarSubtitles(video, root, sidecarBase)
	if err != nil || len(got) != 1 || got[0].Codec != "dvd_subtitle" {
		t.Fatalf(".sub with .idx: %+v %v", got, err)
	}
}
