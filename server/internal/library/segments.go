package library

import (
	"strings"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/probe"
)

// Intro/outro detection from chapter markers. Anime releases from Blu-ray (and many
// web rips) carry chapters at the cold open, OP, parts A/B, ED and preview. Titles are
// often generic ("Chapter 7"), so the shape is what we go on: an OP is a 60..120 s
// chapter that starts within the first 4 minutes; an ED is one of the same length that
// starts in the last 5 minutes. A chapter before the OP that is under 3 minutes is the
// cold open (not skippable); a short chapter after the ED is the next-episode preview.
//
// Files without chapters get nothing here; audio fingerprinting across a season is
// the follow-up for those (see ROADMAP).
//
// A linked chapter (probe.Chapter.Linked) is an OP or ED the release keeps in a file of
// its own: it is not in this file, so it is never a segment here, and it says that this
// file has none of that kind. One in the first half of the file means no intro from the
// chapters, one in the second half no outro: otherwise the cold open before a linked OP,
// often about a minute long, would be taken for the OP and skipped.
// With the ED elsewhere the file's last chapter is still its preview when it is short and
// its title says so.
func segmentsFromChapters(chapters []probe.Chapter, durationMs int64) []model.MediaSegment {
	if len(chapters) < 3 || durationMs <= 0 {
		return nil
	}
	const (
		minLen   = 55_000
		maxLen   = 125_000
		typical  = 90_000 // TV anime OP/ED are 89..91 s almost without exception
		introBy  = 4 * 60_000
		outroIn  = 5 * 60_000
		previewM = 45_000
	)
	dist := func(c probe.Chapter) int64 {
		d := c.EndMs - c.StartMs - typical
		if d < 0 {
			d = -d
		}
		return d
	}
	// Among the chapters that fit the window, take the one closest to 90 s: a cold open
	// can also be 60..120 s long, and it comes first, so "first that fits" picks it.
	var out []model.MediaSegment
	var introIdx, outroIdx = -1, -1
	var introElsewhere, outroElsewhere bool
	own := 0
	for _, c := range chapters {
		switch {
		case !c.Linked:
			own++
		case c.StartMs < durationMs/2:
			introElsewhere = true
		default:
			outroElsewhere = true
		}
	}
	// An OP or ED is told from a cold open by its place among the file's own chapters, so
	// there must be three of those, as in a file that links to nothing: a prologue, the
	// episode and a linked ED are still two chapters.
	introHere, outroHere := own >= 3 && !introElsewhere, own >= 3 && !outroElsewhere
	for i, c := range chapters {
		l := c.EndMs - c.StartMs // 0 for a linked chapter: never a candidate
		if l < minLen || l > maxLen {
			continue
		}
		if c.StartMs <= introBy && i < len(chapters)-1 {
			if introHere && (introIdx < 0 || dist(c) < dist(chapters[introIdx])) {
				introIdx = i
			}
			continue
		}
		if c.StartMs >= durationMs-outroIn && i > 0 && outroHere {
			if outroIdx < 0 || dist(c) <= dist(chapters[outroIdx]) {
				outroIdx = i
			}
		}
	}
	if introIdx >= 0 {
		c := chapters[introIdx]
		out = append(out, model.MediaSegment{Type: "intro", StartMs: c.StartMs, EndMs: c.EndMs, Origin: "chapters"})
	}
	if outroIdx >= 0 && outroIdx != introIdx {
		c := chapters[outroIdx]
		out = append(out, model.MediaSegment{Type: "outro", StartMs: c.StartMs, EndMs: c.EndMs, Origin: "chapters"})
		if outroIdx+1 < len(chapters) {
			p := chapters[outroIdx+1]
			if p.EndMs-p.StartMs <= previewM {
				out = append(out, model.MediaSegment{Type: "preview", StartMs: p.StartMs, EndMs: p.EndMs, Origin: "chapters"})
			}
		}
	}
	// The ED is in another file, so nothing here is the outro; what the file ends on can
	// still be the preview, but only when its title says so: after a linked ED there is
	// as often a scene that follows the credits, and that must not be skippable.
	if outroElsewhere {
		p := chapters[len(chapters)-1]
		if l := p.EndMs - p.StartMs; !p.Linked && l > 0 && l <= previewM && p.StartMs >= durationMs-outroIn && previewTitle(p.Title) {
			out = append(out, model.MediaSegment{Type: "preview", StartMs: p.StartMs, EndMs: p.EndMs, Origin: "chapters"})
		}
	}
	return out
}

// previewTitle reports whether a chapter's title names a next-episode preview.
func previewTitle(title string) bool {
	t := strings.ToLower(title)
	return strings.Contains(t, "preview") || strings.Contains(t, "next") || strings.Contains(t, "予告")
}
