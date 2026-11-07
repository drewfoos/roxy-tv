package library

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/probe"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// A .mks sidecar holding two subtitle tracks (an ASS and an SRT) becomes two tracks with
// their own languages, numbered by file and inner track, each served from the cache as it
// is; and a SAMI sidecar is served converted to SubRip. Needs ffmpeg and ffprobe on PATH.
func TestMKSSidecarTracks(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		skipUnlessRequired(t, "ffmpeg/ffprobe not installed")
	}
	ff, err := ffmpeg.New("", "")
	if err != nil {
		skipUnlessRequired(t, err.Error())
	}
	root := t.TempDir()
	data := t.TempDir()
	safefs.Init(data)
	dir := filepath.Join(root, "Show")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ass := "[Script Info]\nScriptType: v4.00+\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize\nStyle: Default,Arial,40\n\n[Events]\nFormat: Layer, Start, End, Style, Text\nDialogue: 0,0:00:01.00,0:00:02.00,Default,Hello from ASS\n"
	sami := "<SAMI><BODY>\n<SYNC Start=1000><P Class=ENCC>Hola desde SAMI\n</BODY></SAMI>\n"
	if err := os.WriteFile(filepath.Join(t.TempDir(), "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "a.ass"), []byte(ass), 0o644)
	os.WriteFile(filepath.Join(src, "b.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHola desde SRT\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "Show - S01E01.es.smi"), []byte(sami), 0o644)
	mks := filepath.Join(dir, "Show - S01E01.mks")
	out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-i", filepath.Join(src, "a.ass"), "-i", filepath.Join(src, "b.srt"),
		"-map", "0", "-map", "1", "-c:s:0", "copy", "-c:s:1", "copy",
		"-metadata:s:s:0", "language=jpn", "-metadata:s:s:0", "title=Signs", "-metadata:s:s:1", "language=spa", "-f", "matroska", mks).CombinedOutput()
	if err != nil {
		skipUnlessRequired(t, fmt.Sprintf("could not build a .mks with this ffmpeg: %v %s", err, out))
	}
	if err := os.WriteFile(filepath.Join(dir, "Show - S01E01.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := &Library{ff: ff, cfg: &config.Config{DataDir: data}}
	found, err := sidecarSubtitles(filepath.Join(dir, "Show - S01E01.mkv"), root, 5)
	if err != nil {
		t.Fatal(err)
	}
	tracks, err := l.expandSubtitleContainers(context.Background(), found, nil)
	if err != nil {
		t.Fatal(err)
	}
	var smi model.MediaStream
	var inner []model.MediaStream
	for _, st := range tracks {
		if st.ExternalIndex == nil {
			smi = st
		} else {
			inner = append(inner, st)
		}
	}
	if smi.Codec != "sami" || smi.Language != "spa" {
		t.Fatalf("SAMI sidecar: %+v", smi)
	}
	cache := probe.SidecarCachePath(l.cfg.SubtitlesDir(), "src", smi)
	if err := probe.EnsureTextSubtitle(context.Background(), ff, smi.ExternalPath, 0, smi.Codec, cache); err != nil {
		t.Fatalf("SAMI: %v", err)
	}
	if b, _ := os.ReadFile(cache); !strings.Contains(string(b), "Hola desde SAMI") || !strings.Contains(string(b), "-->") || !strings.HasSuffix(cache, ".srt") {
		t.Fatalf("SAMI served as %s: %q", cache, b)
	}
	tracks = inner
	if len(tracks) != 2 {
		t.Fatalf("tracks: %+v", tracks)
	}
	langs := map[string]model.MediaStream{}
	for _, st := range tracks {
		langs[st.Language] = st
	}
	if a, ok := langs["jpn"]; !ok || a.Codec != "ass" || a.Title != "Signs" || a.ExternalIndex == nil {
		t.Fatalf("ASS track: %+v", a)
	}
	b, ok := langs["spa"]
	if !ok || b.Codec != "subrip" || b.ExternalIndex == nil || b.Index == langs["jpn"].Index || sidecarKey(b) == sidecarKey(langs["jpn"]) {
		t.Fatalf("SRT track: %+v", b)
	}
	// Served from the cache: ASS as is, SAMI as SubRip.
	for _, st := range tracks {
		cache := probe.SidecarCachePath(l.cfg.SubtitlesDir(), "src", st)
		if err := probe.EnsureTextSubtitle(context.Background(), ff, st.ExternalPath, *st.ExternalIndex, st.Codec, cache); err != nil {
			t.Fatalf("%s: %v", st.Codec, err)
		}
		b, _ := os.ReadFile(cache)
		switch st.Codec {
		case "ass":
			if !strings.Contains(string(b), "Hello from ASS") || !strings.HasSuffix(cache, ".ass") {
				t.Errorf("ASS served as %s: %q", cache, b)
			}
		case "subrip":
			if !strings.Contains(string(b), "Hola desde SRT") || !strings.HasSuffix(cache, ".srt") {
				t.Errorf("SRT served as %s: %q", cache, b)
			}
		}
	}
}
