// Package transcode turns a library file into a lower-bitrate stream for Roxy's own
// players (libmpv + libass), using jellyfin-ffmpeg and whichever GPU encoder the server
// has: NVIDIA (NVENC), Intel (Quick Sync), AMD (VAAPI on Linux, AMF on Windows), or
// software x264.
//
// The output is Matroska streamed over HTTP, not HLS: video is re-encoded to H.264 and
// audio to AAC, while every subtitle track, font attachment and chapter is copied as-is.
// The player therefore keeps rendering ASS with libass, keeps dub/sub switching and
// skip markers, exactly as in direct play. Timestamps keep the file's own clock
// (-copyts), so a stream started mid-file still lines up with subtitles and segments.
// A seek beyond what the player has buffered restarts the stream at the new position.
// HLS (for browsers and Cast) is a later addition beside this, not a replacement.
package transcode

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Encoder is one way to encode H.264.
type Encoder string

const (
	NVENC    Encoder = "nvenc"
	QSV      Encoder = "qsv"
	VAAPI    Encoder = "vaapi"
	AMF      Encoder = "amf"
	Software Encoder = "software"
)

// Request is what the player asked for.
type Request struct {
	Path        string
	StartSec    float64
	MaxHeight   int // 0 = keep; else the quality asked for by its height (720, 1080, 1440...): see OutputSize
	BitrateK    int // video kbps
	SrcWidth    int
	SrcHeight   int
	SrcFPS      float64 // 0 = not known
	SrcHDR      bool
	Container   string // source container, for the subtitle codec choice
	VAAPIDevice string
	// Audio lists the file's embedded audio tracks in order (what `-map 0:a` picks up).
	Audio []AudioTrack
	// Subtitles lists the codecs of the file's embedded subtitle tracks in order (what
	// `-map 0:s` picks up). Empty: the choice falls back to the container.
	Subtitles []string
	// Web: for a browser, which cannot switch audio tracks or decode AC3 / E-AC3 / DTS /
	// TrueHD: carry one audio track (WebAudio, by its stream index) as AAC unless it is
	// already a codec browsers decode, leave subtitles and fonts out (the page draws
	// them from the API), and restart timestamps at zero at StartSec (the player adds
	// the offset back) instead of keeping the file's clock.
	Web      bool
	WebAudio *WebAudioTrack
}

// WebAudioTrack names the one audio track a web stream carries.
type WebAudioTrack struct {
	Index    int // ffprobe stream index
	Codec    string
	Channels int
}

// AudioTrack is what the per-track audio choice needs to know.
type AudioTrack struct {
	Codec    string
	Channels int
	Bitrate  int64 // bits/s; 0 when the container does not say (common in MKV)
}

// lowQuality: the 480p-class presets, meant for slow connections.
func (r Request) lowQuality() bool {
	return (r.MaxHeight > 0 && r.MaxHeight <= 480) || (r.BitrateK > 0 && r.BitrateK < 2500)
}

// audioArgs decides each audio track on its own; every track stays in the stream so the
// dub/sub switch remains instant.
//   - Normal qualities: tracks that are already compact and that every player decodes
//     (AAC, AC3, E-AC3, Opus, MP3; at most 640 kb/s) are copied untouched, which also
//     keeps them for soundbar passthrough. Heavy ones (FLAC, TrueHD, DTS, PCM) become
//     AAC, 5.1 at 384 kb/s or stereo at 160 kb/s, so surround survives.
//   - Low qualities: stereo AAC at 128 kb/s (a small stereo AAC/Opus track is copied).
func audioArgs(r Request) []string {
	if len(r.Audio) == 0 {
		return []string{"-c:a", "aac", "-ac", "2", "-b:a", "160k"}
	}
	var args []string
	for i, a := range r.Audio {
		n := strconv.Itoa(i)
		if r.lowQuality() {
			if r.copiesAudio(a) {
				args = append(args, "-c:a:"+n, "copy")
			} else {
				args = append(args, "-c:a:"+n, "aac", "-ac:a:"+n, "2", "-b:a:"+n, "128k")
			}
			continue
		}
		switch {
		case r.copiesAudio(a):
			args = append(args, "-c:a:"+n, "copy")
		case a.Channels > 2:
			args = append(args, "-c:a:"+n, "aac", "-ac:a:"+n, "6", "-b:a:"+n, "384k")
		default:
			args = append(args, "-c:a:"+n, "aac", "-ac:a:"+n, "2", "-b:a:"+n, "160k")
		}
	}
	return args
}

// copiesAudio: whether a track of the file goes out untouched (the rules above).
func (r Request) copiesAudio(a AudioTrack) bool {
	codec := strings.ToLower(a.Codec)
	if r.lowQuality() {
		return (codec == "aac" || codec == "opus") && a.Channels > 0 && a.Channels <= 2 && (a.Bitrate == 0 || a.Bitrate <= 160_000)
	}
	small := a.Bitrate == 0 || a.Bitrate <= 640_000
	return small && (codec == "aac" || codec == "ac3" || codec == "eac3" || codec == "opus" || codec == "mp3")
}

// CopiesAudio reports whether a transcode at this height and bitrate sends an audio track
// as it is in the file ("Now playing" says which tracks are converted). web: a browser's
// stream, which keeps only what browsers decode.
func CopiesAudio(maxHeight, bitrateK int, web bool, a AudioTrack) bool {
	if web {
		return BrowserAudio(a.Codec)
	}
	return Request{MaxHeight: maxHeight, BitrateK: bitrateK}.copiesAudio(a)
}

// Auto lets the server pick the first encoder that works.
const Auto = "auto"

// Candidates are the hardware encoders this OS can have, in the order Auto tries them.
func Candidates() []Encoder {
	if runtime.GOOS != "linux" {
		return []Encoder{NVENC, QSV, AMF}
	}
	return []Encoder{NVENC, QSV, VAAPI, AMF}
}

// Label is how settings screens name an encoder.
func (e Encoder) Label() string {
	switch e {
	case NVENC:
		return "NVIDIA NVENC"
	case QSV:
		return "Intel Quick Sync"
	case VAAPI:
		return "VAAPI (AMD / Intel on Linux)"
	case AMF:
		return "AMD AMF"
	}
	return "Software (CPU)"
}

// NormalizeChoice maps a setting or env value to auto | nvenc | qsv | vaapi | amf | software,
// or "" when it names nothing this OS can use.
func NormalizeChoice(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "", Auto:
		return Auto
	case "none", "software", "cpu", "x264", "libx264":
		return string(Software)
	}
	for _, e := range Candidates() {
		if v == string(e) {
			return v
		}
	}
	return ""
}

// OutputSize is the picture a request is converted to: the quality asked for is a class
// named by its height ("1440p"), which means fitting inside that height AND the width a
// 16:9 picture of that height has (2560 x 1440), never larger than the file. A film wider
// than 16:9 used to be scaled by height alone: "1440p" of a 3840 x 1604 film came out
// 3448 x 1440 (still "4K" by its width, and more than a small GPU encodes in real time),
// "720p" of a 1920 x 802 one 1724 x 720. Both sides are even; the fitted ones are the
// nearest even numbers, so a picture a hair wider than 16:9 (1915 x 1077) still gets the
// full 1280 x 720, and the width is the one ffmpeg's scale=-2:h works out. Width 0: left
// to ffmpeg (the source's width is not known).
func OutputSize(srcW, srcH, maxHeight int) (w, h int) {
	h = maxHeight
	if srcH > 0 && (h <= 0 || h > srcH) {
		h = srcH
	}
	if h <= 0 {
		return 0, 0
	}
	h -= h % 2
	if srcW <= 0 || srcH <= 0 {
		return 0, h
	}
	if boxW := maxHeight * 16 / 9; maxHeight > 0 && srcW*h > boxW*srcH {
		if fit := nearestEven(boxW, srcH, srcW); fit < h {
			h = fit
		}
	}
	if h < 2 {
		h = 2
	}
	return nearestEven(h, srcW, srcH), h
}

// nearestEven is a*b/c as the nearest even number.
func nearestEven(a, b, c int) int {
	return int((int64(a)*int64(b)+int64(c))/(2*int64(c))) * 2
}

// Detector picks the encoder transcodes use and can be told to pick again when the
// admin changes the setting. Test results are cached, but not for good: a GPU can go
// while the server runs (see Failed), and may come back.
type Detector struct {
	ffmpeg string
	device string

	mu       sync.Mutex
	want     string // auto | nvenc | qsv | vaapi | amf | software
	decided  bool
	chosen   Encoder
	tonemapx bool
	tested   map[Encoder]bool
	measures bool // time the encoders once one is chosen (NewDetector: on)
	// How many pixels a second the chosen hardware encoder and the software one get
	// through on this server, measured once in the background (see measure); 0 = not
	// known. gen counts choices, so a measurement of an encoder since replaced is dropped.
	hwRate, swRate float64
	gen            int
	// A hardware encoder that stopped working while the server ran (see Failed): which,
	// when it was last found not to work, and whether it is being tried again right now.
	lost      Encoder
	lostAt    time.Time
	retrying  bool
	rechecked time.Time // the last test run on the word of a failed conversion
}

// failedRecheck: on the word of failed conversions, a hardware encoder's test is run again
// at most this often (a file it cannot take fails every time it is opened).
var failedRecheck = 30 * time.Second

// lostRetry: a hardware encoder that stopped working is tried again this often, so one
// that returns (a device reset, a driver loaded again) is used again without a restart.
var lostRetry = 10 * time.Minute

// Failed is told that a conversion with encoder e ended in an ffmpeg error. A hardware
// encoder that passed its test at start can stop working while the server runs: a GPU
// the kernel failed to power down in the middle of an encode stays unusable until it is
// reset. Every conversion then failed the moment it began, and players asked again
// without end. The encoder's test is run again, and when it no longer passes the choice
// is made again without it. The answer is the encoder to use now.
func (d *Detector) Failed(e Encoder) Encoder {
	d.mu.Lock()
	if !d.decided {
		d.decide()
	}
	if e == Software || e != d.chosen || time.Since(d.rechecked) < failedRecheck {
		defer d.mu.Unlock()
		return d.chosen
	}
	d.rechecked = time.Now()
	d.mu.Unlock()
	ok := d.test(e) // not under the lock: conversions that start meanwhile do not wait for it
	d.mu.Lock()
	defer d.mu.Unlock()
	if ok || e != d.chosen {
		return d.chosen // the encoder is fine: it was that file, or that moment
	}
	slog.Warn("transcode: the hardware encoder stopped working; it is tried again every "+lostRetry.String(), "encoder", e)
	d.tested[e] = false
	d.lost, d.lostAt = e, time.Now()
	d.decide()
	return d.chosen
}

// retryLost runs with mu held: once lostRetry has passed, the encoder that stopped working
// is tested again in the background, and chosen again when it passes.
func (d *Detector) retryLost() {
	if d.lost == "" || d.retrying || time.Since(d.lostAt) < lostRetry {
		return
	}
	d.retrying = true
	e := d.lost
	go func() {
		ok := d.test(e)
		d.mu.Lock()
		defer d.mu.Unlock()
		d.retrying = false
		if d.lost != e {
			return
		}
		d.tested[e] = ok
		if !ok {
			d.lostAt = time.Now()
			return
		}
		slog.Info("transcode: the hardware encoder works again", "encoder", e)
		d.lost = ""
		d.decide()
	}()
}

// hwHeadroom: a hardware encoder is used for a conversion it can make at least this many
// times faster than the video plays; a seek, a second viewer or a busy scene need the rest.
const hwHeadroom = 1.5

// swRealContent: the software encoder on real pictures against the test pattern it is
// measured with (measured on a 4K film: 0.37).
const swRealContent = 0.4

// EncoderFor is the encoder for one request: the chosen hardware encoder when it can make
// that picture comfortably faster than it plays, else the software encoder if that is the
// quicker of the two here. Small GPUs are the reason: an RX 550 encodes about 90 million
// pixels a second, which is 1080p at 43 frames a second but a 1440p-class picture of a
// 24 fps film at only 1.2 times its speed, where sixteen CPU cores do 3.2 times. Until the
// speeds are measured, and when they cannot be, the chosen encoder is used as before.
func (d *Detector) EncoderFor(r Request) Encoder {
	e := d.Encoder()
	if e == Software {
		return e
	}
	d.mu.Lock()
	hw, sw := d.hwRate, d.swRate
	d.mu.Unlock()
	w, h := OutputSize(r.SrcWidth, r.SrcHeight, r.MaxHeight)
	if hw <= 0 || sw <= 0 || w <= 0 || h <= 0 {
		return e
	}
	fps := r.SrcFPS
	if fps <= 0 || fps > 240 {
		fps = 24
	}
	need := float64(w) * float64(h) * fps
	if need*hwHeadroom <= hw || sw*swRealContent <= hw {
		return e
	}
	return Software
}

// SetRates sets the measured speeds (pixels a second): for tests.
func (d *Detector) SetRates(hw, sw float64) {
	d.mu.Lock()
	d.hwRate, d.swRate = hw, sw
	d.mu.Unlock()
}

// measure times the chosen hardware encoder and the software one on four seconds of a
// 1080p test pattern each, in the background (a few seconds of one GPU and some CPU), and
// once more when the server has settled: right after a start (a boot with every other
// service starting too, a deploy) the machine is busy and both read low.
func (d *Detector) measure(e Encoder, gen int) {
	if !d.measured(gen, e, d.speed(e), d.speed(Software)) {
		return
	}
	time.Sleep(remeasureAfter)
	d.mu.Lock()
	current := gen == d.gen
	d.mu.Unlock()
	if current {
		d.measured(gen, e, d.speed(e), d.speed(Software))
	}
}

// remeasureAfter is how long after the first measurement the second one is made.
var remeasureAfter = 10 * time.Minute

// measured records the speeds found for choice number [gen], keeping the higher of each
// when there are two readings (what an encoder can do is its best one; the lower was a
// busy machine). Those of a choice since replaced are dropped: false.
func (d *Detector) measured(gen int, e Encoder, hw, sw float64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if gen != d.gen {
		return false
	}
	d.hwRate, d.swRate = max(d.hwRate, hw), max(d.swRate, sw)
	slog.Info("transcode: encoder speed", "encoder", e, "megapixelsPerSec", int(d.hwRate/1e6), "software", int(d.swRate/1e6))
	return true
}

// speed is how many pixels a second [e] encodes (a 1080p test pattern, 96 frames); 0 when
// the test could not be run.
func (d *Detector) speed(e Encoder) float64 {
	const frames, w, h = 96, 1920, 1080
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, hwInit(e, d.device)...)
	args = append(args, "-f", "lavfi", "-i", fmt.Sprintf("testsrc2=s=%dx%d:r=24:d=%d", w, h, frames/24))
	args = append(args, "-vf", videoFilter(e, h, false, false))
	args = append(args, videoCodec(e, 8000)...)
	args = append(args, "-f", "null", "-")
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	out := &capped{max: 64 << 10}
	cmd := exec.CommandContext(ctx, d.ffmpeg, args...)
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = out, out, 5*time.Second
	start := time.Now()
	if err := cmd.Run(); err != nil {
		slog.Debug("transcode: encoder speed test", "encoder", e, "err", err, "out", strings.TrimSpace(out.String()))
		return 0
	}
	took := time.Since(start).Seconds()
	if took <= 0 {
		return 0
	}
	return frames * w * h / took
}

func NewDetector(ffmpegPath, want, vaapiDevice string) *Detector {
	if vaapiDevice == "" {
		vaapiDevice = "/dev/dri/renderD128"
	}
	w := NormalizeChoice(want)
	if w == "" {
		slog.Warn("transcode: unknown hardware acceleration, using auto", "value", want)
		w = Auto
	}
	return &Detector{ffmpeg: ffmpegPath, want: w, device: vaapiDevice, tested: map[Encoder]bool{}, measures: true}
}

// Encoder is the encoder transcodes use: the chosen one when it works, else (and for
// Auto) the first that passes a one-second test encode, in the order NVENC, Quick Sync,
// VAAPI, AMF, software.
func (d *Detector) Encoder() Encoder {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.decided {
		d.decide()
	}
	d.retryLost()
	return d.chosen
}

// Choice is the configured setting (auto or one encoder), not necessarily what runs.
func (d *Detector) Choice() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.want
}

// SetChoice switches to [choice] (see NormalizeChoice). A named hardware encoder must
// pass its test encode first; otherwise nothing changes and an error says why.
func (d *Detector) SetChoice(choice string) (Encoder, error) {
	w := NormalizeChoice(choice)
	if w == "" {
		return "", fmt.Errorf("unknown encoder %q", choice)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if w != Auto && w != string(Software) && !d.works(Encoder(w)) {
		return "", fmt.Errorf("%s did not work on this server (no such GPU, driver or device access)", Encoder(w).Label())
	}
	d.want = w
	d.lost = "" // a new choice: what stopped working under the old one is not waited for
	d.decide()
	return d.chosen, nil
}

// Available tests every candidate (cached) and says which work; software always does.
func (d *Detector) Available() map[Encoder]bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[Encoder]bool{Software: true}
	for _, e := range Candidates() {
		out[e] = d.works(e)
	}
	return out
}

// decide runs with mu held.
func (d *Detector) decide() {
	var order []Encoder
	switch d.want {
	case Auto:
		order = Candidates()
	case string(Software):
	default:
		order = []Encoder{Encoder(d.want)}
	}
	d.chosen = Software
	for _, e := range order {
		if d.works(e) {
			d.chosen = e
			break
		}
		slog.Info("transcode: encoder unavailable", "encoder", e)
	}
	if !d.decided {
		d.tonemapx = d.hasFilter("tonemapx")
	}
	d.decided = true
	d.gen++
	d.hwRate, d.swRate = 0, 0
	slog.Info("transcode: encoder", "setting", d.want, "encoder", d.chosen, "tonemapx", d.tonemapx)
	if d.chosen != Software && d.measures {
		go d.measure(d.chosen, d.gen)
	}
}

// works runs with mu held.
func (d *Detector) works(e Encoder) bool {
	if ok, seen := d.tested[e]; seen {
		return ok
	}
	ok := d.test(e)
	d.tested[e] = ok
	return ok
}

func (d *Detector) test(e Encoder) bool {
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, hwInit(e, d.device)...)
	args = append(args, "-f", "lavfi", "-i", "testsrc2=s=1280x720:d=1:r=24")
	args = append(args, "-vf", videoFilter(e, 720, false, false))
	args = append(args, videoCodec(e, 2000)...)
	args = append(args, "-f", "null", "-")
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	out := &capped{max: 64 << 10}
	cmd := exec.CommandContext(ctx, d.ffmpeg, args...)
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = out, out, 5*time.Second
	if err := cmd.Run(); err != nil {
		slog.Debug("transcode: encoder test", "encoder", e, "err", err, "out", strings.TrimSpace(out.String()))
		return false
	}
	return true
}

func (d *Detector) hasFilter(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	out := &capped{max: 4 << 20} // the whole list is ~100 KB
	cmd := exec.CommandContext(ctx, d.ffmpeg, "-hide_banner", "-filters")
	cmd.Stdout, cmd.WaitDelay = out, 5*time.Second
	return cmd.Run() == nil && strings.Contains(out.String(), " "+name+" ")
}

// checkTimeout bounds each encoder check (a test encode, the filter list).
var checkTimeout = 20 * time.Second

// capped keeps at most max bytes of what is written to it (the first ones) and drops the
// rest: the encoder checks read ffmpeg's output, which a broken or odd ffmpeg binary could
// make endless, and used to hold all of it in memory (with no time limit on the filter
// list either).
type capped struct {
	max int
	buf []byte
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - len(c.buf); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		c.buf = append(c.buf, p[:room]...)
	}
	return len(p), nil
}

func (c *capped) String() string { return string(c.buf) }

// Args is the ffmpeg command line for a request, writing Matroska to stdout.
func (d *Detector) Args(r Request) []string {
	return d.ArgsWith(r, d.EncoderFor(r))
}

// ArgsWith is Args with the encoder named: the same picture from another encoder, when
// the one chosen for it failed.
func (d *Detector) ArgsWith(r Request, e Encoder) []string {
	d.mu.Lock()
	tonemapx := d.tonemapx
	d.mu.Unlock()
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	args = append(args, hwInit(e, d.device)...)
	if r.StartSec > 0 {
		// Input seek (fast, keyframe-accurate) keeping the file's own clock.
		args = append(args, "-ss", strconv.FormatFloat(r.StartSec, 'f', 3, 64))
	}
	if r.Web {
		args = append(args, "-i", r.Path, "-map", "0:v:0")
		if r.WebAudio != nil {
			args = append(args, "-map", "0:"+strconv.Itoa(r.WebAudio.Index))
		} else {
			args = append(args, "-map", "0:a:0?")
		}
	} else {
		args = append(args, "-copyts", "-i", r.Path)
		args = append(args,
			"-map", "0:v:0", "-map", "0:a?", "-map", "0:s?", "-map", "0:t?",
			"-map_metadata", "0", "-map_chapters", "0",
		)
	}
	_, height := OutputSize(r.SrcWidth, r.SrcHeight, r.MaxHeight)
	args = append(args, "-vf", videoFilter(e, height, r.SrcHDR, tonemapx))
	bitrate := r.BitrateK
	if bitrate <= 0 {
		bitrate = 8000
	}
	args = append(args, videoCodec(e, bitrate)...)
	if r.Web {
		// One track, in a codec the browser decodes; small stereo AAC / Opus is copied.
		codec, channels := "", 2
		if r.WebAudio != nil {
			codec, channels = strings.ToLower(r.WebAudio.Codec), r.WebAudio.Channels
		}
		switch {
		case browserAudio[codec] && !r.lowQuality():
			args = append(args, "-c:a", "copy")
		case channels > 2 && !r.lowQuality():
			args = append(args, "-c:a", "aac", "-ac", "6", "-b:a", "384k")
		default:
			args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "160k")
		}
		args = append(args, "-sn", "-dn", "-map_chapters", "-1",
			"-avoid_negative_ts", "make_zero", "-max_muxing_queue_size", "4096",
			"-f", "matroska", "pipe:1")
		return args
	}
	// Audio: every track (dub and sub stay switchable), each copied or converted on its own.
	args = append(args, audioArgs(r)...)
	args = append(args, subtitleArgs(r)...)
	args = append(args, "-c:t", "copy",
		"-avoid_negative_ts", "disabled", "-max_muxing_queue_size", "4096",
		"-f", "matroska", "pipe:1")
	return args
}

// matroskaSubtitles are the subtitle codecs a Matroska stream carries as they are.
var matroskaSubtitles = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true, "webvtt": true, "vtt": true,
	"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true,
	"hdmv_text_subtitle": true,
}

// subtitleArgs decides each subtitle track on its own. Subtitles pass through untouched
// where Matroska can carry them; a text format it cannot carry (MP4's mov_text) becomes
// SRT. One choice for the whole file went wrong both ways: an MP4 with a picture track
// (DVD subtitles) was asked to turn pictures into SRT, and ffmpeg gave up on the whole
// stream. A track that can be neither carried nor converted (teletext in a TS file) is
// left out, which is better than no stream: the tracks after it then sit one place
// earlier than in the file. A Matroska source is always copied: whatever it holds,
// Matroska can hold.
func subtitleArgs(r Request) []string {
	container := strings.ToLower(r.Container)
	if len(r.Subtitles) == 0 {
		if strings.Contains(container, "mp4") || strings.Contains(container, "mov") {
			return []string{"-c:s", "srt"}
		}
		return []string{"-c:s", "copy"}
	}
	matroska := strings.Contains(container, "mkv") || strings.Contains(container, "matroska") || strings.Contains(container, "webm")
	var drop, codecs []string
	out := 0
	for i, codec := range r.Subtitles {
		codec = strings.ToLower(codec)
		choice := ""
		switch {
		case matroska || matroskaSubtitles[codec]:
			choice = "copy"
		case model.IsTextSubtitleCodec(codec):
			choice = "srt"
		}
		if choice == "" {
			drop = append(drop, "-map", "-0:s:"+strconv.Itoa(i))
			continue
		}
		codecs = append(codecs, "-c:s:"+strconv.Itoa(out), choice)
		out++
	}
	return append(drop, codecs...)
}

// hwInit is the device setup before the input. Decoding stays on the CPU except for
// NVIDIA (whose decoder falls back by itself when it cannot handle a format, Hi10P for
// one); frames are scaled on the CPU and uploaded for the encoders that need GPU frames.
func hwInit(e Encoder, device string) []string {
	switch e {
	case NVENC:
		return []string{"-hwaccel", "cuda"}
	case QSV:
		return []string{"-init_hw_device", "qsv=hw", "-filter_hw_device", "hw"}
	case VAAPI:
		return []string{"-init_hw_device", "vaapi=va:" + device, "-filter_hw_device", "va"}
	}
	return nil
}

func videoFilter(e Encoder, height int, hdr, tonemapx bool) string {
	var f []string
	if hdr && tonemapx {
		f = append(f, "tonemapx=tonemap=bt2390:desat=0:peak=100:t=bt709:m=bt709:p=bt709:format=yuv420p")
	}
	if height > 0 {
		f = append(f, fmt.Sprintf("scale=-2:%d", height-height%2))
	}
	switch e {
	case QSV:
		f = append(f, "format=nv12", "hwupload=extra_hw_frames=64")
	case VAAPI:
		f = append(f, "format=nv12", "hwupload")
	case AMF:
		f = append(f, "format=nv12")
	default:
		f = append(f, "format=yuv420p")
	}
	return strings.Join(f, ",")
}

func videoCodec(e Encoder, kbps int) []string {
	br := strconv.Itoa(kbps) + "k"
	buf := strconv.Itoa(kbps*2) + "k"
	rate := []string{"-b:v", br, "-maxrate", br, "-bufsize", buf}
	switch e {
	case NVENC:
		return append([]string{"-c:v", "h264_nvenc", "-preset", "p4", "-rc", "vbr", "-profile:v", "high"}, rate...)
	case QSV:
		return append([]string{"-c:v", "h264_qsv", "-preset", "veryfast", "-profile:v", "high"}, rate...)
	case VAAPI:
		return append([]string{"-c:v", "h264_vaapi", "-profile:v", "high"}, rate...)
	case AMF:
		return append([]string{"-c:v", "h264_amf", "-quality", "speed", "-rc", "vbr_peak", "-profile:v", "high"}, rate...)
	}
	return append([]string{"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high"}, rate...)
}
