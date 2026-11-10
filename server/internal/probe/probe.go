// Package probe runs ffprobe and normalizes the result into Roxy's MediaStream model.
package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
)

// Result is the normalized probe of one file.
type Result struct {
	Path        string              `json:"path"`
	Container   string              `json:"container"`
	Size        int64               `json:"size"`
	DurationMs  int64               `json:"durationMs"`
	Bitrate     int64               `json:"bitrate"`
	ModTime     time.Time           `json:"modTime"`
	Streams     []model.MediaStream `json:"streams"`
	Attachments []model.Attachment  `json:"attachments"`
	Chapters    []Chapter           `json:"chapters,omitempty"`
	Raw         json.RawMessage     `json:"-"`
	// StreamCount is how many streams ffprobe listed, the ones Roxy leaves out (data
	// streams, cover art) included: every index below it is the file's own.
	StreamCount int `json:"-"`
}

type Chapter struct {
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Title   string `json:"title,omitempty"`
	// Linked: what the chapter plays is in another file (Matroska segment linking: an
	// opening or ending kept once for a whole season). StartMs and EndMs are then both
	// the place in this file where that part would play. See matroska.go.
	Linked bool `json:"linked,omitempty"`
}

// ffprobe JSON shapes (only the fields we read).
type ffprobeOut struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams  []ffStream `json:"streams"`
	Chapters []struct {
		ID        int64             `json:"id"`
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

type ffStream struct {
	Index            int               `json:"index"`
	CodecName        string            `json:"codec_name"`
	CodecType        string            `json:"codec_type"`
	Profile          string            `json:"profile"`
	Level            int               `json:"level"`
	PixFmt           string            `json:"pix_fmt"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
	RFrameRate       string            `json:"r_frame_rate"`
	AvgFrameRate     string            `json:"avg_frame_rate"`
	ColorPrimaries   string            `json:"color_primaries"`
	ColorTransfer    string            `json:"color_transfer"`
	ColorSpace       string            `json:"color_space"`
	BitsPerRawSample string            `json:"bits_per_raw_sample"`
	Channels         int               `json:"channels"`
	ChannelLayout    string            `json:"channel_layout"`
	SampleRate       string            `json:"sample_rate"`
	BitRate          string            `json:"bit_rate"`
	Tags             map[string]string `json:"tags"`
	Disposition      map[string]int    `json:"disposition"`
	SideDataList     []map[string]any  `json:"side_data_list"`
}

// File probes one media file.
func File(ctx context.Context, ff *ffmpeg.FFmpeg, path string) (*Result, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	out, err := ff.Probe(ctx, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", "-show_chapters", "-i", path)
	if err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	var raw ffprobeOut
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing ffprobe output for %s: %w", filepath.Base(path), err)
	}

	res := &Result{
		Path:      path,
		Container: normalizeContainer(raw.Format.FormatName, path),
		Size:      fi.Size(),
		ModTime:   fi.ModTime(),
		Raw:       json.RawMessage(out),
	}
	if d, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil {
		res.DurationMs = int64(d * 1000)
	}
	if b, err := strconv.ParseInt(raw.Format.BitRate, 10, 64); err == nil {
		res.Bitrate = b
	}
	res.StreamCount = len(raw.Streams)
	for _, s := range raw.Streams {
		if s.Index+1 > res.StreamCount {
			res.StreamCount = s.Index + 1
		}
		switch s.CodecType {
		case "video":
			if s.Disposition["attached_pic"] == 1 {
				continue // cover art, not a video track
			}
			res.Streams = append(res.Streams, videoStream(s))
		case "audio":
			res.Streams = append(res.Streams, audioStream(s))
		case "subtitle":
			res.Streams = append(res.Streams, subtitleStream(s))
		case "attachment":
			res.Attachments = append(res.Attachments, model.Attachment{
				Index:    s.Index,
				Filename: s.Tags["filename"],
				Mime:     s.Tags["mimetype"],
			})
		}
	}
	var uids []uint64 // ffprobe's chapter id is the Matroska chapter UID, printed signed
	for _, c := range raw.Chapters {
		st, _ := strconv.ParseFloat(c.StartTime, 64)
		en, _ := strconv.ParseFloat(c.EndTime, 64)
		res.Chapters = append(res.Chapters, Chapter{StartMs: int64(st * 1000), EndMs: int64(en * 1000), Title: c.Tags["title"]})
		uids = append(uids, uint64(c.ID))
	}
	if strings.Contains(raw.Format.FormatName, "matroska") {
		eds, err := matroskaEditionsOfFile(path)
		if err != nil {
			// ffprobe's list stands. It is wrong only for a file with linked chapters,
			// which this could not find out.
			slog.Warn("probe: could not read the file's own chapter list", "file", filepath.Base(path), "err", err)
		}
		res.Chapters = ownChapters(eds, res.Chapters, uids)
	}
	return res, nil
}

// ownChapters corrects the chapters ffprobe listed for a Matroska file (uids: each one's
// chapter UID) by the file's editions: no chapter that plays another file is left as one
// of this file's own. A file without linked chapters keeps ffprobe's list as it is.
func ownChapters(eds []mkvEdition, listed []Chapter, uids []uint64) []Chapter {
	if chapters, ok := chaptersOfEditions(eds); ok {
		return chapters
	}
	linked := linkedChapterUIDs(eds) // of an edition that is not the one played
	if len(linked) == 0 {
		return listed
	}
	var own []Chapter
	for i, c := range listed {
		if !linked[uids[i]] {
			own = append(own, c)
		}
	}
	return own
}

func common(s ffStream, t model.StreamType) model.MediaStream {
	st := model.MediaStream{
		Index:             s.Index,
		Type:              t,
		Codec:             strings.ToLower(s.CodecName),
		Profile:           s.Profile,
		Level:             s.Level,
		Language:          s.Tags["language"],
		Title:             s.Tags["title"],
		IsDefault:         s.Disposition["default"] == 1,
		IsForced:          s.Disposition["forced"] == 1,
		IsHearingImpaired: s.Disposition["hearing_impaired"] == 1,
	}
	if b, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil {
		st.Bitrate = b
	}
	return st
}

func videoStream(s ffStream) model.MediaStream {
	st := common(s, model.StreamVideo)
	st.PixFmt = s.PixFmt
	st.Width, st.Height = s.Width, s.Height
	st.FPS = parseRate(s.AvgFrameRate)
	if st.FPS == 0 {
		st.FPS = parseRate(s.RFrameRate)
	}
	st.ColorPrimaries, st.ColorTransfer, st.ColorSpace = s.ColorPrimaries, s.ColorTransfer, s.ColorSpace
	st.BitDepth = bitDepth(s.PixFmt, s.BitsPerRawSample)
	st.RangeType, st.DVProfile = rangeType(s)
	return st
}

func audioStream(s ffStream) model.MediaStream {
	st := common(s, model.StreamAudio)
	st.Channels = s.Channels
	st.ChannelLayout = s.ChannelLayout
	st.SampleRate, _ = strconv.Atoi(s.SampleRate)
	return st
}

func subtitleStream(s ffStream) model.MediaStream {
	st := common(s, model.StreamSubtitle)
	st.IsTextFormat = model.IsTextSubtitleCodec(st.Codec)
	return st
}

var pixDepth = regexp.MustCompile(`p(\d{2})(le|be)?$`)

func bitDepth(pixFmt, bitsPerRaw string) int {
	if m := pixDepth.FindStringSubmatch(pixFmt); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 9 && n <= 16 {
			return n
		}
	}
	if n, err := strconv.Atoi(bitsPerRaw); err == nil && n > 0 {
		return n
	}
	if pixFmt != "" {
		return 8
	}
	return 0
}

func rangeType(s ffStream) (model.RangeType, int) {
	dv := 0
	hdr10plus := false
	for _, sd := range s.SideDataList {
		t, _ := sd["side_data_type"].(string)
		switch {
		case strings.Contains(t, "DOVI configuration"):
			if p, ok := sd["dv_profile"].(float64); ok {
				dv = int(p)
			}
		case strings.Contains(t, "HDR Dynamic Metadata") || strings.Contains(t, "SMPTE2094"):
			hdr10plus = true
		}
	}
	if dv > 0 {
		return model.RangeDV, dv
	}
	switch s.ColorTransfer {
	case "smpte2084":
		if hdr10plus {
			return model.RangeHDR10Plus, 0
		}
		return model.RangeHDR10, 0
	case "arib-std-b67":
		return model.RangeHLG, 0
	}
	if s.CodecName != "" {
		return model.RangeSDR, 0
	}
	return "", 0
}

func parseRate(r string) float64 {
	if r == "" || r == "0/0" {
		return 0
	}
	parts := strings.SplitN(r, "/", 2)
	num, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	if len(parts) == 1 {
		return num
	}
	den, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || den == 0 {
		return 0
	}
	return num / den
}

// normalizeContainer maps ffprobe's comma-separated format_name to one container token.
func normalizeContainer(formatName, path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	switch {
	case strings.Contains(formatName, "matroska"):
		if ext == "webm" {
			return "webm"
		}
		return "mkv"
	case strings.Contains(formatName, "mp4"):
		if ext == "m4v" || ext == "mov" {
			return ext
		}
		return "mp4"
	case strings.Contains(formatName, "avi"):
		return "avi"
	case strings.Contains(formatName, "mpegts"):
		return "ts"
	}
	if ext != "" {
		return ext
	}
	return strings.SplitN(formatName, ",", 2)[0]
}
