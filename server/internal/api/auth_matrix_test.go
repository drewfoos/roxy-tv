package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
	"github.com/roxy-media/roxy/server/internal/trickplay"
)

// authWorld is the whole router (its real auth middleware) over a small library: a film
// with a sidecar subtitle, a font, seek previews and a poster, the film's trailer (an
// extra), and a second film; users alice (basic) and admin, and the app's update files.
type authWorld struct {
	h          http.Handler
	st         *store.Store
	authSvc    *auth.Service
	alice      *model.User
	aliceTok   string
	aliceDev   *model.Device
	adminTok   string
	aliceProf  string // alice's default profile id
	adminProf  string // the admin's default profile id
	apiKey     string
	mediaPaths []string // every path that serves src1's media
}

func newAuthWorld(t *testing.T) *authWorld {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	data, media := filepath.Join(base, "config"), filepath.Join(base, "media")
	cfg := &config.Config{DataDir: data}
	film := filepath.Join(media, "Film (2020)")
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	video := filepath.Join(film, "Film (2020).mkv")
	write(video, "not really a video")
	write(filepath.Join(film, "Film (2020).en.srt"), "1\n00:00:01,000 --> 00:00:02,000\nhello\n")
	write(filepath.Join(film, "Film (2020)-trailer.mkv"), "trailer")
	write(filepath.Join(media, "Other (2021)", "Other (2021).mkv"), "other")
	font := filepath.Join(cfg.AttachmentsDir(), "src1", "Font.ttf")
	write(font, "\x00\x01\x00\x00font")
	write(filepath.Join(cfg.TrickplayDir(), "src1", "manifest.json"), `{"intervalMs":10000,"width":320,"height":180,"columns":10,"rows":10,"count":1}`)
	write(filepath.Join(cfg.TrickplayDir(), "src1", "sheet_001.jpg"), "\xff\xd8\xff jpeg")
	poster := filepath.Join(cfg.ImagesDir(), "item1-poster.jpg")
	write(poster, "\xff\xd8\xff jpeg")
	write(filepath.Join(data, "apps", "tv", "latest.json"), `{"tv":{"versionCode":1,"versionName":"1","file":"tv.apk","sha256":"x"}}`)
	write(filepath.Join(data, "apps", "tv", "tv.apk"), "apk")

	safefs.Init(data)
	safefs.SetMediaRoots([]string{media})
	t.Cleanup(func() { safefs.SetMediaRoots(nil) })
	st, err := store.Open(filepath.Join(data, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: media}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	item1 := "item1"
	for _, it := range []*model.Item{
		{ID: "item1", GUID: "g1", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"},
		{ID: "trailer1", GUID: "extra:t1", LibraryID: lib.ID, Type: model.TypeExtra, Title: "Trailer", ParentID: &item1, ExtraType: "trailer"},
		{ID: "item2", GUID: "g2", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Other"},
	} {
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for _, src := range []*model.MediaSource{
		{ID: "src1", ItemID: "item1", Name: "Film (2020).mkv", Container: "mkv", IsPrimary: true,
			Parts: []model.MediaPart{{ID: "p1", SourceID: "src1", Path: video}},
			Streams: []model.MediaStream{{SourceID: "src1", Index: 1000, Type: model.StreamSubtitle, Codec: "subrip", IsTextFormat: true,
				IsExternal: true, ExternalPath: filepath.Join(film, "Film (2020).en.srt")}},
			Attachments: []model.Attachment{{SourceID: "src1", Index: 3, Filename: "Font.ttf", Mime: "application/x-truetype-font", ExtractedPath: font}}},
		{ID: "srcT", ItemID: "trailer1", Name: "trailer", IsPrimary: true,
			Parts: []model.MediaPart{{ID: "pT", SourceID: "srcT", Path: filepath.Join(film, "Film (2020)-trailer.mkv")}}},
		{ID: "src2", ItemID: "item2", Name: "Other", IsPrimary: true,
			Parts: []model.MediaPart{{ID: "p2", SourceID: "src2", Path: filepath.Join(media, "Other (2021)", "Other (2021).mkv")}}},
	} {
		if err := st.ReplaceSource(ctx, src, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetItemImage(ctx, store.ItemImage{ItemID: "item1", Type: "poster", Path: poster, FetchedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	authSvc := auth.New(st)
	w := &authWorld{st: st, authSvc: authSvc}
	newUser := func(id, role string) (*model.User, *model.Device, string, string) {
		t.Helper()
		u := &model.User{ID: id, Name: id, Role: role, IsAdmin: role == model.RoleAdmin}
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		d, tok, err := authSvc.IssueToken(ctx, u, auth.ClientInfo{Client: "test", DeviceID: id + "-phone"})
		if err != nil {
			t.Fatal(err)
		}
		prof, err := st.DefaultProfileID(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return u, d, tok, prof
	}
	w.alice, w.aliceDev, w.aliceTok, w.aliceProf = newUser("alice", model.RoleBasic)
	_, _, w.adminTok, w.adminProf = newUser("admin", model.RoleAdmin)
	if _, err := st.DB().ExecContext(ctx, `UPDATE profiles SET avatar_path = ? WHERE id IN (?, ?)`, poster, w.aliceProf, w.adminProf); err != nil {
		t.Fatal(err)
	}
	key, err := authSvc.CreateAPIKey(ctx, "sonarr")
	if err != nil {
		t.Fatal(err)
	}
	w.apiKey = key.Token

	lb := library.New(st, nil, cfg)
	w.h = NewRouter(cfg, st, lb, &ffmpeg.FFmpeg{}, authSvc, nil, trickplay.New(st, &ffmpeg.FFmpeg{}, cfg), nil)
	w.mediaPaths = []string{
		"/v1/stream/src1",
		"/v1/sources/src1/subtitles/1000",
		"/v1/sources/src1/attachments/3",
		"/v1/sources/src1/trickplay",
		"/v1/sources/src1/trickplay/0",
		"/v1/sources/src1/remux",
		"/v1/sources/src1/transcode",
	}
	return w
}

// do sends a request through the whole router, with whatever credential set adds.
func (w *authWorld) do(method, path string, set func(r *http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = "192.168.1.50:5000"
	if set != nil {
		set(r)
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	return rec
}

func bearer(tok string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

// Every endpoint that serves media or what plays with it, and the items they belong to,
// refuses a request without a working credential: none, a made-up token, a signed-out
// device's, an idle-expired one's, a disabled user's, an integration API key (webhooks
// only), and an account token put in the URL. A signed-in user gets through (the status is
// whatever the endpoint says; never 401/403).
func TestMediaEndpointsRefuseMissingAndRevokedCredentials(t *testing.T) {
	ctx := context.Background()
	w := newAuthWorld(t)
	paths := append(append([]string{}, w.mediaPaths...),
		"/v1/sources/src1", "/v1/sources/src1/token", "/v1/stream/srcT", "/v1/stream/src2",
		"/v1/items", "/v1/items/item1", "/v1/items/item1/images/poster", "/v1/items/item1/extras",
		"/v1/items/item1/details", "/v1/home", "/v1/me/resume",
		"/v1/apps/tv/latest?package=tv", "/v1/apps/tv/download?package=tv",
		"/v1/me/profiles/"+w.aliceProf+"/avatar",
	)
	for _, p := range paths {
		rec := w.do(http.MethodGet, p, bearer(w.aliceTok))
		if rec.Code == 401 || rec.Code == 403 {
			t.Errorf("signed in: GET %s refused (%d %s)", p, rec.Code, rec.Body)
		}
		t.Logf("signed in: GET %-44s %d", p, rec.Code)
		// It needs a sign-in, so no shared cache may keep it and hand it to anyone.
		if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "public") {
			t.Errorf("signed in: GET %s is cacheable by shared caches (%q)", p, cc)
		}
	}
	if rec := w.do(http.MethodHead, "/v1/stream/src1", bearer(w.aliceTok)); rec.Code == 401 {
		t.Error("signed in: HEAD on the stream refused")
	}

	// A second sign-in for the cases that end it.
	newDevice := func(name string) (*model.Device, string) {
		t.Helper()
		d, tok, err := w.authSvc.IssueToken(ctx, w.alice, auth.ClientInfo{Client: "test", DeviceID: name})
		if err != nil {
			t.Fatal(err)
		}
		return d, tok
	}
	signedOut, signedOutTok := newDevice("signed-out")
	if err := w.st.DeleteDevice(ctx, signedOut.ID); err != nil {
		t.Fatal(err)
	}
	idle, idleTok := newDevice("idle")
	stale := time.Now().Add(-auth.DeviceIdleExpiry - time.Hour).UTC().Format(store.TimeFormat)
	if _, err := w.st.DB().ExecContext(ctx, `UPDATE devices SET last_seen = ? WHERE id = ?`, stale, idle.ID); err != nil {
		t.Fatal(err)
	}
	bob := &model.User{ID: "bob", Name: "bob", Role: model.RoleBasic}
	if err := w.st.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}
	_, bobTok, err := w.authSvc.IssueToken(ctx, bob, auth.ClientInfo{Client: "test", DeviceID: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	bob.IsDisabled = true
	if err := w.st.UpdateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}

	for name, set := range map[string]func(*http.Request){
		"no credential":          nil,
		"a made-up token":        bearer("not-a-token"),
		"a signed-out device":    bearer(signedOutTok),
		"an idle-expired device": bearer(idleTok),
		"a disabled user":        bearer(bobTok),
		"an integration API key": bearer(w.apiKey),
		"an API key header":      func(r *http.Request) { r.Header.Set("X-Api-Key", w.apiKey) },
		"an account token in a URL": func(r *http.Request) {
			q := r.URL.Query()
			q.Set("api_key", w.aliceTok)
			r.URL.RawQuery = q.Encode()
		},
		"a malformed media token": func(r *http.Request) { r.Header.Set(auth.MediaTokenHeader, "garbage.zz") },
	} {
		for _, p := range paths {
			if rec := w.do(http.MethodGet, p, set); rec.Code != 401 {
				t.Errorf("%s: GET %s = %d, want 401", name, p, rec.Code)
			}
		}
	}
}

// A media token opens one source's stream and what plays with it, for as long as its
// device is signed in and its user enabled, and nothing else: not another source, not
// the item, its images or extras, not the account API, and it cannot mint more tokens.
func TestMediaTokenScope(t *testing.T) {
	ctx := context.Background()
	w := newAuthWorld(t)
	tok, _, err := w.authSvc.NewMediaToken(ctx, "src1", w.alice.ID, w.aliceDev.ID)
	if err != nil {
		t.Fatal(err)
	}
	inURL := func(tok string) func(*http.Request) {
		return func(r *http.Request) {
			q := r.URL.Query()
			q.Set("mt", tok)
			r.URL.RawQuery = q.Encode()
		}
	}
	inHeader := func(tok string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set(auth.MediaTokenHeader, tok) }
	}
	for _, p := range w.mediaPaths {
		for how, set := range map[string]func(*http.Request){"?mt=": inURL(tok), "header": inHeader(tok)} {
			if rec := w.do(http.MethodGet, p, set); rec.Code == 401 || rec.Code == 403 {
				t.Errorf("media token (%s): GET %s refused (%d)", how, p, rec.Code)
			}
		}
	}
	for _, p := range []string{
		"/v1/stream/src2", "/v1/sources/src2/subtitles/1000", "/v1/sources/src2/trickplay/0", "/v1/stream/srcT",
		"/v1/sources/src1", "/v1/sources/src1/token", "/v1/items/item1", "/v1/items/item1/images/poster",
		"/v1/items/item1/extras", "/v1/items", "/v1/home", "/v1/auth/me", "/v1/me/profiles", "/v1/devices",
		"/v1/apps/tv/download", "/v1/users", "/v1/library/folders",
		"/v1/sources/src1/../../items/item1", "/v1/sources/src1%2F..%2F..%2Fitems%2Fitem1",
	} {
		if rec := w.do(http.MethodGet, p, inURL(tok)); rec.Code != 401 && rec.Code != 404 {
			t.Errorf("media token for src1: GET %s = %d, want 401 (or 404)", p, rec.Code)
		}
	}
	if rec := w.do(http.MethodPost, "/v1/me/progress", inURL(tok)); rec.Code != 401 {
		t.Errorf("media token: POST /v1/me/progress = %d, want 401", rec.Code)
	}
	if rec := w.do(http.MethodPost, "/v1/auth/cookie", inURL(tok)); rec.Code != 401 {
		t.Errorf("media token: POST /v1/auth/cookie = %d, want 401", rec.Code)
	}

	// Its user disabled: refused; enabled again: works again (the token itself is fine).
	w.alice.IsDisabled = true
	if err := w.st.UpdateUser(ctx, w.alice); err != nil {
		t.Fatal(err)
	}
	if rec := w.do(http.MethodGet, "/v1/stream/src1", inURL(tok)); rec.Code != 401 {
		t.Errorf("media token of a disabled user: %d, want 401", rec.Code)
	}
	w.alice.IsDisabled = false
	if err := w.st.UpdateUser(ctx, w.alice); err != nil {
		t.Fatal(err)
	}
	// Its device signed out: refused everywhere.
	if err := w.st.DeleteDevice(ctx, w.aliceDev.ID); err != nil {
		t.Fatal(err)
	}
	for _, p := range w.mediaPaths {
		if rec := w.do(http.MethodGet, p, inURL(tok)); rec.Code != 401 {
			t.Errorf("media token after its device signed out: GET %s = %d, want 401", p, rec.Code)
		}
	}
}

// The browser's session cookie: reads (GET/HEAD) only, so a page elsewhere cannot change
// anything through it.
func TestSessionCookieOnlyReads(t *testing.T) {
	w := newAuthWorld(t)
	cookie := func(r *http.Request) { r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: w.aliceTok}) }
	if rec := w.do(http.MethodGet, "/v1/stream/src1", cookie); rec.Code == 401 || rec.Code == 403 {
		t.Errorf("cookie: GET stream refused (%d)", rec.Code)
	}
	for _, p := range []string{"/v1/me/progress", "/v1/me/played/item1", "/v1/me/favorite/item1", "/v1/auth/logout", "/v1/sources/src1/token"} {
		method := http.MethodPost
		if strings.HasSuffix(p, "/token") {
			continue // a GET: covered above as a read
		}
		if rec := w.do(method, p, cookie); rec.Code != 401 {
			t.Errorf("cookie: %s %s = %d, want 401", method, p, rec.Code)
		}
	}
}

// A signed-in user reaches nothing outside their permissions: another user's profile
// (its avatar, its settings) is not found; a profile header naming another user's
// profile is not honoured; a basic user gets 403 from admin endpoints, including the
// ones that touch media (refreshing, identifying, replacing artwork, scanning, folders).
func TestUsersStayWithinTheirPermissions(t *testing.T) {
	ctx := context.Background()
	w := newAuthWorld(t)
	alice := bearer(w.aliceTok)
	if rec := w.do(http.MethodGet, "/v1/me/profiles/"+w.adminProf+"/avatar", alice); rec.Code != 404 {
		t.Errorf("another user's avatar: %d, want 404", rec.Code)
	}
	if rec := w.do(http.MethodPatch, "/v1/me/profiles/"+w.adminProf, alice); rec.Code != 404 {
		t.Errorf("another user's profile: PATCH = %d, want 404", rec.Code)
	}
	if rec := w.do(http.MethodDelete, "/v1/me/profiles/"+w.adminProf, alice); rec.Code != 404 {
		t.Errorf("another user's profile: DELETE = %d, want 404", rec.Code)
	}
	// Watch state written with another user's profile in the header lands on alice's own.
	r := httptest.NewRequest(http.MethodPost, "/v1/me/played/item1", nil)
	r.Header.Set("Authorization", "Bearer "+w.aliceTok)
	r.Header.Set("X-Roxy-Profile", w.adminProf)
	r.RemoteAddr = "192.168.1.50:5000"
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	if rec.Code != 200 && rec.Code != 204 {
		t.Fatalf("mark watched: %d %s", rec.Code, rec.Body)
	}
	if d, _ := w.st.GetUserItemData(ctx, w.adminProf, "g1"); d != nil && d.Played {
		t.Error("alice marked an item watched on the admin's profile")
	}
	if d, _ := w.st.GetUserItemData(ctx, w.aliceProf, "g1"); d == nil || !d.Played {
		t.Error("alice's mark did not land on her own profile")
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/v1/items/item1/refresh"}, {http.MethodGet, "/v1/items/item1/identify"},
		{http.MethodPost, "/v1/items/item1/identify"}, {http.MethodPut, "/v1/items/item1/metadata"},
		{http.MethodPut, "/v1/items/item1/images/poster"}, {http.MethodGet, "/v1/items/item1/images/poster/candidates"},
		{http.MethodPost, "/v1/libraries/scan"}, {http.MethodPost, "/v1/library/scan"},
		{http.MethodGet, "/v1/library/folders"}, {http.MethodGet, "/v1/library/browse"},
		{http.MethodGet, "/v1/library/manage"}, {http.MethodPost, "/v1/segments/detect"},
		{http.MethodGet, "/v1/users"}, {http.MethodGet, "/v1/auth/keys"}, {http.MethodGet, "/v1/settings/arr"},
		{http.MethodGet, "/v1/webhooks/events"}, {http.MethodPost, "/v1/webhooks/sonarr"},
	} {
		if rec := w.do(c.method, c.path, alice); rec.Code != 403 {
			t.Errorf("basic user: %s %s = %d, want 403", c.method, c.path, rec.Code)
		}
	}
	// The admin passes the same checks (not a blanket refusal).
	if rec := w.do(http.MethodGet, "/v1/library/folders", bearer(w.adminTok)); rec.Code != 200 {
		t.Errorf("admin: GET /v1/library/folders = %d", rec.Code)
	}
}
