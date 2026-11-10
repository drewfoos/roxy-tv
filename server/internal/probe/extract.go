package probe

import (
	"context"
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
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
// so it runs with a working directory of its own. Names are sanitized to a single path
// component.
//
// One extraction per folder at a time, and ffmpeg writes into a temporary folder: a font
// is moved into dir only once ffmpeg has finished, so a request never reads one that is
// still being written (or was left half written by a stopped ffmpeg).
func ExtractAttachments(ctx context.Context, ff *ffmpeg.FFmpeg, path, dir string, atts []model.Attachment) error {
	if len(atts) == 0 {
		return nil
	}
	defer attachmentLocks.lock(dir)()
	return extractAttachments(ctx, ff, path, dir, atts)
}

// EnsureAttachments is ExtractAttachments for a request that found a font not extracted
// yet: fonts already in dir are used as they are. A subtitle track asks for all its fonts
// at once, and each of those requests used to run its own ffmpeg over the whole file,
// all writing the same files.
func EnsureAttachments(ctx context.Context, ff *ffmpeg.FFmpeg, path, dir string, atts []model.Attachment) error {
	if len(atts) == 0 {
		return nil
	}
	defer attachmentLocks.lock(dir)()
	if attachmentsPresent(dir, atts) {
		return nil
	}
	return extractAttachments(ctx, ff, path, dir, atts)
}

var attachmentLocks keyLocks

// attachmentsPresent fills in the attachments from dir when every one of them is there.
func attachmentsPresent(dir string, atts []model.Attachment) bool {
	var at []time.Time
	for _, a := range atts {
		fi, err := os.Stat(filepath.Join(dir, safeName(a.Filename)))
		if err != nil || fi.IsDir() {
			return false
		}
		at = append(at, fi.ModTime().UTC())
	}
	for i := range atts {
		atts[i].ExtractedPath = filepath.Join(dir, safeName(atts[i].Filename))
		t := at[i]
		atts[i].ExtractedAt = &t
	}
	return true
}

// extractTemp names the temporary folders extractAttachments works in.
const extractTemp = ".extract-"

func extractAttachments(ctx context.Context, ff *ffmpeg.FFmpeg, path, dir string, atts []model.Attachment) error {
	if err := safefs.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Left by a run that was killed; nobody else is in this folder while its lock is held.
	if old, err := os.ReadDir(dir); err == nil {
		for _, e := range old {
			if e.IsDir() && strings.HasPrefix(e.Name(), extractTemp) {
				_ = safefs.RemoveAll(filepath.Join(dir, e.Name()))
			}
		}
	}
	tmp, err := os.MkdirTemp(dir, extractTemp+"*")
	if err != nil {
		return err
	}
	defer func() { _ = safefs.RemoveAll(tmp) }()
	// jellyfin-ffmpeg: -dump_attachment:t "" dumps all attachments using their filename tags.
	_, err = ff.Run(ctx, tmp, "-y", "-v", "error", "-dump_attachment:t", "", "-i", path, "-t", "0", "-f", "null", "-")
	if err != nil && !strings.Contains(err.Error(), "At least one output file") {
		// A stopped ffmpeg leaves whatever it was writing; none of it is kept.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// ffmpeg may still have written the files before complaining; verify below.
		if _, statErr := os.Stat(filepath.Join(tmp, safeName(atts[0].Filename))); statErr != nil {
			return fmt.Errorf("extracting attachments from %s: %w", filepath.Base(path), err)
		}
	}
	now := time.Now().UTC()
	moved := map[string]bool{} // two attachments may carry one name
	for i := range atts {
		name := safeName(atts[i].Filename)
		p := filepath.Join(dir, name)
		if !moved[name] {
			if fi, err := os.Stat(filepath.Join(tmp, name)); err != nil || fi.IsDir() {
				continue
			}
			if err := safefs.Rename(filepath.Join(tmp, name), p); err != nil {
				return err
			}
			moved[name] = true
		}
		atts[i].ExtractedPath = p
		t := now
		atts[i].ExtractedAt = &t
	}
	return nil
}

// subtitleLocks serialises extraction per cache file, so a burst of requests for the same
// track (a player retrying, ingest warming the cache while someone presses play) runs
// ffmpeg once instead of several times over the same file.
var subtitleLocks keyLocks

// EnsureTextSubtitle extracts the track to dst unless a copy is already there. A copy at
// dst is always complete: extraction writes a temporary file and renames it into place.
func EnsureTextSubtitle(ctx context.Context, ff *ffmpeg.FFmpeg, path string, streamIndex int, codec, dst string) error {
	defer subtitleLocks.lock(dst)()
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	// A few at a time across all files: each reads its whole video to find the track's
	// packets, and nothing limited how many ran at once (a season's worth of episodes
	// opened together, a scan detecting languages while people press play). Others wait
	// their turn, or give up when their request ends.
	select {
	case extractSlots <- struct{}{}:
		defer func() { <-extractSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if _, err := os.Stat(dst); err == nil { // made while this one waited
		return nil
	}
	return ExtractTextSubtitle(ctx, ff, path, streamIndex, codec, dst)
}

// extractSlots caps simultaneous subtitle extractions.
var extractSlots = make(chan struct{}, 4)

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

// SidecarCachePath is where a sidecar subtitle that cannot be served as it is goes once
// extracted or converted: a track inside a .mks, or a SAMI file. Named after the track and
// the sidecar's modification time and size, so a replaced file is never served from the
// cache.
// "" for a sidecar served as it is.
func SidecarCachePath(subtitlesDir, sourceID string, st model.MediaStream) string {
	if !st.IsExternal || (st.ExternalIndex == nil && !ConvertedToSRT(st.Codec)) {
		return ""
	}
	ext := map[string]string{"ass": ".ass", "ssa": ".ass", "subrip": ".srt", "srt": ".srt", "webvtt": ".vtt", "vtt": ".vtt"}[st.Codec]
	if ConvertedToSRT(st.Codec) || ext == "" {
		ext = ".srt"
	}
	// The time to the millisecond (as recorded) and the size: a file replaced within the
	// same second still gets a new name, so it is never served from the old one's cache.
	return filepath.Join(subtitlesDir, sourceID, fmt.Sprintf("%d-%d-%d%s", st.Index, st.ExternalModTime.UnixMilli(), st.ExternalSize, ext))
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
