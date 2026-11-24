package music

import (
	"bytes"
	"context"
	"image"
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
