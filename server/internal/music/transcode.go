package music

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// Conversion: a song in another format, for a player that cannot play the file as it is
// or a connection that should not carry it (a 100 MB FLAC over a phone's data plan).
// The whole song is converted once into the cache folder and then served like any file,
// so a player can seek in it and ask for parts of it; the next request for the same
// song in the same format is the cached file. The cache has a size, and the files
// longest unused go first.

// AudioFormat is what a song is converted to.
type AudioFormat struct {
	Codec    string // "opus", "mp3", "aac" or "flac"
	BitrateK int    // kbit/s; 0 for flac (lossless: 16 bits, 48 kHz at most)
}

// audioBitrates are the only bitrates converted to: a request snaps to the nearest, so
// that the cache cannot be filled with a copy per kilobit.
var audioBitrates = []int{64, 96, 128, 160, 192, 256, 320}

var defaultBitrate = map[string]int{"opus": 128, "mp3": 192, "aac": 192}

// ErrBadAudioFormat: the format asked for is not one a song is converted to.
var ErrBadAudioFormat = errors.New("unknown audio format (opus, mp3, aac or flac)")

// ParseAudioFormat reads a request's format and bitrate ("opus", "160").
func ParseAudioFormat(format, bitrate string) (AudioFormat, error) {
	f := AudioFormat{Codec: strings.ToLower(strings.TrimSpace(format))}
	switch f.Codec {
	case "flac":
		return f, nil
	case "opus", "mp3", "aac":
	default:
		return f, ErrBadAudioFormat
	}
	want := defaultBitrate[f.Codec]
	if b := strings.TrimSpace(bitrate); b != "" {
		n, err := strconv.Atoi(b)
		if err != nil || n <= 0 {
			return f, errors.New("the bitrate must be a number of kbit/s")
		}
		want = n
	}
	f.BitrateK = audioBitrates[0]
	for _, b := range audioBitrates {
		if abs(b-want) < abs(f.BitrateK-want) {
			f.BitrateK = b
		}
	}
	if f.Codec == "opus" && f.BitrateK > 256 {
		f.BitrateK = 256 // past this Opus is as good as it gets
	}
	return f, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Ext is the converted file's extension.
func (f AudioFormat) Ext() string {
	switch f.Codec {
	case "opus":
		return "ogg"
	case "aac":
		return "m4a"
	}
	return f.Codec
}

// Mime is the converted file's type.
func (f AudioFormat) Mime() string {
	switch f.Codec {
	case "opus":
		return "audio/ogg"
	case "mp3":
		return "audio/mpeg"
	case "aac":
		return "audio/mp4"
	}
	return "audio/flac"
}

// args is the ffmpeg command that converts in to out. Sound with more than two channels
// is mixed down to stereo; a sample rate the format cannot carry is brought down.
func (f AudioFormat) args(in, out string, sampleRate, channels int) []string {
	a := []string{"-v", "error", "-y", "-i", in, "-map", "0:a:0", "-vn", "-sn", "-dn", "-map_metadata", "-1"}
	if channels > 2 {
		a = append(a, "-ac", "2")
	}
	rate := func(max int) {
		if sampleRate > max {
			a = append(a, "-ar", strconv.Itoa(max))
		}
	}
	bitrate := strconv.Itoa(f.BitrateK) + "k"
	switch f.Codec {
	case "opus":
		a = append(a, "-c:a", "libopus", "-b:a", bitrate, "-vbr", "on", "-f", "ogg")
	case "mp3":
		rate(48000)
		a = append(a, "-c:a", "libmp3lame", "-b:a", bitrate, "-f", "mp3")
	case "aac":
		rate(48000)
		a = append(a, "-c:a", "aac", "-b:a", bitrate, "-movflags", "+faststart", "-f", "mp4")
	default: // flac: CD-like, whatever the file was (88.2 and 176.4 kHz come down to 44.1, the rest to 48)
		switch {
		case sampleRate > 48000 && sampleRate%44100 == 0:
			a = append(a, "-ar", "44100")
		case sampleRate > 48000:
			a = append(a, "-ar", "48000")
		}
		a = append(a, "-c:a", "flac", "-sample_fmt", "s16", "-f", "flac")
	}
	return append(a, out)
}

// Transcoder converts songs and keeps the results.
type Transcoder struct {
	ff       *ffmpeg.FFmpeg
	dir      string
	maxBytes int64
	// Limit is how long one conversion may take.
	Limit time.Duration

	mu    sync.Mutex
	calls map[string]*conversion
	slots chan struct{} // conversions running at once
}

type conversion struct {
	done chan struct{}
	err  error
}

// NewTranscoder keeps converted songs under dir (inside the data folder), up to
// maxBytes of them, converting at most two at a time.
func NewTranscoder(ff *ffmpeg.FFmpeg, dir string, maxBytes int64) *Transcoder {
	return &Transcoder{ff: ff, dir: dir, maxBytes: maxBytes, Limit: 10 * time.Minute, calls: map[string]*conversion{}, slots: make(chan struct{}, 2)}
}

// Source is the file to convert, and what tells one version of it from another.
type Source struct {
	TrackID    string
	Path       string
	Size       int64
	Mtime      int64
	SampleRate int
	Channels   int
}

// File returns the converted song's path, converting it first when it is not in the
// cache. Requests for the same song in the same format while it is being converted wait
// for that one conversion. The conversion goes on when the request that started it
// leaves (the next one finds it done); ctx only ends this caller's wait.
func (t *Transcoder) File(ctx context.Context, src Source, f AudioFormat) (string, error) {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d|%s|%d", src.TrackID, src.Size, src.Mtime, f.Codec, f.BitrateK)))
	key := hex.EncodeToString(sum[:16])
	path := filepath.Join(t.dir, key[:2], key+"."+f.Ext())
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		now := time.Now()
		_ = os.Chtimes(path, now, now) // used: it is not the next to go
		return path, nil
	}
	t.mu.Lock()
	c := t.calls[key]
	if c == nil {
		c = &conversion{done: make(chan struct{})}
		t.calls[key] = c
		go func() {
			c.err = t.convert(src, f, path)
			t.mu.Lock()
			delete(t.calls, key)
			t.mu.Unlock()
			close(c.done)
		}()
	}
	t.mu.Unlock()
	select {
	case <-c.done:
		if c.err != nil {
			return "", c.err
		}
		return path, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (t *Transcoder) convert(src Source, f AudioFormat, path string) error {
	t.slots <- struct{}{}
	defer func() { <-t.slots }()
	ctx, cancel := context.WithTimeout(context.Background(), t.Limit)
	defer cancel()
	if err := safefs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".part"
	started := time.Now()
	if _, err := t.ff.Run(ctx, "", f.args(src.Path, tmp, src.SampleRate, src.Channels)...); err != nil {
		_ = safefs.Remove(tmp)
		return fmt.Errorf("converting to %s: %w", f.Codec, err)
	}
	if fi, err := os.Stat(tmp); err != nil || fi.Size() == 0 {
		_ = safefs.Remove(tmp)
		return fmt.Errorf("converting to %s: ffmpeg wrote nothing", f.Codec)
	}
	if err := safefs.Rename(tmp, path); err != nil {
		_ = safefs.Remove(tmp)
		return err
	}
	slog.Debug("music: converted", "file", filepath.Base(src.Path), "to", f.Codec, "kbps", f.BitrateK, "took", time.Since(started).Round(time.Millisecond))
	t.trim(path)
	return nil
}

// trim removes the converted songs longest unused until the cache is within its size
// (keep, the one just made, stays whatever its age).
func (t *Transcoder) trim(keep string) {
	type file struct {
		path string
		size int64
		used time.Time
	}
	var files []file
	var total int64
	_ = filepath.WalkDir(t.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		// A ".part" left by a conversion that was cut off (the server stopped) is rubbish
		// once it is old; one being written now is not touched.
		if strings.HasSuffix(p, ".part") {
			if time.Since(fi.ModTime()) > 2*t.Limit {
				_ = safefs.Remove(p)
			}
			return nil
		}
		files = append(files, file{p, fi.Size(), fi.ModTime()})
		total += fi.Size()
		return nil
	})
	if total <= t.maxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].used.Before(files[j].used) })
	for _, f := range files {
		if total <= t.maxBytes {
			break
		}
		if f.path == keep {
			continue
		}
		if safefs.Remove(f.path) == nil {
			total -= f.size
		}
	}
}
