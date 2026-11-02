package segments

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// AniSkip fills intro/outro gaps from api.aniskip.com: crowd-submitted opening and ending
// times for anime episodes, keyed by MyAnimeList id and episode number (the source
// Aniyomi and mpv's ani-skip script use). It ranks below the file's own chapters and the
// fingerprint detector, which both measure the actual file; it covers what they miss (a
// season with one episode on disk, an OP that changes every week).
const (
	AniSkipOrigin = "aniskip"
	// A submission is only used when it was made on a file this close in length: times from
	// a differently cut release (another intro card, a longer preview) would be off.
	aniskipLengthTolSec = 5.0
	aniskipBase         = "https://api.aniskip.com/v2"
	aniskipMinGap       = 250 * time.Millisecond // be polite: one request at a time, spaced
)

type AniSkip struct {
	client *http.Client
	base   string

	mu   sync.Mutex
	last time.Time
}

func NewAniSkip() *AniSkip {
	return &AniSkip{client: &http.Client{Timeout: 10 * time.Second}, base: aniskipBase}
}

type aniskipResponse struct {
	Found   bool            `json:"found"`
	Results []aniskipResult `json:"results"`
}

type aniskipResult struct {
	Interval struct {
		StartTime float64 `json:"startTime"`
		EndTime   float64 `json:"endTime"`
	} `json:"interval"`
	SkipType      string  `json:"skipType"` // op | ed | mixed-op | mixed-ed | recap
	EpisodeLength float64 `json:"episodeLength"`
}

// Lookup returns the segments AniSkip knows for episode [episode] of the MAL entry
// [malID], fitted to a file [durationSec] long; nil when it has none that fit.
func (a *AniSkip) Lookup(ctx context.Context, malID string, episode int, durationSec float64) ([]model.MediaSegment, error) {
	a.mu.Lock()
	if wait := aniskipMinGap - time.Since(a.last); wait > 0 {
		time.Sleep(wait)
	}
	a.last = time.Now()
	a.mu.Unlock()

	q := url.Values{}
	for _, t := range []string{"op", "ed", "mixed-op", "mixed-ed", "recap"} {
		q.Add("types[]", t)
	}
	// With the length, AniSkip answers with the submission closest to it per type.
	q.Set("episodeLength", fmt.Sprintf("%.0f", durationSec))
	u := fmt.Sprintf("%s/skip-times/%s/%d?%s", a.base, url.PathEscape(malID), episode, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RoxyServer (+https://github.com/drewfoos/roxy-tv)")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // nothing submitted for this episode
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aniskip: %s", resp.Status)
	}
	var r aniskipResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if !r.Found {
		return nil, nil
	}
	return aniskipSegments(r.Results, durationSec), nil
}

// aniskipSegments turns AniSkip results into Roxy segments for a file [dur] seconds long:
// only submissions made on a file of the same length, only plausible lengths, an intro
// starting in the first seconds starts at 0, an outro ending near the end ends at the end,
// and a short stretch after the outro is the next-episode preview (as the detector does).
func aniskipSegments(results []aniskipResult, dur float64) []model.MediaSegment {
	var out []model.MediaSegment
	have := map[string]bool{}
	for _, r := range results {
		if r.EpisodeLength <= 0 || math.Abs(r.EpisodeLength-dur) > aniskipLengthTolSec {
			continue
		}
		st, en := r.Interval.StartTime, r.Interval.EndTime
		if st < 0 || en <= st || en > dur+1 {
			continue
		}
		var typ string
		switch r.SkipType {
		case "op", "mixed-op":
			typ = "intro"
			if en-st < MinIntroSec || en-st > MaxIntroSec {
				continue
			}
			if st <= StartSnapSec {
				st = 0
			}
		case "ed", "mixed-ed":
			typ = "outro"
			if en-st < MinOutroSec || en-st > MaxOutroSec {
				continue
			}
			if dur-en <= StartSnapSec {
				en = dur
			}
		case "recap":
			typ = "recap"
		default:
			continue
		}
		if have[typ] {
			continue
		}
		have[typ] = true
		out = append(out, model.MediaSegment{Type: typ, StartMs: ms(st), EndMs: ms(math.Min(en, dur)), Origin: AniSkipOrigin})
		if typ == "outro" {
			if rest := dur - en; rest >= MinPreviewSec && rest <= MaxPreviewSec && !have["preview"] {
				have["preview"] = true
				out = append(out, model.MediaSegment{Type: "preview", StartMs: ms(en), EndMs: ms(dur), Origin: AniSkipOrigin})
			}
		}
	}
	return out
}
