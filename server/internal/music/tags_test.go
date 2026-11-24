package music

import (
	"reflect"
	"testing"
)

// An MP3 as a ripping tool leaves it: lower-case ID3 names, a cover picture as a second
// stream, and a "track" that is the tool's own playlist position.
const probeMP3 = `{
  "format": {"format_name": "mp3", "duration": "203.064979", "bit_rate": "281457",
    "tags": {"title": "Levitating (feat. DaBaby)", "artist": "Dua Lipa, DaBaby", "album": "Levitating (feat. DaBaby)",
             "date": "2020", "genre": "Pop, Dance Pop, Disco", "track": "342", "lyrics-eng": "If you wanna run away with me"}},
  "streams": [
    {"index": 0, "codec_name": "mp3", "codec_type": "audio", "sample_rate": "48000", "channels": 2, "bit_rate": "281000", "disposition": {"attached_pic": 0}},
    {"index": 1, "codec_name": "png", "codec_type": "video", "disposition": {"attached_pic": 1}}
  ]}`

// A FLAC from a release group: upper-case Vorbis names, every unused field filled with
// the group's name, 24-bit / 96 kHz.
const probeFLAC = `{
  "format": {"format_name": "flac", "duration": "178.586667", "bit_rate": "2794666",
    "tags": {"TITLE": "I'm Only Sleeping (2022 Mix)", "ARTIST": "The Beatles", "ALBUM": "The Beatles 1962 – 1966 (2023 Edition)",
             "album_artist": "The Beatles", "track": "17", "disc": "2", "DATE": "2023-11-10", "GENRE": "Rock",
             "COMPILATION": "SOMETOOL", "LYRICS": "When I wake up early in the morning\nLift my head, I'm still yawning",
             "REPLAYGAIN_TRACK_GAIN": "-7.25 dB", "REPLAYGAIN_TRACK_PEAK": "0.988", "MUSICBRAINZ_TRACKID": "abc-123"}},
  "streams": [
    {"index": 0, "codec_name": "flac", "codec_type": "audio", "sample_rate": "96000", "channels": 2, "bits_per_raw_sample": "24"},
    {"index": 1, "codec_name": "mjpeg", "codec_type": "video", "disposition": {"attached_pic": 1}}
  ]}`

func TestParseProbe(t *testing.T) {
	mp3, err := parseProbe([]byte(probeMP3), "/music/Dua Lipa/Levitating.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if mp3.Title != "Levitating (feat. DaBaby)" || mp3.Artist != "Dua Lipa, DaBaby" || mp3.Album != "Levitating (feat. DaBaby)" {
		t.Errorf("names: %+v", mp3)
	}
	if mp3.Track != 0 {
		t.Errorf("a playlist position (342) was taken for a track number: %d", mp3.Track)
	}
	if mp3.Year != 2020 || !reflect.DeepEqual(mp3.Genres, []string{"Pop", "Dance Pop", "Disco"}) {
		t.Errorf("year %d genres %q", mp3.Year, mp3.Genres)
	}
	if mp3.Codec != "mp3" || mp3.Container != "mp3" || mp3.SampleRate != 48000 || mp3.Channels != 2 || mp3.BitDepth != 0 {
		t.Errorf("encoding: %+v", mp3)
	}
	if mp3.Bitrate != 281457 || mp3.DurationMs != 203064 {
		t.Errorf("the file's own rate and length are the ones to keep: %d b/s, %d ms", mp3.Bitrate, mp3.DurationMs)
	}
	if mp3.PictureStream != 1 {
		t.Errorf("picture stream = %d, want 1", mp3.PictureStream)
	}
	if mp3.Lyrics != "If you wanna run away with me" {
		t.Errorf("ID3 lyrics (lyrics-<language>) not read: %q", mp3.Lyrics)
	}

	flac, err := parseProbe([]byte(probeFLAC), "/music/The Beatles/x/I'm Only Sleeping.flac")
	if err != nil {
		t.Fatal(err)
	}
	if flac.Title != "I'm Only Sleeping (2022 Mix)" || flac.Artist != "The Beatles" || flac.AlbumArtist != "The Beatles" {
		t.Errorf("upper-case tag names not read: %+v", flac)
	}
	if flac.Track != 17 || flac.Disc != 2 || flac.Year != 2023 {
		t.Errorf("track %d disc %d year %d", flac.Track, flac.Disc, flac.Year)
	}
	if flac.Compilation {
		t.Error("a release tool's name in the compilation field was read as yes")
	}
	if flac.BitDepth != 24 || flac.SampleRate != 96000 || flac.Codec != "flac" {
		t.Errorf("encoding: %d-bit %d Hz %s", flac.BitDepth, flac.SampleRate, flac.Codec)
	}
	if flac.GainTrack == nil || *flac.GainTrack != -7.25 || flac.PeakTrack == nil || *flac.PeakTrack != 0.988 {
		t.Errorf("ReplayGain: %v %v", flac.GainTrack, flac.PeakTrack)
	}
	if flac.GainAlbum != nil {
		t.Error("an album gain appeared from nowhere")
	}
	if flac.MBRecording != "abc-123" {
		t.Errorf("MusicBrainz id: %q", flac.MBRecording)
	}
}

// Ogg and Opus keep their tags on the audio stream, not on the file.
func TestParseProbeReadsStreamTags(t *testing.T) {
	info, err := parseProbe([]byte(`{"format": {"format_name": "ogg", "duration": "10.5"},
		"streams": [{"index": 0, "codec_name": "opus", "codec_type": "audio", "sample_rate": "48000", "channels": 2,
			"tags": {"TITLE": "Song", "ARTIST": "Someone", "ARTISTS": "Someone;Someone Else", "TRACKNUMBER": "3/12", "R128_TRACK_GAIN": "-512"}}]}`), "/m/song.opus")
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Song" || info.Artist != "Someone" || info.Track != 3 {
		t.Errorf("%+v", info)
	}
	if !reflect.DeepEqual(info.Artists, []string{"Someone", "Someone Else"}) {
		t.Errorf("artists: %q", info.Artists)
	}
	// -512 / 256 = -2 dB against a reference 5 dB lower: +3 dB on the ReplayGain scale.
	if info.GainTrack == nil || *info.GainTrack != 3 {
		t.Errorf("R128 gain: %v", info.GainTrack)
	}
}

func TestParseProbeRefusesAFileWithNoAudio(t *testing.T) {
	if _, err := parseProbe([]byte(`{"format": {"format_name": "png_pipe"}, "streams": [{"index": 0, "codec_type": "video", "codec_name": "png"}]}`), "/m/cover.mp3"); err == nil {
		t.Error("a file with no audio was taken for a song")
	}
	if _, err := parseProbe([]byte(`not json`), "/m/x.mp3"); err == nil {
		t.Error("unreadable ffprobe output was accepted")
	}
}

func TestTitleFromName(t *testing.T) {
	for in, want := range map[string]string{
		"/m/Joost/12 - Milf.mp3":               "Milf",
		"/m/02. MANTRA.mp3":                    "MANTRA",
		"/m/Escape (The Pina Colada Song).mp3": "Escape (The Pina Colada Song)",
		"/m/07 Song.flac":                      "Song",
		"/m/1999.mp3":                          "1999", // a title that is a number
		"/m/24kGoldn - Mood.mp3":               "24kGoldn - Mood",
		"/m/100 gecs.mp3":                      "100 gecs",
	} {
		if got := TitleFromName(in); got != want {
			t.Errorf("TitleFromName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsAudioFile(t *testing.T) {
	for name, want := range map[string]bool{"a.mp3": true, "a.FLAC": true, "a.m4a": true, "a.opus": true, "a.lrc": false, "cover.jpg": false, "a.mkv": false, "mp3": false} {
		if IsAudioFile(name) != want {
			t.Errorf("IsAudioFile(%q) = %v", name, !want)
		}
	}
}
