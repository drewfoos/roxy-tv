package music

import (
	"bytes"
	"context"
	"image"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/store"
)

// needFFmpeg skips when the machine has no ffmpeg, and fails instead when
// ROXY_REQUIRE_INTEGRATION is set (CI's integration job), so a green job means it ran.
func needFFmpeg(t *testing.T) *ffmpeg.FFmpeg {
	t.Helper()
	ff, err := ffmpeg.New(os.Getenv("ROXY_FFMPEG_PATH"), os.Getenv("ROXY_FFPROBE_PATH"))
	if err == nil {
		return ff
	}
	if os.Getenv("ROXY_REQUIRE_INTEGRATION") != "" {
		t.Fatalf("ROXY_REQUIRE_INTEGRATION is set but ffmpeg is not usable: %v", err)
	}
	t.Skipf("ffmpeg not available: %v", err)
	return nil
}

// makeAudio has ffmpeg make a two-second tone with tags (and, with cover set, a picture
// in the file).
func makeAudio(t *testing.T, ff *ffmpeg.FFmpeg, path string, cover string, args ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=2"}
	if cover != "" {
		cmd = append(cmd, "-i", cover, "-map", "0:a", "-map", "1:v", "-c:v", "copy", "-disposition:v", "attached_pic")
	}
	cmd = append(cmd, args...)
	cmd = append(cmd, path)
	if out, err := exec.Command(ff.FFmpeg, cmd...).CombinedOutput(); err != nil {
		t.Fatalf("making %s: %v\n%s", filepath.Base(path), err, out)
	}
}

// With the real ffprobe and ffmpeg: files made here are read back as the songs they are
// (tags in each format's own spelling, how they are encoded, the picture inside), and a
// whole scan of them makes the library it should.
func TestRealFilesAreRead(t *testing.T) {
	ff := needFFmpeg(t)
	l := newTestLib(t)
	l.svc.ff = ff
	cover := filepath.Join(filepath.Dir(l.root), "cover.png")
	if err := os.WriteFile(cover, testPicture(t, 90), 0o644); err != nil {
		t.Fatal(err)
	}
	mp3 := filepath.Join(l.root, "Joost", "Kleinkunst", "01 - Intro.mp3")
	makeAudio(t, ff, mp3, cover, "-c:a", "libmp3lame", "-b:a", "128k", "-id3v2_version", "3",
		"-metadata", "title=Intro", "-metadata", "artist=Joost, Ski Aggu", "-metadata", "album=Kleinkunst", "-metadata", "album_artist=Joost",
		"-metadata", "track=1/12", "-metadata", "disc=1", "-metadata", "date=2022-09-23", "-metadata", "genre=Hip Hop; Pop")
	flac := filepath.Join(l.root, "Joost", "Kleinkunst", "02 - Wachtmuziek.flac")
	makeAudio(t, ff, flac, cover, "-c:a", "flac", "-sample_fmt", "s32", "-bits_per_raw_sample", "24", "-ar", "96000",
		"-metadata", "TITLE=Wachtmuziek", "-metadata", "ARTIST=Joost", "-metadata", "ALBUM=Kleinkunst", "-metadata", "ALBUMARTIST=Joost",
		"-metadata", "TRACKNUMBER=2", "-metadata", "DATE=2022", "-metadata", "LYRICS=Eerste regel", "-metadata", "REPLAYGAIN_TRACK_GAIN=-6.50 dB")
	opus := filepath.Join(l.root, "Loose", "song.opus")
	makeAudio(t, ff, opus, "", "-c:a", "libopus", "-b:a", "64k", "-metadata", "title=Loose Song", "-metadata", "artist=Someone")

	ctx := context.Background()
	info, err := ReadFile(ctx, ff, mp3)
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Intro" || info.Artist != "Joost, Ski Aggu" || info.Album != "Kleinkunst" || info.AlbumArtist != "Joost" || info.Track != 1 || info.Disc != 1 || info.Year != 2022 {
		t.Errorf("MP3 tags: %+v", info)
	}
	if info.Codec != "mp3" || info.Container != "mp3" || info.DurationMs < 1900 || info.DurationMs > 2300 || info.Channels == 0 || info.SampleRate == 0 || info.PictureStream < 0 {
		t.Errorf("MP3 encoding: %+v", info)
	}
	data, err := EmbeddedPicture(ctx, ff, mp3, info.PictureStream)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || format != "png" || cfg.Width != 8 {
		t.Errorf("the picture copied out of the MP3: %v %s %+v", err, format, cfg)
	}

	info, err = ReadFile(ctx, ff, flac)
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Wachtmuziek" || info.AlbumArtist != "Joost" || info.Track != 2 || info.Lyrics != "Eerste regel" || info.GainTrack == nil || *info.GainTrack != -6.5 {
		t.Errorf("FLAC tags: %+v", info)
	}
	if info.Codec != "flac" || info.BitDepth != 24 || info.SampleRate != 96000 || info.PictureStream < 0 {
		t.Errorf("FLAC encoding: %d-bit %d Hz, picture stream %d", info.BitDepth, info.SampleRate, info.PictureStream)
	}

	info, err = ReadFile(ctx, ff, opus)
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Loose Song" || info.Artist != "Someone" || info.Codec != "opus" || info.PictureStream != -1 {
		t.Errorf("Opus (tags on the audio stream): %+v", info)
	}

	// Loudness: a full-scale-ish sine has a loudness and a peak worth the name.
	loudness, err := MeasureFile(ctx, ff, flac)
	if err != nil {
		t.Fatal(err)
	}
	if loudness.LUFS > 0 || loudness.LUFS < -40 || loudness.Peak > 1 || loudness.Peak < -40 {
		t.Errorf("the tone's loudness: %+v", loudness)
	}
	// Conversion: each format comes out as what it says, readable, about as long.
	tc := NewTranscoder(ff, filepath.Join(filepath.Dir(l.root), "data", "cache", "music", "converted"), 1<<30)
	for _, f := range []AudioFormat{{"opus", 128}, {"mp3", 192}, {"aac", 192}, {Codec: "flac"}} {
		out, err := tc.File(ctx, "", Source{TrackID: "t", Path: flac, Size: 1, Mtime: 1, SampleRate: 96000, Channels: 2}, f)
		if err != nil {
			t.Errorf("converting to %s: %v", f.Codec, err)
			continue
		}
		got, err := ReadFile(ctx, ff, out)
		if err != nil {
			t.Errorf("reading the %s back: %v", f.Codec, err)
			continue
		}
		if got.Codec != f.Codec || got.DurationMs < 1800 || got.DurationMs > 2400 {
			t.Errorf("%s came out as %s, %d ms", f.Codec, got.Codec, got.DurationMs)
		}
		if f.Codec == "flac" && (got.SampleRate != 48000 || got.BitDepth != 16) {
			t.Errorf("the lossless conversion of a 24-bit/96 kHz file: %d-bit %d Hz", got.BitDepth, got.SampleRate)
		}
	}

	// A file that is not audio at all, under an audio name.
	junk := filepath.Join(l.root, "Loose", "notes.mp3")
	if err := os.WriteFile(junk, []byte("these are not the bytes of a song"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := l.scan(false)
	if res.Files != 4 || res.Added != 3 || res.Failed != 1 {
		t.Fatalf("scan: %+v", res)
	}
	intro, wacht := l.track("Intro"), l.track("Wachtmuziek")
	if intro.Art == "" || intro.Art != wacht.Art {
		t.Errorf("the same picture in two files is one picture: %q %q", intro.Art, wacht.Art)
	}
	if len(intro.Artists) != 2 || intro.Artists[1].Name != "Ski Aggu" || intro.AlbumID == "" || intro.AlbumID != wacht.AlbumID {
		t.Errorf("credits and album: %+v / %+v", intro, wacht)
	}
	if !wacht.Lossless || wacht.BitDepth != 24 || wacht.Lyrics != "embedded" || wacht.GainTrack == nil {
		t.Errorf("the FLAC as a track: %+v", wacht)
	}
	albums, _, _ := l.st.ListMusicAlbums(ctx, l.scope, store.MusicAlbumQuery{})
	if len(albums) != 1 || albums[0].TrackCount != 2 || albums[0].Art != intro.Art {
		t.Errorf("albums: %+v", albums)
	}
}

// With the real ffmpeg: the blocks kept for a song give the loudness ffmpeg itself
// reports for it, and an album's loudness worked out from its songs' blocks is the one
// ffmpeg measures when the songs are fed to it as one file. A quiet song between two
// loud ones is left out of the album by the standard's gate; a mean of the songs' own
// figures over their lengths (what this did before) puts the album well under that.
func TestRealAlbumLoudnessIsThatOfTheAlbumAsOneFile(t *testing.T) {
	ff := needFFmpeg(t)
	ctx := context.Background()
	dir := t.TempDir()
	noise := func(name string, amplitude string, seed string) string {
		p := filepath.Join(dir, name)
		cmd := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "anoisesrc=color=pink:duration=12:sample_rate=48000:amplitude=" + amplitude + ":seed=" + seed, "-ac", "2", p}
		if out, err := exec.Command(ff.FFmpeg, cmd...).CombinedOutput(); err != nil {
			t.Fatalf("making %s: %v\n%s", name, err, out)
		}
		return p
	}
	files := []string{noise("1-loud.flac", "0.5", "1"), noise("2-interlude.flac", "0.02", "2"), noise("3-loud.flac", "0.5", "3")}

	var album Blocks
	var own []float64
	peak := -1000.0
	for _, f := range files {
		l, err := MeasureFile(ctx, ff, f)
		if err != nil {
			t.Fatal(err)
		}
		// Twelve seconds: a block every 100 ms once the first 400 ms are in.
		if n := l.Blocks.Total(); n < 110 || n > 120 {
			t.Errorf("%s: %d blocks", filepath.Base(f), n)
		}
		fromBlocks, ok := l.Blocks.Integrated()
		if !ok || fromBlocks < l.LUFS-0.15 || fromBlocks > l.LUFS+0.15 {
			t.Errorf("%s: ffmpeg says %.1f LUFS, its blocks give %.1f", filepath.Base(f), l.LUFS, fromBlocks)
		}
		back, err := DecodeBlocks(l.Blocks.Encode())
		if err != nil || back.Total() != l.Blocks.Total() {
			t.Errorf("%s: blocks packed and read back: %v", filepath.Base(f), err)
		}
		album.Merge(l.Blocks)
		own = append(own, l.LUFS)
		if l.Peak > peak {
			peak = l.Peak
		}
	}
	if own[1] > own[0]-20 {
		t.Fatalf("the interlude is not quiet enough for this test: %v", own)
	}

	// The album as ffmpeg measures it: the three files joined into one.
	var whole loudnessReader
	err := ff.RunLines(ctx, whole.line, "-hide_banner", "-nostats", "-v", "info", "-i", files[0], "-i", files[1], "-i", files[2],
		"-filter_complex", "[0:a][1:a][2:a]concat=n=3:v=0:a=1,ebur128=peak=true:framelog=info", "-f", "null", "-")
	if err != nil {
		t.Fatal(err)
	}
	truth, err := whole.result()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := album.Integrated()
	if !ok || got < truth.LUFS-0.2 || got > truth.LUFS+0.2 {
		t.Errorf("the album from its songs' blocks: %.1f LUFS; as one file ffmpeg measures %.1f", got, truth.LUFS)
	}
	if peak < truth.Peak-0.3 || peak > truth.Peak+0.3 {
		t.Errorf("the album's peak, the highest of its songs': %.1f; as one file %.1f", peak, truth.Peak)
	}
	// The mean of the songs' figures over their (equal) lengths is over a decibel and a
	// half under it: every song would be turned up that much too far.
	mean := 10 * math.Log10((math.Pow(10, own[0]/10)+math.Pow(10, own[1]/10)+math.Pow(10, own[2]/10))/3)
	if truth.LUFS-mean < 1.2 {
		t.Errorf("this album does not tell the two ways apart: as one file %.1f, mean of the songs %.1f", truth.LUFS, mean)
	}
	t.Logf("songs %.1f / %.1f / %.1f LUFS; album as one file %.1f, from blocks %.1f, mean of the songs %.1f; peak %.1f (one file %.1f)", own[0], own[1], own[2], truth.LUFS, got, mean, peak, truth.Peak)
}
