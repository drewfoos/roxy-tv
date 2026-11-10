package probe

import (
	"context"
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"os"
	"path/filepath"
	"strconv"
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
// Each attachment is asked for by its stream number and written under a name chosen here
// (see extractAttachments). When some of them could not be produced the ones that were
// are kept and filled in, and the error is a *MissingAttachments naming the rest: an
// extraction that leaves a font out is never reported as a success.
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
//
// Attachments a finished ffmpeg run did not produce are remembered (attachmentGaps), so a
// font that cannot be extracted costs one run, not one per request for it.
func EnsureAttachments(ctx context.Context, ff *ffmpeg.FFmpeg, path, dir string, atts []model.Attachment) error {
	if len(atts) == 0 {
		return nil
	}
	defer attachmentLocks.lock(dir)()
	missing := attachmentsPresent(dir, atts)
	if len(missing) == 0 {
		return nil
	}
	if attachmentGaps.covers(dir, path, missing) {
		return &MissingAttachments{File: filepath.Base(path), Names: missing}
	}
	return extractAttachments(ctx, ff, path, dir, atts)
}

// MissingAttachments is the error of an extraction that produced some of a file's
// attachments but not these. The others are extracted and filled in.
type MissingAttachments struct {
	File  string
	Names []string // as the file names them
	Err   error    // what ffmpeg said, when it failed as well
}

func (e *MissingAttachments) Error() string {
	msg := fmt.Sprintf("extracting attachments from %s: %d not extracted (%s)", e.File, len(e.Names), strings.Join(e.Names, ", "))
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *MissingAttachments) Unwrap() error { return e.Err }

var attachmentLocks keyLocks

// attachmentGaps remembers, per attachment folder, what a finished ffmpeg run did not
// produce from the video as it was then (its size and modification time).
var attachmentGaps gapMemo

type gapMemo struct {
	mu sync.Mutex
	m  map[string]attachmentGap
}

type attachmentGap struct {
	size  int64
	mod   time.Time
	names map[string]bool // stored names
}

func (g *gapMemo) set(dir, video string, stored []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fi, err := os.Stat(video)
	if len(stored) == 0 || err != nil {
		delete(g.m, dir)
		return
	}
	if g.m == nil {
		g.m = map[string]attachmentGap{}
	}
	gap := attachmentGap{size: fi.Size(), mod: fi.ModTime(), names: map[string]bool{}}
	for _, n := range stored {
		gap.names[n] = true
	}
	g.m[dir] = gap
}

// covers reports whether every one of the missing attachments is known not to come out
// of the video, which is still the file it was.
func (g *gapMemo) covers(dir, video string, missing []string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	gap, ok := g.m[dir]
	if !ok {
		return false
	}
	fi, err := os.Stat(video)
	if err != nil || fi.Size() != gap.size || !fi.ModTime().Equal(gap.mod) {
		delete(g.m, dir)
		return false
	}
	for _, n := range missing {
		if !gap.names[n] {
			return false
		}
	}
	return true
}

// storedNames is the file each attachment is kept as in its folder: its own name, made a
// single path component, and for a name an earlier attachment already has, that name
// with the stream number in front. Two attachments of one name are two fonts (a renderer
// finds a font by the family inside it, not by its file name), and under one name the
// second replaced the first.
func storedNames(atts []model.Attachment) []string {
	names := make([]string, len(atts))
	taken := map[string]bool{}
	for i, a := range atts {
		name := safeName(a.Filename)
		for n := name; taken[name]; n = name {
			name = fmt.Sprintf("%d-%s", a.Index, n)
		}
		taken[name] = true
		names[i] = name
	}
	return names
}

// attachmentsPresent fills in the attachments that are in dir and returns the stored
// names of those that are not.
func attachmentsPresent(dir string, atts []model.Attachment) (missing []string) {
	for i, name := range storedNames(atts) {
		p := filepath.Join(dir, name)
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() {
			missing = append(missing, name)
			continue
		}
		t := fi.ModTime().UTC()
		atts[i].ExtractedPath, atts[i].ExtractedAt = p, &t
	}
	return missing
}

// extractTemp names the temporary folders extractAttachments works in.
const extractTemp = ".extract-"

// extractAttachments runs ffmpeg once for all the attachments, each asked for by its
// stream number and written to a file named after that number in a temporary folder.
//
// "-dump_attachment:t" with an empty name, which writes each attachment under its own
// "filename" tag, is not used: ffmpeg quietly skips a tag it finds unsafe (one with a
// folder in it, "fonts/A.ttf") and still exits 0, a missing tag stops it, and two
// attachments with one name are written over each other.
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
	args := []string{"-y", "-v", "error"}
	for _, a := range atts {
		args = append(args, fmt.Sprintf("-dump_attachment:%d", a.Index), strconv.Itoa(a.Index))
	}
	args = append(args, "-i", path, "-t", "0", "-f", "null", "-")
	_, runErr := ff.Run(ctx, tmp, args...)
	if runErr != nil && strings.Contains(runErr.Error(), "At least one output file") {
		runErr = nil // the attachments are written before ffmpeg looks for an output
	}
	// A stopped ffmpeg leaves whatever it was writing; none of it is kept.
	if runErr != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	// ffmpeg may have written the files before complaining (about a stream it cannot
	// decode, say), and exits 0 without writing one it has nothing for: what counts is
	// which files are there.
	now := time.Now().UTC()
	names := storedNames(atts)
	var missing, missingStored []string
	for i := range atts {
		from := filepath.Join(tmp, strconv.Itoa(atts[i].Index))
		if fi, err := os.Stat(from); err != nil || !fi.Mode().IsRegular() {
			missing = append(missing, safeName(atts[i].Filename))
			missingStored = append(missingStored, names[i])
			continue
		}
		p := filepath.Join(dir, names[i])
		if err := safefs.Rename(from, p); err != nil {
			return err
		}
		t := now
		atts[i].ExtractedPath, atts[i].ExtractedAt = p, &t
	}
	if runErr != nil {
		// Not remembered: ffmpeg failing may pass (a file still being copied in).
		attachmentGaps.set(dir, path, nil)
		if len(missing) == len(atts) {
			return fmt.Errorf("extracting attachments from %s: %w", filepath.Base(path), runErr)
		}
	} else {
		attachmentGaps.set(dir, path, missingStored)
	}
	if len(missing) > 0 {
		return &MissingAttachments{File: filepath.Base(path), Names: missing, Err: runErr}
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
	if name == "" || name == "." || name == ".." || name == "/" {
		return "attachment"
	}
	return name
}
