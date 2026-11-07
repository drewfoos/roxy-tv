package probe

import (
	"context"
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
)

// ExtractAttachments dumps every attachment stream of the file into dir and fills in
// ExtractedPath/ExtractedAt on the given attachments. This runs eagerly at ingest
// (ADR: eager extraction) so burn-in and clients always find the fonts.
//
// ffmpeg writes each attachment to its "filename" tag relative to the working directory,
// so we run with cmd.Dir = dir. Names are sanitized to a single path component.
func ExtractAttachments(ctx context.Context, ff *ffmpeg.FFmpeg, path, dir string, atts []model.Attachment) error {
	if len(atts) == 0 {
		return nil
	}
	if err := safefs.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// jellyfin-ffmpeg: -dump_attachment:t "" dumps all attachments using their filename tags.
	_, err := ff.Run(ctx, dir, "-y", "-v", "error", "-dump_attachment:t", "", "-i", path, "-t", "0", "-f", "null", "-")
	if err != nil && !strings.Contains(err.Error(), "At least one output file") {
		// ffmpeg may still have written the files before complaining; verify below.
		if _, statErr := os.Stat(filepath.Join(dir, safeName(atts[0].Filename))); statErr != nil {
			return fmt.Errorf("extracting attachments from %s: %w", filepath.Base(path), err)
		}
	}
	now := time.Now().UTC()
	for i := range atts {
		p := filepath.Join(dir, safeName(atts[i].Filename))
		if _, err := os.Stat(p); err == nil {
			atts[i].ExtractedPath = p
			t := now
			atts[i].ExtractedAt = &t
		}
	}
	return nil
}

// subtitleLocks serialises extraction per cache file, so a burst of requests for the same
// track (a player retrying, ingest warming the cache while someone presses play) runs
// ffmpeg once instead of several times over the same file.
var subtitleLocks sync.Map

// EnsureTextSubtitle extracts the track to dst unless a copy is already there. A copy at
// dst is always complete: extraction writes a temporary file and renames it into place.
func EnsureTextSubtitle(ctx context.Context, ff *ffmpeg.FFmpeg, path string, streamIndex int, codec, dst string) error {
	mu, _ := subtitleLocks.LoadOrStore(dst, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	defer subtitleLocks.Delete(dst)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	return ExtractTextSubtitle(ctx, ff, path, streamIndex, codec, dst)
}

// ExtractTextSubtitle copies one embedded text subtitle stream (ASS/SSA/SRT/VTT) to dst
// without conversion. Never converts between formats: ASS stays ASS byte for byte.
//
// ffmpeg writes to a temporary file next to dst, renamed into place only on success.
// Callers skip extraction whenever dst exists, so a file ffmpeg left half written (killed,
// cancelled request, full disk) at dst would be served truncated from then on.
func ExtractTextSubtitle(ctx context.Context, ff *ffmpeg.FFmpeg, path string, streamIndex int, codec, dst string) error {
	if err := safefs.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	format := map[string]string{"ass": "ass", "ssa": "ass", "subrip": "srt", "srt": "srt", "webvtt": "webvtt", "vtt": "webvtt"}[codec]
	conv := "copy"
	if format == "" && ConvertedToSRT(codec) {
		// Text formats no player reads (MP4's mov_text, SAMI, MicroDVD, plain text) become
		// SubRip; ASS, SRT and WebVTT are always copied as they are.
		format, conv = "srt", "srt"
	}
	if format == "" {
		return fmt.Errorf("unsupported text subtitle codec %q", codec)
	}
	if err := safefs.Check(dst); err != nil {
		return err
	}
	tf, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := tf.Name()
	tf.Close()
	defer func() { _ = safefs.Remove(tmp) }() // a no-op after the rename
	if _, err := ff.Run(ctx, "", "-y", "-v", "error", "-i", path, "-map", fmt.Sprintf("0:%d", streamIndex), "-c:s", conv, "-fs", fmt.Sprint(MaxTextSubtitle), "-f", format, tmp); err != nil {
		return err
	}
	// Real tracks are a few MB even with karaoke; one at the cap is a planted bomb (an
	// 88 MB track was copied to /config whole and read into memory). Not kept truncated.
	fi, err := os.Stat(tmp)
	if err != nil {
		return err
	}
	if fi.Size() >= MaxTextSubtitle {
		return fmt.Errorf("subtitle track is larger than %d MB", MaxTextSubtitle>>20)
	}
	return safefs.Rename(tmp, dst)
}

// ConvertedToSRT reports a text subtitle codec served as SubRip (converted), not as is.
func ConvertedToSRT(codec string) bool {
	switch codec {
	case "mov_text", "sami", "microdvd", "text":
		return true
	}
	return false
}

// MaxTextSubtitle caps an extracted text subtitle track.
const MaxTextSubtitle = 32 << 20

func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		return "attachment"
	}
	return name
}
