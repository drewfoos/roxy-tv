package library

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/probe"
)

// chapterRules is the version of the rules that turn a file's chapters into segments
// (segmentsFromChapters, and what probe hands it). Raise it when a change to them makes
// segments already stored wrong, and teach RecheckChapterSegments which files to look at.
//
//	1: a chapter that plays from another file (a linked OP or ED) is not this file's, and
//	   the chapters near it are not an intro or an outro either.
const chapterRules = 1

// chapterRulesKey is the setting holding the chapterRules the stored segments follow.
const chapterRulesKey = "segments:chapter-rules"

// RecheckChapterSegments brings segments stored from chapters under older rules up to
// the present ones, once: a normal scan only reads files that changed, so without this
// the skip buttons an older build got wrong would stay until each file was rescanned by
// hand. It returns how many files had their segments changed.
//
// Rules 1 only change files that link to others, and finding those takes only each
// file's chapter list (no ffprobe), so every file is asked. Segments from other origins
// are left alone. A file that is not there (an unmounted drive, most likely) is left as
// it is and the pass runs again at the next start, until every file has been seen.
//
// Not covered: a file whose played edition is plain while another edition links out. A
// forced scan corrects those.
func (l *Library) RecheckChapterSegments(ctx context.Context) (changed int, err error) {
	var have int
	if _, err := l.store.GetSetting(ctx, chapterRulesKey, &have); err != nil {
		return 0, err
	}
	if have >= chapterRules {
		return 0, nil
	}
	sources, err := l.store.SourcesWithSegments(ctx)
	if err != nil {
		return 0, err
	}
	missing := 0
	for _, src := range sources {
		if err := ctx.Err(); err != nil {
			return changed, err
		}
		if _, err := os.Stat(src.Path); err != nil {
			missing++
			continue
		}
		chapters, linked, err := probe.LinkedChapters(src.Path)
		if err != nil {
			// A chapter list this reader cannot follow: a scan goes by ffprobe's list for
			// such a file, which is what the stored segments came from.
			slog.Warn("segments: could not read the file's own chapter list", "file", filepath.Base(src.Path), "err", err)
			continue
		}
		if !linked {
			continue
		}
		// What the chapters give now, less the kinds another origin already holds (there
		// is one segment of a kind per file, and a stored one is not displaced), against
		// what is stored from chapters.
		var stored, wanted []model.MediaSegment
		taken := map[string]bool{}
		for _, sg := range src.Segments {
			if sg.Origin == "chapters" {
				stored = append(stored, sg)
			} else {
				taken[sg.Type] = true
			}
		}
		for _, sg := range segmentsFromChapters(chapters, src.DurationMs) {
			if !taken[sg.Type] {
				wanted = append(wanted, sg)
			}
		}
		if sameSegments(wanted, stored) {
			continue
		}
		if err := l.store.ReplaceSegmentsByOrigin(ctx, src.SourceID, "chapters", wanted); err != nil {
			return changed, err
		}
		changed++
	}
	if missing > 0 {
		slog.Warn("segments: some files could not be found; their chapters are checked again at the next start", "files", missing, "corrected", changed)
		return changed, nil
	}
	if changed > 0 {
		slog.Info("segments: corrected skip markers taken from chapters that play from another file", "files", changed)
	}
	return changed, l.store.SetSetting(ctx, chapterRulesKey, chapterRules)
}

// sameSegments reports whether two lists hold the same segments, in any order.
func sameSegments(a, b []model.MediaSegment) bool {
	if len(a) != len(b) {
		return false
	}
	for _, sg := range a {
		if !slices.Contains(b, sg) {
			return false
		}
	}
	return true
}
