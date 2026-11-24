package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

// musicWorld is the whole router over a music folder of stand-in audio files: a stand-in
// ffprobe answers with the JSON kept beside each file ("x.mp3.probe") and a stand-in
// ffmpeg hands out its picture ("x.mp3.pic"). Users: alice (basic) and an admin.
type musicWorld struct {
	t        *testing.T
	h        http.Handler
	st       *store.Store
	root     string
	outside  string // a folder that is not a music folder
	aliceTok string
	adminTok string
	apiKey   string
}

func newMusicWorld(t *testing.T) *musicWorld {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	data, root, outside := filepath.Join(base, "config"), filepath.Join(base, "Music"), filepath.Join(base, "elsewhere")
	for _, d := range []string{data, root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	safefs.Init(data)
	probe, mpeg := filepath.Join(base, "ffprobe"), filepath.Join(base, "ffmpeg")
	if err := os.WriteFile(probe, []byte("#!/bin/sh\nfor a; do f=\"$a\"; done\n[ -f \"$f.probe\" ] || exit 1\ncat \"$f.probe\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mpeg, []byte("#!/bin/sh\nwhile [ $# -gt 0 ]; do [ \"$1\" = \"-i\" ] && f=\"$2\"; shift; done\n[ -f \"$f.pic\" ] || exit 1\ncat \"$f.pic\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(data, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{DataDir: data}
	authSvc := auth.New(st)
	w := &musicWorld{t: t, st: st, root: root, outside: outside}
	newUser := func(id, role string) string {
		u := &model.User{ID: id, Name: id, Role: role, IsAdmin: role == model.RoleAdmin}
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		_, tok, err := authSvc.IssueToken(ctx, u, auth.ClientInfo{Client: "test", DeviceID: id + "-phone"})
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	w.aliceTok, w.adminTok = newUser("alice", model.RoleBasic), newUser("admin", model.RoleAdmin)
	key, err := authSvc.CreateAPIKey(ctx, "sonarr")
	if err != nil {
		t.Fatal(err)
	}
	w.apiKey = key.Token
	ff := &ffmpeg.FFmpeg{FFmpeg: mpeg, FFprobe: probe}
	w.h = NewRouter(cfg, st, library.New(st, nil, cfg), ff, authSvc, nil, trickplay.New(st, ff, cfg), nil)
	return w
}

// song writes a stand-in audio file with its tags; pic, when given, is its cover.
func (w *musicWorld) song(rel string, pic []byte, kv ...string) string {
	w.t.Helper()
	path := filepath.Join(w.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("the bytes of "+rel), 0o644); err != nil {
		w.t.Fatal(err)
	}
	tags := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		tags[kv[i]] = kv[i+1]
	}
	streams := []any{map[string]any{"index": 0, "codec_name": "mp3", "codec_type": "audio", "sample_rate": "44100", "channels": 2}}
	if pic != nil {
		streams = append(streams, map[string]any{"index": 1, "codec_name": "png", "codec_type": "video", "disposition": map[string]int{"attached_pic": 1}})
		if err := os.WriteFile(path+".pic", pic, 0o644); err != nil {
			w.t.Fatal(err)
		}
	}
	b, _ := json.Marshal(map[string]any{"format": map[string]any{"format_name": "mp3", "duration": "200", "bit_rate": "320000", "tags": tags}, "streams": streams})
	if err := os.WriteFile(path+".probe", b, 0o644); err != nil {
		w.t.Fatal(err)
	}
	return path
}

func (w *musicWorld) do(method, path, tok string, body any, set ...func(*http.Request)) *httptest.ResponseRecorder {
	w.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, rd)
	r.RemoteAddr = "192.168.1.50:5000"
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	for _, f := range set {
		f(r)
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	return rec
}

// get reads a JSON answer, failing the test unless the status is want.
func (w *musicWorld) get(path, tok string, want int, out any) {
	w.t.Helper()
	rec := w.do(http.MethodGet, path, tok, nil)
	if rec.Code != want {
		w.t.Fatalf("GET %s = %d, want %d: %s", path, rec.Code, want, rec.Body.String())
	}
	if out != nil {
		// Emptied first: decoding into a list that was used before leaves the old values of
		// whatever the new answer does not mention.
		v := reflect.ValueOf(out).Elem()
		v.Set(reflect.Zero(v.Type()))
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			w.t.Fatalf("GET %s: %v in %s", path, err, rec.Body.String())
		}
	}
}

// scanned waits for the scan that an added folder (or POST /v1/music/scan) starts.
func (w *musicWorld) scanned() {
	w.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var st struct {
			Scanning   bool       `json:"scanning"`
			FinishedAt *time.Time `json:"finishedAt"`
		}
		w.get("/v1/music/scan", w.adminTok, 200, &st)
		if !st.Scanning && st.FinishedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.t.Fatal("the scan did not finish")
}

type musicList struct {
	Total   int                 `json:"total"`
	Tracks  []model.MusicTrack  `json:"tracks"`
	Albums  []model.MusicAlbum  `json:"albums"`
	Artists []model.MusicArtist `json:"artists"`
}

func trackTitles(ts []model.MusicTrack) string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Title
	}
	return strings.Join(out, " | ")
}

func smallPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 400, 400))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The whole of it through the router: a folder is added, scanned, listed, searched,
// played, favourited and removed.
func TestMusicAPI(t *testing.T) {
	w := newMusicWorld(t)

	// A server with no music says so, and lists nothing.
	var home struct {
		Counts        store.MusicCounts `json:"counts"`
		RecentlyAdded []struct {
			Album *model.MusicAlbum `json:"album"`
			Track *model.MusicTrack `json:"track"`
		} `json:"recentlyAdded"`
		MostPlayed []model.MusicTrack `json:"mostPlayed"`
	}
	w.get("/v1/music", w.aliceTok, 200, &home)
	if home.Counts.Tracks != 0 || home.RecentlyAdded != nil {
		t.Fatalf("an empty server: %+v", home)
	}

	pic := smallPNG(t)
	w.song("Joost/Kleinkunst/01 - Intro.mp3", pic, "title", "Intro", "artist", "Joost", "album", "Kleinkunst", "track", "1")
	second := w.song("Joost/Kleinkunst/02 - Wachtmuziek.mp3", pic, "title", "Wachtmuziek", "artist", "Joost, Ski Aggu", "album", "Kleinkunst", "track", "2")
	w.song("The Beatles/Yesterday.mp3", nil, "title", "Yesterday", "artist", "The Beatles", "lyrics", "Yesterday\nAll my troubles seemed so far away")
	if err := os.WriteFile(strings.TrimSuffix(second, ".mp3")+".lrc", []byte("[00:10.00]Eerste regel\n[00:20.50]Tweede regel"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Only an admin adds a folder, and only a real folder that is not already read.
	if rec := w.do("POST", "/v1/music/folders", w.aliceTok, map[string]string{"path": w.root}); rec.Code != 403 {
		t.Fatalf("a basic user added a music folder: %d", rec.Code)
	}
	for _, bad := range []string{"", "relative/path", filepath.Join(w.root, "nope"), filepath.Join(w.root, "The Beatles", "Yesterday.mp3")} {
		if rec := w.do("POST", "/v1/music/folders", w.adminTok, map[string]string{"path": bad}); rec.Code != 400 {
			t.Errorf("folder %q accepted: %d", bad, rec.Code)
		}
	}
	rec := w.do("POST", "/v1/music/folders", w.adminTok, map[string]string{"path": w.root})
	if rec.Code != 201 {
		t.Fatalf("adding the folder: %d %s", rec.Code, rec.Body.String())
	}
	var folder model.MusicFolder
	json.Unmarshal(rec.Body.Bytes(), &folder)
	if folder.Name != "Music" || folder.ID == 0 {
		t.Errorf("folder: %+v", folder)
	}
	for _, again := range []string{w.root, filepath.Join(w.root, "Joost"), filepath.Dir(w.root)} {
		if rec := w.do("POST", "/v1/music/folders", w.adminTok, map[string]string{"path": again}); rec.Code != 400 {
			t.Errorf("folder %q (the same, inside, or around one) accepted: %d", again, rec.Code)
		}
	}
	w.scanned()

	w.get("/v1/music", w.aliceTok, 200, &home)
	if home.Counts.Tracks != 3 || home.Counts.Albums != 1 || home.Counts.Artists != 2 || len(home.MostPlayed) != 0 {
		t.Fatalf("after the scan: %+v", home)
	}
	// Recently added: the album once (not a tile for each of its songs), the lone song by itself.
	var kinds []string
	for _, e := range home.RecentlyAdded {
		switch {
		case e.Album != nil && e.Track == nil:
			kinds = append(kinds, "album:"+e.Album.Title)
		case e.Track != nil && e.Album == nil:
			kinds = append(kinds, "song:"+e.Track.Title)
		default:
			kinds = append(kinds, "?")
		}
	}
	sort.Strings(kinds)
	if strings.Join(kinds, " | ") != "album:Kleinkunst | song:Yesterday" {
		t.Errorf("recently added: %v", kinds)
	}

	// Lists: by title, in pages, by album order.
	var list musicList
	w.get("/v1/music/tracks?limit=2", w.aliceTok, 200, &list)
	if list.Total != 3 || trackTitles(list.Tracks) != "Intro | Wachtmuziek" {
		t.Errorf("first page: %d, %s", list.Total, trackTitles(list.Tracks))
	}
	w.get("/v1/music/tracks?limit=2&offset=2", w.aliceTok, 200, &list)
	if trackTitles(list.Tracks) != "Yesterday" {
		t.Errorf("second page: %s", trackTitles(list.Tracks))
	}
	var artists musicList
	w.get("/v1/music/artists", w.aliceTok, 200, &artists)
	if len(artists.Artists) != 2 || artists.Artists[0].Name != "The Beatles" || artists.Artists[1].Name != "Joost" {
		t.Fatalf("artists: %+v", artists.Artists)
	}
	joost := artists.Artists[1]
	var page struct {
		Artist model.MusicArtist  `json:"artist"`
		Album  model.MusicAlbum   `json:"album"`
		Tracks []model.MusicTrack `json:"tracks"`
		Albums []model.MusicAlbum `json:"albums"`
	}
	w.get("/v1/music/artists/"+joost.ID, w.aliceTok, 200, &page)
	if page.Artist.TrackCount != 2 || len(page.Tracks) != 2 || len(page.Albums) != 1 || page.Albums[0].Title != "Kleinkunst" {
		t.Fatalf("artist page: %+v", page)
	}
	album := page.Albums[0]
	w.get("/v1/music/albums/"+album.ID, w.aliceTok, 200, &page)
	if page.Album.TrackCount != 2 || trackTitles(page.Tracks) != "Intro | Wachtmuziek" {
		t.Errorf("album page: %+v", page)
	}
	wacht := page.Tracks[1]
	w.get("/v1/music/artists/nobody", w.aliceTok, 404, nil)
	w.get("/v1/music/albums/nothing", w.aliceTok, 404, nil)
	w.get("/v1/music/tracks/nothing", w.aliceTok, 404, nil)

	// Search: any word, any case, guests too.
	var found musicList
	w.get("/v1/music/search?q=AGGU", w.aliceTok, 200, &found)
	if trackTitles(found.Tracks) != "Wachtmuziek" || len(found.Artists) != 1 || found.Artists[0].Name != "Ski Aggu" {
		t.Errorf("search for a guest: %+v", found)
	}
	w.get("/v1/music/search?q=kleinkunst", w.aliceTok, 200, &found)
	if len(found.Albums) != 1 || len(found.Tracks) != 2 {
		t.Errorf("search for an album: %+v", found)
	}
	w.get("/v1/music/search?q=", w.aliceTok, 200, &found)
	if len(found.Tracks)+len(found.Albums)+len(found.Artists) != 0 {
		t.Errorf("an empty search found things: %+v", found)
	}

	// The file itself, whole and in part.
	stream := "/v1/music/tracks/" + wacht.ID + "/stream"
	rec = w.do("GET", stream, w.aliceTok, nil)
	if rec.Code != 200 || rec.Body.String() != "the bytes of Joost/Kleinkunst/02 - Wachtmuziek.mp3" || rec.Header().Get("Content-Type") != "audio/mpeg" || rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("stream: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	rec = w.do("GET", stream, w.aliceTok, nil, func(r *http.Request) { r.Header.Set("Range", "bytes=4-8") })
	if rec.Code != 206 || rec.Body.String() != "bytes" {
		t.Errorf("a range: %d %q", rec.Code, rec.Body.String())
	}
	if rec = w.do("HEAD", stream, w.aliceTok, nil); rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") == "" {
		t.Errorf("HEAD: %d, %d bytes, length %q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Length"))
	}
	// Nobody else: no credential, an integration key, an account token in the address.
	for name, r := range map[string]*httptest.ResponseRecorder{
		"no credential": w.do("GET", stream, "", nil),
		"an API key":    w.do("GET", stream, w.apiKey, nil),
		"a URL token":   w.do("GET", stream+"?api_key="+w.aliceTok, "", nil),
	} {
		if r.Code != 401 {
			t.Errorf("%s opened a song: %d", name, r.Code)
		}
	}
	// The browser's own requests (an <audio> element sends no header) carry the cookie.
	rec = w.do("GET", stream, "", nil, func(r *http.Request) { r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: w.aliceTok}) })
	if rec.Code != 200 {
		t.Errorf("the session cookie did not open a song: %d", rec.Code)
	}

	// The cover: as it is, and smaller; its address is for good.
	if wacht.Art == "" || album.Art != wacht.Art {
		t.Fatalf("covers: track %q album %q", wacht.Art, album.Art)
	}
	rec = w.do("GET", "/v1/music/art/"+wacht.Art, w.aliceTok, nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), pic) || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("cover: %d %s %s", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
	}
	rec = w.do("GET", "/v1/music/art/"+wacht.Art+"?w=100", w.aliceTok, nil)
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes())); rec.Code != 200 || err != nil || cfg.Width != 160 {
		t.Errorf("a smaller cover: %d %v %s %+v", rec.Code, err, format, cfg)
	}
	// A cover whose header reads as a picture and whose body does not decode cannot be
	// made smaller: it is sent as it is, not refused.
	broken := append([]byte(nil), pic...)
	broken[len(broken)-20] ^= 0xFF // inside the image data: its checksum no longer fits
	brokenHash := "ab" + strings.Repeat("0", 38)
	artDir := filepath.Join(filepath.Dir(w.root), "config", "cache", "music", "art", "ab")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artDir, brokenHash), broken, 0o644); err != nil {
		t.Fatal(err)
	}
	rec = w.do("GET", "/v1/music/art/"+brokenHash+"?w=100", w.aliceTok, nil)
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), broken) {
		t.Errorf("a cover that cannot be resized: %d, %d bytes (want it as it is, %d)", rec.Code, rec.Body.Len(), len(broken))
	}
	for _, bad := range []string{"nope", strings.Repeat("a", 40), "..%2F..%2Froxy.db", strings.Repeat("A", 40)} {
		if rec := w.do("GET", "/v1/music/art/"+bad, w.aliceTok, nil); rec.Code != 404 {
			t.Errorf("picture %q: %d", bad, rec.Code)
		}
	}
	if rec := w.do("GET", "/v1/music/art/"+wacht.Art, "", nil); rec.Code != 401 {
		t.Errorf("a cover without signing in: %d", rec.Code)
	}

	// Lyrics: the file beside the song (synced), the words in the file, none.
	var lyr struct {
		Source string `json:"source"`
		Synced bool   `json:"synced"`
		Lines  []struct {
			Ms   int64  `json:"ms"`
			Text string `json:"text"`
		} `json:"lines"`
	}
	w.get("/v1/music/tracks/"+wacht.ID+"/lyrics", w.aliceTok, 200, &lyr)
	if lyr.Source != "lrc" || !lyr.Synced || len(lyr.Lines) != 2 || lyr.Lines[1].Ms != 20500 || lyr.Lines[1].Text != "Tweede regel" {
		t.Errorf("synced lyrics: %+v", lyr)
	}
	if wacht.Lyrics != "lrc" {
		t.Errorf("the track says its lyrics are %q", wacht.Lyrics)
	}
	w.get("/v1/music/tracks?q=yesterday", w.aliceTok, 200, &list)
	yesterday := list.Tracks[0]
	w.get("/v1/music/tracks/"+yesterday.ID+"/lyrics", w.aliceTok, 200, &lyr)
	if lyr.Source != "embedded" || lyr.Synced || len(lyr.Lines) != 2 {
		t.Errorf("embedded lyrics: %+v", lyr)
	}
	w.get("/v1/music/tracks?q=intro", w.aliceTok, 200, &list)
	w.get("/v1/music/tracks/"+list.Tracks[0].ID+"/lyrics", w.aliceTok, 404, nil)

	// Plays and favourites belong to a profile.
	played := "/v1/music/tracks/" + wacht.ID + "/played"
	for i := 0; i < 2; i++ { // the second report within moments is the same listen
		rec = w.do("POST", played, w.aliceTok, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"playCount":1`) {
			t.Fatalf("play %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if rec = w.do("POST", "/v1/music/favorites/track/"+wacht.ID, w.aliceTok, nil); rec.Code != 204 {
		t.Fatalf("favourite: %d", rec.Code)
	}
	if rec = w.do("POST", "/v1/music/favorites/artist/"+joost.ID, w.aliceTok, nil); rec.Code != 204 {
		t.Fatalf("favourite artist: %d", rec.Code)
	}
	for _, bad := range []string{"/v1/music/favorites/track/nothing", "/v1/music/favorites/playlist/" + wacht.ID} {
		if rec = w.do("POST", bad, w.aliceTok, nil); rec.Code != 404 {
			t.Errorf("POST %s = %d", bad, rec.Code)
		}
	}
	w.get("/v1/music/tracks?favorite=1", w.aliceTok, 200, &list)
	if trackTitles(list.Tracks) != "Wachtmuziek" || list.Tracks[0].PlayCount != 1 || list.Tracks[0].LastPlayedAt == nil {
		t.Errorf("alice's favourites: %+v", list.Tracks)
	}
	w.get("/v1/music/artists?favorite=1", w.aliceTok, 200, &artists)
	if len(artists.Artists) != 1 || !artists.Artists[0].Favorite {
		t.Errorf("alice's favourite artists: %+v", artists.Artists)
	}
	w.get("/v1/music", w.aliceTok, 200, &home)
	if len(home.MostPlayed) != 1 || home.MostPlayed[0].ID != wacht.ID {
		t.Errorf("most played: %s", trackTitles(home.MostPlayed))
	}
	w.get("/v1/music/tracks?favorite=1", w.adminTok, 200, &list)
	if len(list.Tracks) != 0 {
		t.Errorf("the admin sees alice's favourites: %+v", list.Tracks)
	}
	w.get("/v1/music/tracks/"+wacht.ID, w.adminTok, 200, &wacht)
	if wacht.PlayCount != 0 || wacht.Favorite {
		t.Errorf("the admin sees alice's plays: %+v", wacht)
	}
	if rec = w.do("DELETE", "/v1/music/favorites/track/"+wacht.ID, w.aliceTok, nil); rec.Code != 204 {
		t.Fatalf("unfavourite: %d", rec.Code)
	}
	w.get("/v1/music/tracks?favorite=1", w.aliceTok, 200, &list)
	if len(list.Tracks) != 0 {
		t.Errorf("still a favourite: %+v", list.Tracks)
	}

	// A scan on request picks up a new file; only an admin may ask.
	w.song("The Beatles/Help.mp3", nil, "title", "Help!", "artist", "The Beatles")
	if rec = w.do("POST", "/v1/music/scan", w.aliceTok, nil); rec.Code != 403 {
		t.Errorf("a basic user started a scan: %d", rec.Code)
	}
	if rec = w.do("GET", "/v1/music/folders", w.aliceTok, nil); rec.Code != 403 {
		t.Errorf("a basic user listed the folders: %d", rec.Code)
	}
	time.Sleep(30 * time.Millisecond) // the status of the first scan must not be read as this one's
	if rec = w.do("POST", "/v1/music/scan", w.adminTok, map[string]bool{"force": false}); rec.Code != 202 {
		t.Fatalf("scan: %d", rec.Code)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		w.get("/v1/music/tracks", w.aliceTok, 200, &list)
		if list.Total == 4 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if list.Total != 4 {
		t.Fatalf("the new file was not picked up: %d tracks", list.Total)
	}
	w.scanned()

	// Removing the folder forgets the songs; the files stay.
	if rec = w.do("DELETE", fmt.Sprintf("/v1/music/folders/%d", folder.ID), w.aliceTok, nil); rec.Code != 403 {
		t.Errorf("a basic user removed the folder: %d", rec.Code)
	}
	if rec = w.do("DELETE", fmt.Sprintf("/v1/music/folders/%d", folder.ID), w.adminTok, nil); rec.Code != 204 {
		t.Fatalf("removing the folder: %d", rec.Code)
	}
	w.get("/v1/music", w.aliceTok, 200, &home)
	if home.Counts.Tracks != 0 {
		t.Errorf("songs left: %+v", home.Counts)
	}
	w.get(stream, w.aliceTok, 404, nil)
	if _, err := os.Stat(second); err != nil {
		t.Errorf("the file itself must stay: %v", err)
	}
}

// A song's row that names a file outside the music folders (through a link, or a row
// made by hand) is not served, and neither are lyrics found through a link.
func TestMusicNeverLeavesItsFolders(t *testing.T) {
	w := newMusicWorld(t)
	secret := filepath.Join(w.outside, "roxy.db")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	song := w.song("A/song.mp3", nil, "title", "Song", "artist", "A")
	// The lyrics "file" beside the song is a link to something else entirely.
	if err := os.Symlink(secret, strings.TrimSuffix(song, ".mp3")+".lrc"); err != nil {
		t.Fatal(err)
	}
	if rec := w.do("POST", "/v1/music/folders", w.adminTok, map[string]string{"path": w.root}); rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	w.scanned()
	var list musicList
	w.get("/v1/music/tracks", w.aliceTok, 200, &list)
	if len(list.Tracks) != 1 {
		t.Fatalf("tracks: %+v", list.Tracks)
	}
	id := list.Tracks[0].ID
	if rec := w.do("GET", "/v1/music/tracks/"+id+"/lyrics", w.aliceTok, nil); rec.Code != 403 || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("lyrics through a link out of the folder: %d %s", rec.Code, rec.Body.String())
	}
	// The row is pointed at the outside file.
	if _, err := w.st.DB().Exec(`UPDATE music_tracks SET path = ? WHERE id = ?`, secret, id); err != nil {
		t.Fatal(err)
	}
	if rec := w.do("GET", "/v1/music/tracks/"+id+"/stream", w.aliceTok, nil); rec.Code != 403 || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("a file outside the music folders was served: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAudioMimeAndPlayWindow(t *testing.T) {
	for c, want := range map[string]string{"mp3": "audio/mpeg", "flac": "audio/flac", "m4a": "audio/mp4", "opus": "audio/ogg", "wav": "audio/wav", "xyz": "application/octet-stream"} {
		if got := audioMime(c, ""); got != want {
			t.Errorf("audioMime(%q) = %q, want %q", c, got, want)
		}
	}
	if d := samePlayWindow(200_000); d != 30*time.Second {
		t.Errorf("a long song: %v", d)
	}
	if d := samePlayWindow(20_000); d != 10*time.Second {
		t.Errorf("a 20-second clip: %v", d)
	}
	if d := samePlayWindow(0); d != 30*time.Second {
		t.Errorf("unknown length: %v", d)
	}
}
