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
	MaxHeight   int // 0 = keep
	BitrateK    int // video kbps
	SrcHeight   int
	SrcHDR      bool
	Container   string // source container, for the subtitle codec choice
	VAAPIDevice string
	// Audio lists the file's embedded audio tracks in order (what `-map 0:a` picks up).
	Audio []AudioTrack
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
		codec := strings.ToLower(a.Codec)
		small := a.Bitrate == 0 || a.Bitrate <= 640_000
		if r.lowQuality() {
			if (codec == "aac" || codec == "opus") && a.Channels > 0 && a.Channels <= 2 && (a.Bitrate == 0 || a.Bitrate <= 160_000) {
				args = append(args, "-c:a:"+n, "copy")
			} else {
				args = append(args, "-c:a:"+n, "aac", "-ac:a:"+n, "2", "-b:a:"+n, "128k")
			}
			continue
		}
		switch {
		case small && (codec == "aac" || codec == "ac3" || codec == "eac3" || codec == "opus" || codec == "mp3"):
			args = append(args, "-c:a:"+n, "copy")
		case a.Channels > 2:
			args = append(args, "-c:a:"+n, "aac", "-ac:a:"+n, "6", "-b:a:"+n, "384k")
		default:
			args = append(args, "-c:a:"+n, "aac", "-ac:a:"+n, "2", "-b:a:"+n, "160k")
		}
	}
	return args
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

// Detector picks the encoder transcodes use and can be told to pick again when the
// admin changes the setting. Test results are cached: a GPU does not come and go.
type Detector struct {
	ffmpeg string
	device string

	mu       sync.Mutex
	want     string // auto | nvenc | qsv | vaapi | amf | software
	decided  bool
	chosen   Encoder
	tonemapx bool
	tested   map[Encoder]bool
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
	return &Detector{ffmpeg: ffmpegPath, want: w, device: vaapiDevice, tested: map[Encoder]bool{}}
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
	slog.Info("transcode: encoder", "setting", d.want, "encoder", d.chosen, "tonemapx", d.tonemapx)
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, d.ffmpeg, args...).CombinedOutput()
	if err != nil {
		slog.Debug("transcode: encoder test", "encoder", e, "err", err, "out", strings.TrimSpace(string(out)))
		return false
	}
	return true
}

func (d *Detector) hasFilter(name string) bool {
	out, err := exec.Command(d.ffmpeg, "-hide_banner", "-filters").Output()
	return err == nil && strings.Contains(string(out), " "+name+" ")
}

// Args is the ffmpeg command line for a request, writing Matroska to stdout.
func (d *Detector) Args(r Request) []string {
	e := d.Encoder()
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
	height := r.MaxHeight
	if height <= 0 || (r.SrcHeight > 0 && height > r.SrcHeight) {
		height = r.SrcHeight
	}
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
	// Subtitles and fonts pass through untouched; MP4's mov_text has no Matroska form.
	if strings.Contains(strings.ToLower(r.Container), "mp4") || strings.Contains(strings.ToLower(r.Container), "mov") {
		args = append(args, "-c:s", "srt")
	} else {
		args = append(args, "-c:s", "copy")
	}
	args = append(args, "-c:t", "copy",
		"-avoid_negative_ts", "disabled", "-max_muxing_queue_size", "4096",
		"-f", "matroska", "pipe:1")
	return args
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
