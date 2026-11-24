package music

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
)

// audioExts are the files a music folder is read for.
var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true, ".oga": true, ".opus": true,
	".wav": true, ".aiff": true, ".aif": true, ".wma": true, ".ape": true, ".wv": true, ".dsf": true, ".mka": true,
}

// IsAudioFile reports whether name is a file a music folder is read for.
func IsAudioFile(name string) bool { return audioExts[strings.ToLower(filepath.Ext(name))] }

// FileInfo is what one audio file says about itself: its tags and how it is encoded.
type FileInfo struct {
	Title       string
	Artist      string   // the artist tag as written
	Artists     []string // the "artists" tag, when the file names each artist on its own
	AlbumArtist string
	Album       string
	Compilation bool
	Disc, Track int
	Year        int
	Genres      []string
	Lyrics      string
	MBRecording string
	MBAlbum     string
	GainTrack   *float64
	PeakTrack   *float64
	GainAlbum   *float64
	PeakAlbum   *float64

	DurationMs int64
	Container  string
	Codec      string
	Bitrate    int
	SampleRate int
	BitDepth   int
	Channels   int
	// PictureStream is the index of the stream holding the cover picture, -1 for none.
	PictureStream int
}

// ReadFile asks ffprobe about one audio file.
func ReadFile(ctx context.Context, ff *ffmpeg.FFmpeg, path string) (*FileInfo, error) {
	out, err := ff.Probe(ctx, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", "-i", path)
	if err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	return parseProbe(out, path)
}

type probeOut struct {
	Format struct {
		FormatName string            `json:"format_name"`
		Duration   string            `json:"duration"`
		BitRate    string            `json:"bit_rate"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		Index            int               `json:"index"`
		CodecName        string            `json:"codec_name"`
		CodecType        string            `json:"codec_type"`
		SampleRate       string            `json:"sample_rate"`
		Channels         int               `json:"channels"`
		BitsPerRawSample string            `json:"bits_per_raw_sample"`
		BitsPerSample    int               `json:"bits_per_sample"`
		BitRate          string            `json:"bit_rate"`
		Duration         string            `json:"duration"`
		Tags             map[string]string `json:"tags"`
		Disposition      map[string]int    `json:"disposition"`
	} `json:"streams"`
}

// parseProbe reads ffprobe's JSON for an audio file. A file with no audio stream is an
// error: it is not a song, whatever it is called.
func parseProbe(out []byte, path string) (*FileInfo, error) {
	var raw probeOut
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing ffprobe output for %s: %w", filepath.Base(path), err)
	}
	info := &FileInfo{PictureStream: -1, Container: containerOf(raw.Format.FormatName, path)}
	// Tags are on the file for most formats and on the audio stream for Ogg and Opus.
	// Names differ in case between formats (ID3 "artist", Vorbis "ARTIST").
	tags := map[string]string{}
	put := func(m map[string]string) {
		for k, v := range m {
			k = strings.ToLower(strings.TrimSpace(k))
			if _, dup := tags[k]; !dup && strings.TrimSpace(v) != "" {
				tags[k] = strings.TrimSpace(v)
			}
		}
	}
	put(raw.Format.Tags)
	audio := false
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "audio":
			if audio {
				continue // the first audio stream is the song
			}
			audio = true
			put(s.Tags)
			info.Codec = strings.ToLower(s.CodecName)
			info.SampleRate, _ = strconv.Atoi(s.SampleRate)
			info.Channels = s.Channels
			if n, err := strconv.Atoi(s.BitsPerRawSample); err == nil && n > 0 {
				info.BitDepth = n
			} else if s.BitsPerSample > 0 {
				info.BitDepth = s.BitsPerSample
			}
			if n, err := strconv.Atoi(s.BitRate); err == nil {
				info.Bitrate = n
			}
			if d, err := strconv.ParseFloat(s.Duration, 64); err == nil && d > 0 {
				info.DurationMs = int64(d * 1000)
			}
		case "video":
			if info.PictureStream < 0 && s.Disposition["attached_pic"] == 1 {
				info.PictureStream = s.Index
			}
		}
	}
	if !audio {
		return nil, fmt.Errorf("%s has no audio", filepath.Base(path))
	}
	if d, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil && d > 0 {
		info.DurationMs = int64(d * 1000)
	}
	// The file's overall rate is the honest one (a lossless stream reports none of its own).
	if n, err := strconv.Atoi(raw.Format.BitRate); err == nil && n > 0 {
		info.Bitrate = n
	}

	tag := func(names ...string) string {
		for _, n := range names {
			if v := tags[n]; v != "" {
				return v
			}
		}
		return ""
	}
	info.Title = tag("title")
	info.Artist = tag("artist")
	info.AlbumArtist = tag("album_artist", "albumartist", "album artist")
	info.Album = tag("album")
	if v := tag("artists"); v != "" {
		for _, name := range strings.Split(v, ";") {
			if name = strings.TrimSpace(name); name != "" {
				info.Artists = append(info.Artists, name)
			}
		}
	}
	// A compilation flag is "1" (iTunes, Picard); release tools also put their own name
	// in every field they do not use, so anything else is not a yes.
	switch strings.ToLower(tag("compilation", "tcmp")) {
	case "1", "true", "yes":
		info.Compilation = true
	}
	info.Track = leadingNumber(tag("track", "tracknumber"))
	info.Disc = leadingNumber(tag("disc", "discnumber"))
	info.Year = yearOf(tag("originaldate", "originalyear", "date", "year", "tdor", "tyer"))
	info.Genres = genresOf(tag("genre"))
	info.MBRecording = tag("musicbrainz_trackid", "musicbrainz track id", "musicbrainz_recordingid")
	info.MBAlbum = tag("musicbrainz_albumid", "musicbrainz album id")
	info.GainTrack = decibels(tag("replaygain_track_gain", "r128_track_gain"), strings.HasPrefix(firstKey(tags, "replaygain_track_gain", "r128_track_gain"), "r128"))
	info.GainAlbum = decibels(tag("replaygain_album_gain", "r128_album_gain"), strings.HasPrefix(firstKey(tags, "replaygain_album_gain", "r128_album_gain"), "r128"))
	info.PeakTrack = number(tag("replaygain_track_peak"))
	info.PeakAlbum = number(tag("replaygain_album_peak"))
	// Lyrics: "lyrics" (Vorbis, MP4), "unsyncedlyrics", and ID3's "lyrics-<language>".
	info.Lyrics = tag("lyrics", "unsyncedlyrics", "unsynced lyrics")
	if info.Lyrics == "" {
		for k, v := range tags {
			if strings.HasPrefix(k, "lyrics-") && len(v) > len(info.Lyrics) {
				info.Lyrics = v
			}
		}
	}
	return info, nil
}

func firstKey(tags map[string]string, names ...string) string {
	for _, n := range names {
		if tags[n] != "" {
			return n
		}
	}
	return ""
}

// leadingNumber reads "3", "03" and "3/12" as 3. A number no album has (a download tool
// writing its own playlist position, say 342) is not a track number.
func leadingNumber(s string) int {
	if i := strings.IndexAny(s, "/ "); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 || n > 200 {
		return 0
	}
	return n
}

var reYear = regexp.MustCompile(`\b(1[5-9]\d\d|20\d\d)\b`)

func yearOf(s string) int {
	n, _ := strconv.Atoi(reYear.FindString(s))
	return n
}

// genresOf splits a genre tag ("Pop, Dance Pop; Disco") into its genres, one of each.
func genresOf(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == '/' || r == '\x00' }) {
		g = strings.TrimSpace(g)
		if k := strings.ToLower(g); g != "" && !seen[k] {
			seen[k] = true
			out = append(out, g)
		}
	}
	return out
}

// decibels reads a ReplayGain value ("-7.25 dB"). An R128 tag holds the gain in 1/256 dB
// against a reference 5 dB lower, which is brought to the ReplayGain scale.
func decibels(s string, r128 bool) *float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "db"))
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	if r128 {
		v = v/256 + 5
	}
	return &v
}

func number(s string) *float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &v
}

// containerOf names the file's container by its extension (what a player is told it is),
// falling back to ffprobe's name for one without.
func containerOf(formatName, path string) string {
	if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."); ext != "" {
		switch ext {
		case "aif":
			return "aiff"
		case "oga":
			return "ogg"
		}
		return ext
	}
	return strings.SplitN(formatName, ",", 2)[0]
}

var reTrackPrefix = regexp.MustCompile(`^\s*(?:\d{1,3}\s*[-._)]\s*|\d{1,2}\s+)`)

// TitleFromName is the title of a file whose tags give none: its name without the
// extension and without a leading track number ("07 - Song.mp3" is "Song").
func TitleFromName(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if t := strings.TrimSpace(reTrackPrefix.ReplaceAllString(stem, "")); t != "" {
		return t
	}
	return stem
}
