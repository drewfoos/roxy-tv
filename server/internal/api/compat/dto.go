package compat

import (
	"context"
	"fmt"
	"strings"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
)

// itemDTO renders an item as a Jellyfin BaseItemDto (the fields request app and the TV app read).
func (h *Handler) itemDTO(ctx context.Context, it *model.Item, withSources bool) map[string]any {
	dto := map[string]any{
		"Id":           it.ID,
		"ServerId":     ServerID,
		"Name":         it.Title,
		"SortName":     firstNonEmpty(it.SortTitle, it.Title),
		"Type":         jfType(it.Type),
		"IsFolder":     it.Type == model.TypeSeries || it.Type == model.TypeSeason,
		"LocationType": "FileSystem",
		"DateCreated":  jfTime(it.DateCreated),
		"ProviderIds":  providerIDs(it.ProviderIDs),
		"MediaType":    mediaType(it.Type),
	}
	if it.Year != nil {
		dto["ProductionYear"] = *it.Year
	}
	if it.Overview != "" {
		dto["Overview"] = it.Overview
	}
	if it.RuntimeMs != nil {
		dto["RunTimeTicks"] = ticks(*it.RuntimeMs)
	}
	if it.ParentID != nil {
		dto["ParentId"] = *it.ParentID
	}
	if p := auth.PrincipalFrom(ctx); p != nil && p.User != nil && (it.Type == model.TypeEpisode || it.Type == model.TypeMovie) {
		d, _ := h.store.GetUserItemData(ctx, p.ScopeID, it.GUID)
		dto["UserData"] = userDataDTO(it, d)
	}

	switch it.Type {
	case model.TypeSeason:
		if it.IndexAired != nil {
			dto["IndexNumber"] = *it.IndexAired
		}
		if it.ParentID != nil {
			dto["SeriesId"] = *it.ParentID
			if series, err := h.store.GetItem(ctx, *it.ParentID); err == nil && series != nil {
				dto["SeriesName"] = series.Title
			}
		}
	case model.TypeEpisode:
		if it.IndexAired != nil {
			dto["IndexNumber"] = *it.IndexAired
		}
		if it.SeasonAired != nil {
			dto["ParentIndexNumber"] = *it.SeasonAired
		}
		if it.IndexAbsolute != nil {
			dto["AbsoluteEpisodeNumber"] = *it.IndexAbsolute
		}
		if it.ParentID != nil {
			dto["SeasonId"] = *it.ParentID
			if season, err := h.store.GetItem(ctx, *it.ParentID); err == nil && season != nil {
				dto["SeasonName"] = season.Title
				if season.ParentID != nil {
					dto["SeriesId"] = *season.ParentID
					if series, err := h.store.GetItem(ctx, *season.ParentID); err == nil && series != nil {
						dto["SeriesName"] = series.Title
					}
				}
			}
		}
	}

	if withSources && (it.Type == model.TypeEpisode || it.Type == model.TypeMovie) {
		sources, err := h.store.SourcesForItem(ctx, it.ID)
		if err == nil && len(sources) > 0 {
			ms := make([]map[string]any, 0, len(sources))
			for i := range sources {
				ms = append(ms, h.mediaSourceDTO(it, &sources[i], nil))
			}
			dto["MediaSources"] = ms
			dto["MediaStreams"] = ms[0]["MediaStreams"]
			dto["Container"] = sources[0].Container
			if sources[0].Parts != nil {
				dto["Path"] = sources[0].Parts[0].Path
			}
			for _, st := range sources[0].Streams {
				if st.Type == model.StreamVideo {
					dto["Width"], dto["Height"] = st.Width, st.Height
					dto["IsHD"] = st.Height >= 720
					break
				}
			}
		}
	}
	return dto
}

// mediaSourceDTO renders a MediaSourceInfo. When pb is non-nil the delivery decisions
// from PlaybackInfo (direct play URL, subtitle delivery methods) are included.
func (h *Handler) mediaSourceDTO(it *model.Item, src *model.MediaSource, pb *playbackDecision) map[string]any {
	streams := make([]map[string]any, 0, len(src.Streams))
	for _, st := range src.Streams {
		streams = append(streams, h.streamDTO(it, src, st, pb))
	}
	atts := make([]map[string]any, 0, len(src.Attachments))
	for _, a := range src.Attachments {
		atts = append(atts, map[string]any{
			"Codec":       attachmentCodec(a),
			"Index":       a.Index,
			"FileName":    a.Filename,
			"MimeType":    a.Mime,
			"DeliveryUrl": fmt.Sprintf("/Videos/%s/%s/Attachments/%d", it.ID, src.ID, a.Index),
		})
	}
	path := ""
	if len(src.Parts) > 0 {
		path = src.Parts[0].Path
	}
	dto := map[string]any{
		"Id":                       src.ID,
		"Protocol":                 "File",
		"Path":                     path,
		"Type":                     "Default",
		"Container":                src.Container,
		"Size":                     src.Size,
		"Name":                     src.Name,
		"IsRemote":                 false,
		"RunTimeTicks":             ticks(src.DurationMs),
		"Bitrate":                  src.Bitrate,
		"ReadAtNativeFramerate":    false,
		"IgnoreDts":                false,
		"IgnoreIndex":              false,
		"GenPtsInput":              false,
		"SupportsTranscoding":      false, // Phase 3
		"SupportsDirectStream":     false, // Phase 3 (remux)
		"SupportsDirectPlay":       true,
		"IsInfiniteStream":         false,
		"RequiresOpening":          false,
		"RequiresClosing":          false,
		"RequiresLooping":          false,
		"SupportsProbing":          true,
		"UseMostCompatibleTranscodingProfile": false,
		"VideoType":                "VideoFile",
		"MediaStreams":             streams,
		"MediaAttachments":         atts,
		"Formats":                  []any{},
		"RequiredHttpHeaders":      map[string]any{},
		"DefaultAudioStreamIndex":  defaultStream(src, model.StreamAudio),
		"TranscodingSubProtocol":   "http",
		"HasSegments":              false,
	}
	if pb != nil {
		dto["DirectStreamUrl"] = pb.directPlayURL
		if pb.subtitleIndex >= 0 {
			dto["DefaultSubtitleStreamIndex"] = pb.subtitleIndex
		}
		if pb.audioIndex >= 0 {
			dto["DefaultAudioStreamIndex"] = pb.audioIndex
		}
	}
	return dto
}

func (h *Handler) streamDTO(it *model.Item, src *model.MediaSource, st model.MediaStream, pb *playbackDecision) map[string]any {
	dto := map[string]any{
		"Index":             st.Index,
		"Type":              jfStreamType(st.Type),
		"Codec":             jfCodec(st.Codec),
		"IsDefault":         st.IsDefault,
		"IsForced":          st.IsForced,
		"IsHearingImpaired": st.IsHearingImpaired,
		"IsExternal":        st.IsExternal,
		"IsInterlaced":      false,
		"IsTextSubtitleStream": st.Type == model.StreamSubtitle && st.IsTextFormat,
		"SupportsExternalStream": st.Type == model.StreamSubtitle && st.IsTextFormat,
		"DisplayTitle":      displayTitle(st),
	}
	if st.Language != "" {
		dto["Language"] = st.Language
	}
	if st.Title != "" {
		dto["Title"] = st.Title
	}
	if st.Profile != "" {
		dto["Profile"] = st.Profile
	}
	if st.Level != 0 {
		dto["Level"] = st.Level
	}
	if st.Bitrate != 0 {
		dto["BitRate"] = st.Bitrate
	}
	switch st.Type {
	case model.StreamVideo:
		dto["Width"], dto["Height"] = st.Width, st.Height
		dto["BitDepth"] = st.BitDepth
		dto["PixelFormat"] = st.PixFmt
		dto["AverageFrameRate"] = st.FPS
		dto["RealFrameRate"] = st.FPS
		dto["AspectRatio"] = aspect(st.Width, st.Height)
		dto["VideoRange"] = videoRange(st.RangeType)
		dto["VideoRangeType"] = videoRangeType(st.RangeType)
		if st.ColorPrimaries != "" {
			dto["ColorPrimaries"] = st.ColorPrimaries
		}
		if st.ColorTransfer != "" {
			dto["ColorTransfer"] = st.ColorTransfer
		}
		if st.ColorSpace != "" {
			dto["ColorSpace"] = st.ColorSpace
		}
		if st.DVProfile != 0 {
			dto["DvProfile"] = st.DVProfile
		}
		dto["IsAVC"] = st.Codec == "h264"
	case model.StreamAudio:
		dto["Channels"] = st.Channels
		dto["ChannelLayout"] = st.ChannelLayout
		dto["SampleRate"] = st.SampleRate
	case model.StreamSubtitle:
		if st.IsExternal {
			dto["Path"] = st.ExternalPath
		}
		method := "Embed"
		if st.IsTextFormat {
			method = "External"
		}
		if pb != nil {
			if m, ok := pb.subtitleMethods[st.Index]; ok {
				method = m
			}
		}
		dto["DeliveryMethod"] = method
		if method == "External" && st.IsTextFormat {
			dto["DeliveryUrl"] = fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/0/Stream.%s", it.ID, src.ID, st.Index, subtitleExt(st.Codec))
			dto["IsExternalUrl"] = false
		}
	}
	return dto
}

type playbackDecision struct {
	directPlayURL   string
	subtitleMethods map[int]string // stream index -> External | Embed | Encode | Hls
	subtitleIndex   int
	audioIndex      int
}

func jfType(t model.ItemType) string {
	switch t {
	case model.TypeMovie:
		return "Movie"
	case model.TypeSeries:
		return "Series"
	case model.TypeSeason:
		return "Season"
	case model.TypeEpisode:
		return "Episode"
	}
	return "Folder"
}

func mediaType(t model.ItemType) string {
	if t == model.TypeMovie || t == model.TypeEpisode {
		return "Video"
	}
	return "Unknown"
}

func jfStreamType(t model.StreamType) string {
	switch t {
	case model.StreamVideo:
		return "Video"
	case model.StreamAudio:
		return "Audio"
	case model.StreamSubtitle:
		return "Subtitle"
	}
	return "Data"
}

// jfCodec maps ffprobe codec names to the names Jellyfin clients expect.
func jfCodec(c string) string {
	switch c {
	case "subrip":
		return "srt"
	case "hdmv_pgs_subtitle":
		return "PGSSUB"
	case "dvd_subtitle":
		return "DVDSUB"
	case "dvb_subtitle":
		return "DVBSUB"
	}
	return c
}

func subtitleExt(codec string) string {
	switch codec {
	case "ass", "ssa":
		return "ass"
	case "subrip", "srt":
		return "srt"
	case "webvtt", "vtt":
		return "vtt"
	}
	return "txt"
}

func providerIDs(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		switch k {
		case "tmdb":
			out["Tmdb"] = v
		case "tvdb":
			out["Tvdb"] = v
		case "imdb":
			out["Imdb"] = v
		case "anidb":
			out["AniDB"] = v
		case "anilist":
			out["AniList"] = v
		case "mal":
			out["MyAnimeList"] = v
		default:
			out[strings.Title(k)] = v
		}
	}
	return out
}

func attachmentCodec(a model.Attachment) string {
	switch {
	case strings.Contains(a.Mime, "opentype"), strings.HasSuffix(strings.ToLower(a.Filename), ".otf"):
		return "otf"
	case strings.Contains(a.Mime, "truetype"), strings.HasSuffix(strings.ToLower(a.Filename), ".ttf"):
		return "ttf"
	}
	return ""
}

func defaultStream(src *model.MediaSource, t model.StreamType) any {
	for _, st := range src.Streams {
		if st.Type == t && st.IsDefault {
			return st.Index
		}
	}
	for _, st := range src.Streams {
		if st.Type == t {
			return st.Index
		}
	}
	return nil
}

func displayTitle(st model.MediaStream) string {
	var parts []string
	if st.Language != "" {
		parts = append(parts, st.Language)
	}
	if st.Title != "" {
		parts = append(parts, st.Title)
	}
	switch st.Type {
	case model.StreamVideo:
		parts = append(parts, fmt.Sprintf("%dp", st.Height), strings.ToUpper(st.Codec))
		if st.BitDepth > 8 {
			parts = append(parts, fmt.Sprintf("%d-bit", st.BitDepth))
		}
		if st.RangeType != "" && st.RangeType != model.RangeSDR {
			parts = append(parts, string(st.RangeType))
		}
	case model.StreamAudio:
		parts = append(parts, strings.ToUpper(st.Codec), st.ChannelLayout)
	case model.StreamSubtitle:
		parts = append(parts, strings.ToUpper(jfCodec(st.Codec)))
		if st.IsForced {
			parts = append(parts, "Forced")
		}
		if st.IsExternal {
			parts = append(parts, "External")
		}
	}
	return strings.Join(parts, " - ")
}

func aspect(w, h int) string {
	if w == 0 || h == 0 {
		return ""
	}
	r := float64(w) / float64(h)
	switch {
	case r > 1.75 && r < 1.80:
		return "16:9"
	case r > 1.32 && r < 1.35:
		return "4:3"
	case r > 2.35 && r < 2.45:
		return "2.40:1"
	}
	return fmt.Sprintf("%d:%d", w, h)
}

func videoRange(r model.RangeType) string {
	if r == "" {
		return "Unknown"
	}
	if r == model.RangeSDR {
		return "SDR"
	}
	return "HDR"
}

// videoRangeType maps Roxy's range to Jellyfin's VideoRangeType enum names, which the
// Kotlin/TypeScript SDKs parse strictly.
func videoRangeType(r model.RangeType) string {
	switch r {
	case model.RangeSDR:
		return "SDR"
	case model.RangeHDR10:
		return "HDR10"
	case model.RangeHDR10Plus:
		return "HDR10Plus"
	case model.RangeHLG:
		return "HLG"
	case model.RangeDV:
		return "DOVI"
	}
	return "Unknown"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
