package library

import (
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
func segmentsFromChapters(chapters []probe.Chapter, durationMs int64) []model.MediaSegment {
	if len(chapters) < 3 || durationMs <= 0 {
		return nil
	}
	const (
		minLen   = 55_000
		maxLen   = 125_000
		introBy  = 4 * 60_000
		outroIn  = 5 * 60_000
		previewM = 45_000
	)
	var out []model.MediaSegment
	var introIdx, outroIdx = -1, -1
	for i, c := range chapters {
		l := c.EndMs - c.StartMs
		if l < minLen || l > maxLen {
			continue
		}
		if introIdx < 0 && c.StartMs <= introBy && i < len(chapters)-1 {
			introIdx = i
			continue
		}
		if c.StartMs >= durationMs-outroIn && i > 0 {
			outroIdx = i // keep the last candidate
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
	return out
}
