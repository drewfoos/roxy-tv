package compat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
)

// Jellyfin DeviceProfile, the parts the decision needs.
type deviceProfile struct {
	MaxStreamingBitrate int64 `json:"MaxStreamingBitrate"`
	DirectPlayProfiles  []struct {
		Container  string `json:"Container"`
		Type       string `json:"Type"`
		VideoCodec string `json:"VideoCodec"`
		AudioCodec string `json:"AudioCodec"`
	} `json:"DirectPlayProfiles"`
	SubtitleProfiles []struct {
		Format string `json:"Format"`
		Method string `json:"Method"`
	} `json:"SubtitleProfiles"`
	CodecProfiles []struct {
		Type       string `json:"Type"`
		Codec      string `json:"Codec"`
		Conditions []struct {
			Condition  string `json:"Condition"`
			Property   string `json:"Property"`
			Value      string `json:"Value"`
			IsRequired bool   `json:"IsRequired"`
		} `json:"Conditions"`
	} `json:"CodecProfiles"`
}

type playbackInfoRequest struct {
	DeviceProfile                       *deviceProfile `json:"DeviceProfile"`
	MaxStreamingBitrate                 int64          `json:"MaxStreamingBitrate"`
	StartTimeTicks                      int64          `json:"StartTimeTicks"`
	AudioStreamIndex                    *int           `json:"AudioStreamIndex"`
	SubtitleStreamIndex                 *int           `json:"SubtitleStreamIndex"`
	MediaSourceId                       string         `json:"MediaSourceId"`
	EnableDirectPlay                    *bool          `json:"EnableDirectPlay"`
	EnableDirectStream                  *bool          `json:"EnableDirectStream"`
	EnableTranscoding                   *bool          `json:"EnableTranscoding"`
	AlwaysBurnInSubtitleWhenTranscoding bool           `json:"AlwaysBurnInSubtitleWhenTranscoding"`
}

// playbackInfo is the Phase 2 decision: direct play only. It still honours the
// subtitle profile so a client that declares ASS as External gets DeliveryMethod
// External + a DeliveryUrl, and one that does not gets Encode (which, with no
// transcoder yet, means "render it yourself or go without").
func (h *Handler) playbackInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	it, err := h.store.GetItem(ctx, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if it == nil {
		writeError(w, 404, "Item not found")
		return
	}
	var req playbackInfoRequest
	if r.Method == http.MethodPost && r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	q := r.URL.Query()
	if v := q.Get("MediaSourceId"); v != "" {
		req.MediaSourceId = v
	}
	if v := q.Get("AudioStreamIndex"); v != "" {
		n, _ := strconv.Atoi(v)
		req.AudioStreamIndex = &n
	}
	if v := q.Get("SubtitleStreamIndex"); v != "" {
		n, _ := strconv.Atoi(v)
		req.SubtitleStreamIndex = &n
	}

	sources, err := h.store.SourcesForItem(ctx, it.ID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if req.MediaSourceId != "" {
		for i := range sources {
			if sources[i].ID == req.MediaSourceId {
				sources = sources[i : i+1]
				break
			}
		}
	}
	playSession := strings.ReplaceAll(uuid.NewString(), "-", "")
	token, _ := auth.TokenFromRequest(r)

	out := make([]map[string]any, 0, len(sources))
	for i := range sources {
		src := &sources[i]
		pb := &playbackDecision{
			subtitleMethods: map[int]string{},
			subtitleIndex:   -1,
			audioIndex:      -1,
		}
		if req.AudioStreamIndex != nil {
			pb.audioIndex = *req.AudioStreamIndex
		}
		if req.SubtitleStreamIndex != nil {
			pb.subtitleIndex = *req.SubtitleStreamIndex
		}
		for _, st := range src.Streams {
			if st.Type != model.StreamSubtitle {
				continue
			}
			pb.subtitleMethods[st.Index] = subtitleMethod(req.DeviceProfile, st)
		}
		pb.directPlayURL = fmt.Sprintf("/Videos/%s/stream.%s?Static=true&mediaSourceId=%s&PlaySessionId=%s", it.ID, src.Container, src.ID, playSession)
		if token != "" {
			pb.directPlayURL += "&api_key=" + token
		}
		out = append(out, h.mediaSourceDTO(it, src, pb))
	}
	writeJSON(w, 200, map[string]any{
		"MediaSources":  out,
		"PlaySessionId": playSession,
	})
}

// subtitleMethod applies Jellyfin's rule: a text track whose format the client lists as
// External (or Embed) is delivered that way; otherwise it would be burned in (Encode).
// ASS/SSA are never converted (SupportsSubtitleConversionTo returns false for them).
func subtitleMethod(p *deviceProfile, st model.MediaStream) string {
	format := subtitleExt(st.Codec)
	if !st.IsTextFormat {
		format = strings.ToLower(jfCodec(st.Codec))
	}
	if p == nil {
		if st.IsTextFormat {
			return "External"
		}
		return "Embed"
	}
	for _, sp := range p.SubtitleProfiles {
		if !strings.EqualFold(sp.Format, format) && !(format == "ass" && strings.EqualFold(sp.Format, "ssa")) {
			continue
		}
		switch strings.ToLower(sp.Method) {
		case "external":
			if st.IsTextFormat {
				return "External"
			}
		case "embed":
			return "Embed"
		}
	}
	// SRT can be converted to VTT for HLS later; ASS/SSA cannot: burn-in.
	return "Encode"
}
