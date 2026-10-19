package store

import (
	"context"
	"sort"
	"strings"
)

// MediaFacts summarises the files behind a series or movie for the details page:
// what is on disk, how big, which codecs, and which audio / subtitle languages.
type MediaFacts struct {
	Files      int      `json:"files"`
	Bytes      int64    `json:"bytes"`
	Video      []string `json:"video,omitempty"`     // "HEVC 10-bit 1080p HDR10", distinct
	Audio      []string `json:"audio,omitempty"`     // language codes, distinct, most common first
	Subtitles  []string `json:"subtitles,omitempty"` // language codes, distinct
	SubFormats []string `json:"subFormats,omitempty"`
}

// ItemMediaFacts gathers MediaFacts for a top-level item (series via its seasons and
// episodes, or a movie).
func (s *Store) ItemMediaFacts(ctx context.Context, itemID string) (*MediaFacts, error) {
	f := &MediaFacts{}
	// Files and bytes.
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT src.id), COALESCE(SUM(src.size), 0)
		FROM media_sources src
		JOIN items e ON src.item_id = e.id
		WHERE e.id = ? OR e.parent_id = ? OR e.parent_id IN (SELECT id FROM items WHERE parent_id = ?)`, itemID, itemID, itemID).
		Scan(&f.Files, &f.Bytes); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ms.type, ms.codec, COALESCE(ms.bit_depth, 0), COALESCE(ms.width, 0), COALESCE(ms.height, 0), COALESCE(ms.range_type, ''),
		       lower(COALESCE(NULLIF(ms.detected_language, ''), ms.language, '')), COALESCE(ms.is_external, 0)
		FROM media_streams ms
		JOIN media_sources src ON ms.source_id = src.id
		JOIN items e ON src.item_id = e.id
		WHERE e.id = ? OR e.parent_id = ? OR e.parent_id IN (SELECT id FROM items WHERE parent_id = ?)`, itemID, itemID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	video := map[string]int{}
	audio := map[string]int{}
	subs := map[string]int{}
	subFmt := map[string]int{}
	for rows.Next() {
		var typ, codec, rng, lang string
		var depth, w, h, ext int
		if err := rows.Scan(&typ, &codec, &depth, &w, &h, &rng, &lang, &ext); err != nil {
			return nil, err
		}
		switch typ {
		case "video":
			video[videoLabel(codec, depth, w, h, rng)]++
		case "audio":
			if lang != "" && lang != "und" {
				audio[lang]++
			}
		case "subtitle":
			if lang != "" && lang != "und" {
				subs[lang]++
			}
			subFmt[subFormat(codec)]++
		}
	}
	f.Video = keysByCount(video)
	f.Audio = keysByCount(audio)
	f.Subtitles = keysByCount(subs)
	f.SubFormats = keysByCount(subFmt)
	return f, rows.Err()
}

func videoLabel(codec string, depth, w, h int, rng string) string {
	name := map[string]string{"h264": "H.264", "hevc": "HEVC", "av1": "AV1", "vp9": "VP9", "mpeg4": "MPEG-4", "mpeg2video": "MPEG-2"}[strings.ToLower(codec)]
	if name == "" {
		name = strings.ToUpper(codec)
	}
	parts := []string{name}
	if depth > 8 {
		parts = append(parts, "10-bit")
	}
	switch {
	case h >= 2000 || w >= 3800:
		parts = append(parts, "4K")
	case h >= 1000:
		parts = append(parts, "1080p")
	case h >= 700:
		parts = append(parts, "720p")
	case h > 0:
		parts = append(parts, "SD")
	}
	switch strings.ToLower(rng) {
	case "hdr10", "hdr":
		parts = append(parts, "HDR10")
	case "hlg":
		parts = append(parts, "HLG")
	case "dovi", "dolbyvision", "dv":
		parts = append(parts, "Dolby Vision")
	}
	return strings.Join(parts, " ")
}

func subFormat(codec string) string {
	switch strings.ToLower(codec) {
	case "ass", "ssa":
		return "ASS"
	case "subrip", "srt":
		return "SRT"
	case "hdmv_pgs_subtitle", "pgs", "pgssub":
		return "PGS"
	case "mov_text":
		return "MP4 text"
	case "webvtt", "vtt":
		return "WebVTT"
	case "dvd_subtitle", "dvdsub":
		return "DVD"
	}
	return strings.ToUpper(codec)
}

func keysByCount(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}
