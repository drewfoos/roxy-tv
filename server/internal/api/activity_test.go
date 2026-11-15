package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/activity"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/transcode"
)

// activityFixture: streamFixture's library with real tracks on its file, a signed-in
// device, and "Now playing" switched on.
func activityFixture(t *testing.T) (s *Server, admin, viewer *model.Principal) {
	t.Helper()
	s, _, _ = streamFixture(t)
	ctx := context.Background()
	s.board = activity.New()
	s.transcoder = transcode.NewDetector("/nonexistent/ffmpeg", "software", "")
	u := &model.User{ID: "u1", Name: "drew", IsAdmin: true}
	if err := s.store.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	scope, err := s.store.DefaultProfileID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateDevice(ctx, &model.Device{ID: "dev1", UserID: u.ID, Name: "Chrome on Windows", Client: "roxy-web", ClientVersion: "0.1.0", TokenHash: "h1", LastIP: "192.168.1.20"}); err != nil {
		t.Fatal(err)
	}
	src, err := s.store.GetSource(ctx, "src1")
	if err != nil || src == nil {
		t.Fatalf("source: %v", err)
	}
	src.DurationMs = 1_400_000
	src.Streams = append(src.Streams,
		model.MediaStream{SourceID: "src1", Index: 0, Type: model.StreamVideo, Codec: "hevc", Width: 1920, Height: 1080, BitDepth: 10, RangeType: "SDR"},
		model.MediaStream{SourceID: "src1", Index: 1, Type: model.StreamAudio, Codec: "aac", Channels: 2, Language: "eng", IsDefault: true},
		model.MediaStream{SourceID: "src1", Index: 2, Type: model.StreamAudio, Codec: "eac3", Channels: 6, Language: "jpn"},
	)
	if err := s.store.ReplaceSource(ctx, src, "{}"); err != nil {
		t.Fatal(err)
	}
	admin = &model.Principal{UserID: u.ID, User: u, ScopeID: scope, DeviceID: "dev1", IsAdmin: true}
	v := &model.User{ID: "u2", Name: "guest"}
	if err := s.store.CreateUser(ctx, v); err != nil {
		t.Fatal(err)
	}
	viewer = &model.Principal{UserID: v.ID, User: v, DeviceID: "dev2"}
	return s, admin, viewer
}

type activeAnswer struct {
	Sessions   []ActiveSession `json:"sessions"`
	Converting int             `json:"converting"`
}

func active(t *testing.T, s *Server, p *model.Principal) activeAnswer {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/active", nil)
	s.activeSessions(rec, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out activeAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func report(t *testing.T, s *Server, p *model.Principal, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/me/progress", strings.NewReader(body))
	s.progress(rec, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("%s: status %d (%s)", body, rec.Code, rec.Body.String())
	}
}

func TestNowPlayingIsAdminOnly(t *testing.T) {
	s, _, viewer := activityFixture(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/active", nil)
	s.activeSessions(rec, r.WithContext(auth.WithPrincipal(r.Context(), viewer)))
	if rec.Code != 403 {
		t.Fatalf("status %d", rec.Code)
	}
}

// The browser plays with a media token (no device login on the request), and reports its
// position with its login: the two must meet as one session, sent "direct".
func TestNowPlayingDirectPlay(t *testing.T) {
	s, admin, _ := activityFixture(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/stream/src1", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "src1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
	media := &model.Principal{UserID: admin.UserID, User: admin.User, MediaSource: "src1", MediaDevice: "dev1"}
	s.stream(rec, r.WithContext(auth.WithPrincipal(r.Context(), media)))
	if rec.Code != 200 {
		t.Fatalf("stream: status %d", rec.Code)
	}
	report(t, s, admin, `{"itemId":"item1","positionMs":80000,"sourceId":"src1","durationMs":1400000,"subtitleIndex":5,"state":"playing"}`)

	got := active(t, s, admin)
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions: %+v", got.Sessions)
	}
	se := got.Sessions[0]
	if se.State != "playing" || se.PositionMs != 80000 || se.DurationMs != 1400000 || se.Item == nil || se.Item.Title != "Show" {
		t.Errorf("session: %+v", se)
	}
	if se.User.Name != "drew" || se.Profile.Name == "" || se.Device.Name != "Chrome on Windows" || se.Device.Network != "home" || se.Device.Version != "0.1.0" {
		t.Errorf("who and where: %+v %+v %+v", se.User, se.Profile, se.Device)
	}
	p := se.Play
	if p.Method != activity.Direct || p.Reason != "" || p.Video == nil || p.Video.Codec != "hevc" || p.Video.Height != 1080 || p.Video.To != nil {
		t.Errorf("play: %+v video %+v", p, p.Video)
	}
	if p.Audio == nil || p.Audio.Codec != "aac" || p.Audio.To != nil {
		t.Errorf("the default audio track, untouched: %+v", p.Audio)
	}
	if p.Subtitle == nil || p.Subtitle.Codec != "subrip" || !p.Subtitle.External {
		t.Errorf("subtitle: %+v", p.Subtitle)
	}
	if strings.Contains(mustJSON(t, got), "192.168.1.20") {
		t.Error("the device's address must not be in the answer")
	}

	// Paused, then closed.
	report(t, s, admin, `{"itemId":"item1","positionMs":90000,"sourceId":"src1","state":"paused","subtitleIndex":-1}`)
	if se := active(t, s, admin).Sessions[0]; se.State != "paused" || !se.Play.SubtitlesOff || se.Play.Subtitle != nil {
		t.Errorf("paused with subtitles off: %+v %+v", se.State, se.Play)
	}
	// Playing again, then closed, both where it last reported: nothing new to save.
	state := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/v1/me/playing", strings.NewReader(body))
		s.playingState(rec, r.WithContext(auth.WithPrincipal(r.Context(), admin)))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: status %d (%s)", body, rec.Code, rec.Body.String())
		}
	}
	state(`{"itemId":"item1","state":"playing"}`)
	if se := active(t, s, admin).Sessions[0]; se.State != "playing" || se.PositionMs != 90000 {
		t.Errorf("playing again: %+v", se)
	}
	state(`{"itemId":"item1","state":"stopped"}`)
	if got := active(t, s, admin); len(got.Sessions) != 0 {
		t.Errorf("after the player closed where it last reported: %+v", got.Sessions)
	}
	report(t, s, admin, `{"itemId":"item1","positionMs":95000,"sourceId":"src1","state":"playing"}`)
	report(t, s, admin, `{"itemId":"item1","positionMs":95000,"sourceId":"src1","state":"stopped"}`)
	if got := active(t, s, admin); len(got.Sessions) != 0 {
		t.Errorf("after the player closed: %+v", got.Sessions)
	}
}

func TestNowPlayingRemuxAndTranscode(t *testing.T) {
	s, admin, _ := activityFixture(t)
	report(t, s, admin, `{"itemId":"item1","positionMs":80000,"sourceId":"src1","audioIndex":2}`)
	done := s.board.Opened("dev1", activity.Stream{Method: activity.Remux, ItemID: "item1", SourceID: "src1", AudioIndex: 2, Web: true})
	p := active(t, s, admin).Sessions[0].Play
	if p.Method != activity.Remux || !p.Sending || p.Video.To != nil || p.Audio == nil || p.Audio.Codec != "eac3" || p.Audio.To == nil || p.Audio.To.Codec != "aac" || p.Audio.To.Channels != 6 || !strings.Contains(p.Reason, "E-AC3") {
		t.Errorf("remux: %+v audio %+v", p, p.Audio)
	}
	done()

	done = s.board.Opened("dev1", activity.Stream{Method: activity.Transcode, ItemID: "item1", SourceID: "src1", AudioIndex: -1, MaxHeight: 720, BitrateK: 4000, Encoder: "vaapi"})
	got := active(t, s, admin)
	p = got.Sessions[0].Play
	if got.Converting != 1 || p.Method != activity.Transcode || p.Video.To == nil || p.Video.To.Codec != "h264" || p.Video.To.Height != 720 || p.Video.To.BitrateK != 4000 || !p.Video.To.Hardware || !strings.Contains(p.Reason, "720p") {
		t.Errorf("transcode: %+v video.to %+v", p, p.Video.To)
	}
	// An app's transcode keeps E-AC3 as it is (every app decodes it).
	if p.Audio.To != nil {
		t.Errorf("audio in an app's transcode: %+v", p.Audio.To)
	}
	done()
	if got := active(t, s, admin); got.Converting != 0 || got.Sessions[0].Play.Sending {
		t.Errorf("after the stream ended: %+v", got)
	}
}

// A report a phone made offline and sends hours later is history, not something playing.
func TestNowPlayingIgnoresReplayedReports(t *testing.T) {
	s, admin, _ := activityFixture(t)
	report(t, s, admin, `{"itemId":"item1","positionMs":80000,"at":1700000000000}`)
	if got := active(t, s, admin); len(got.Sessions) != 0 {
		t.Errorf("got %+v", got.Sessions)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
