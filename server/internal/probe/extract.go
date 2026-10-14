package probe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
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

// ExtractTextSubtitle copies one embedded text subtitle stream (ASS/SSA/SRT/VTT) to dst
// without conversion. Never converts between formats: ASS stays ASS byte for byte.
func ExtractTextSubtitle(ctx context.Context, ff *ffmpeg.FFmpeg, path string, streamIndex int, codec, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	format := map[string]string{"ass": "ass", "ssa": "ass", "subrip": "srt", "srt": "srt", "webvtt": "webvtt", "vtt": "webvtt"}[codec]
	if format == "" {
		return fmt.Errorf("unsupported text subtitle codec %q", codec)
	}
	_, err := ff.Run(ctx, "", "-y", "-v", "error", "-i", path, "-map", fmt.Sprintf("0:%d", streamIndex), "-c:s", "copy", "-f", format, dst)
	return err
}

func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		return "attachment"
	}
	return name
}
