package music

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

// testLib is a music folder on disk, a database and a scanner. The "audio files" are
// small text files; a stand-in ffprobe answers with the JSON kept beside each
// ("song.mp3.probe") and a stand-in ffmpeg hands out its picture ("song.mp3.pic"), so the
// whole scan runs as it does for real without decoding anything.
type testLib struct {
	t      *testing.T
	ctx    context.Context
	st     *store.Store
	svc    *Service
	root   string
	folder model.MusicFolder
	scope  string // a profile
	serial int
}

func newTestLib(t *testing.T) *testLib {
	t.Helper()
	base := t.TempDir()
	data := filepath.Join(base, "data")
	root := filepath.Join(base, "Music")
	for _, d := range []string{data, root} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	safefs.Init(data)
	probe := filepath.Join(base, "ffprobe")
	mpeg := filepath.Join(base, "ffmpeg")
	// ffprobe: the file is the last argument. ffmpeg: the file follows -i.
	if err := os.WriteFile(probe, []byte("#!/bin/sh\nfor a; do f=\"$a\"; done\n[ -f \"$f.probe\" ] || { echo \"$f: Invalid data found when processing input\" >&2; exit 1; }\ncat \"$f.probe\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mpeg, []byte("#!/bin/sh\nwhile [ $# -gt 0 ]; do [ \"$1\" = \"-i\" ] && f=\"$2\"; shift; done\n[ -f \"$f.pic\" ] || exit 1\ncat \"$f.pic\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(data, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	l := &testLib{t: t, ctx: ctx, st: st, root: root, scope: "profile-1"}
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO users (id, name, password_hash, created_at) VALUES ('u', 'u', 'x', ?)`, store.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO profiles (id, user_id, name, is_default, created_at) VALUES (?, 'u', 'P', 1, ?)`, l.scope, store.Now()); err != nil {
		t.Fatal(err)
	}
	l.folder = model.MusicFolder{Name: "Music", Root: root}
	if err := st.CreateMusicFolder(ctx, &l.folder); err != nil {
		t.Fatal(err)
	}
	l.svc = New(st, &ffmpeg.FFmpeg{FFmpeg: mpeg, FFprobe: probe}, filepath.Join(data, "cache", "music", "art"))
	return l
}

// song is the tags and encoding of one stand-in audio file.
type song struct {
	tags     map[string]string
	codec    string // "mp3" by default
	rate     int
	bits     int
	seconds  float64
	picture  []byte // nil: no cover in the file
	noProbe  bool   // ffprobe fails on it
	contents string // what the file holds ("" = something unique)
}

func tags(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// put writes a stand-in audio file at rel (below the music folder).
func (l *testLib) put(rel string, s song) string {
	l.t.Helper()
	path := filepath.Join(l.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		l.t.Fatal(err)
	}
	if s.contents == "" {
		l.serial++
		s.contents = fmt.Sprintf("audio %d %s", l.serial, strings.Repeat("x", l.serial))
	}
	if err := os.WriteFile(path, []byte(s.contents), 0o644); err != nil {
		l.t.Fatal(err)
	}
	os.Remove(path + ".probe")
	os.Remove(path + ".pic")
	if s.noProbe {
		return path
	}
	if s.codec == "" {
		s.codec = "mp3"
	}
	if s.rate == 0 {
		s.rate = 44100
	}
	if s.seconds == 0 {
		s.seconds = 180
	}
	audio := map[string]any{"index": 0, "codec_name": s.codec, "codec_type": "audio", "sample_rate": fmt.Sprint(s.rate), "channels": 2}
	if s.bits > 0 {
		audio["bits_per_raw_sample"] = fmt.Sprint(s.bits)
	}
	streams := []any{audio}
	if s.picture != nil {
		streams = append(streams, map[string]any{"index": 1, "codec_name": "png", "codec_type": "video", "disposition": map[string]int{"attached_pic": 1}})
		if err := os.WriteFile(path+".pic", s.picture, 0o644); err != nil {
			l.t.Fatal(err)
		}
	}
	b, _ := json.Marshal(map[string]any{
		"format":  map[string]any{"format_name": s.codec, "duration": fmt.Sprint(s.seconds), "bit_rate": "320000", "tags": s.tags},
		"streams": streams,
	})
	if err := os.WriteFile(path+".probe", b, 0o644); err != nil {
		l.t.Fatal(err)
	}
	return path
}

// move renames a stand-in audio file with what belongs to it.
func (l *testLib) move(from, to string) {
	l.t.Helper()
	src, dst := filepath.Join(l.root, filepath.FromSlash(from)), filepath.Join(l.root, filepath.FromSlash(to))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		l.t.Fatal(err)
	}
	for _, ext := range []string{"", ".probe", ".pic"} {
		if _, err := os.Stat(src + ext); err == nil {
			if err := os.Rename(src+ext, dst+ext); err != nil {
				l.t.Fatal(err)
			}
		}
	}
}

func (l *testLib) remove(rel string) {
	l.t.Helper()
	p := filepath.Join(l.root, filepath.FromSlash(rel))
	if err := os.Remove(p); err != nil {
		l.t.Fatal(err)
	}
	os.Remove(p + ".probe")
	os.Remove(p + ".pic")
}

func (l *testLib) scan(force bool) FolderResult {
	l.t.Helper()
	if err := l.svc.Scan(l.ctx, force); err != nil {
		l.t.Fatalf("scan: %v", err)
	}
	st := l.svc.Status()
	if len(st.Folders) != 1 {
		l.t.Fatalf("scan reported %d folders, want 1: %+v", len(st.Folders), st)
	}
	return st.Folders[0]
}

func (l *testLib) tracks() []model.MusicTrack {
	l.t.Helper()
	ts, _, err := l.st.ListMusicTracks(l.ctx, l.scope, store.MusicTrackQuery{})
	if err != nil {
		l.t.Fatal(err)
	}
	return ts
}

func (l *testLib) track(title string) model.MusicTrack {
	l.t.Helper()
	for _, t := range l.tracks() {
		if t.Title == title {
			return t
		}
	}
	l.t.Fatalf("no track titled %q among %v", title, titles(l.tracks()))
	return model.MusicTrack{}
}

func titles(ts []model.MusicTrack) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Title
	}
	sort.Strings(out)
	return out
}

func testPicture(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	img.Set(0, 0, color.RGBA{shade, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func artistNames(as []model.MusicArtist) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.Name
	}
	return out
}

// A first scan: songs by their tags, the artists they credit, albums only where two
// songs of one are there, pictures and lyrics.
func TestScanReadsALibrary(t *testing.T) {
	l := newTestLib(t)
	cover := testPicture(t, 40)
	l.put("Joost/Kleinkunst/01 - Intro.mp3", song{tags: tags("title", "Intro", "artist", "Joost", "album", "Kleinkunst", "album_artist", "Joost", "track", "1", "date", "2022"), picture: cover})
	l.put("Joost/Kleinkunst/02 - Wachtmuziek.mp3", song{tags: tags("title", "Wachtmuziek", "artist", "Joost, Otto Waalkes & Ski Aggu", "album", "Kleinkunst", "album_artist", "Joost", "track", "2", "date", "2022"), picture: cover})
	l.put("Dua Lipa/Levitating (feat. DaBaby)/Levitating (feat. DaBaby).mp3", song{tags: tags("title", "Levitating (feat. DaBaby)", "artist", "Dua Lipa, DaBaby", "album", "Levitating (feat. DaBaby)", "track", "342")})
	l.put("The Beatles/Hi-Res/I'm Only Sleeping.flac", song{codec: "flac", rate: 96000, bits: 24, tags: tags("TITLE", "I'm Only Sleeping", "ARTIST", "The Beatles", "LYRICS", "When I wake up")})
	if err := os.WriteFile(filepath.Join(l.root, "Joost", "Kleinkunst", "01 - Intro.lrc"), []byte("[00:01.00]Hallo"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := l.scan(false)
	if res.Files != 4 || res.Added != 4 || res.Failed != 0 || res.LeftAlone != "" {
		t.Fatalf("first scan: %+v", res)
	}
	if got, want := titles(l.tracks()), []string{"I'm Only Sleeping", "Intro", "Levitating (feat. DaBaby)", "Wachtmuziek"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("tracks %v, want %v", got, want)
	}

	// The artists who lead a song, by name; the guests are artists too but not listed
	// among them.
	leads, _, err := l.st.ListMusicArtists(l.ctx, l.scope, store.MusicArtistQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if got := artistNames(leads); fmt.Sprint(got) != fmt.Sprint([]string{"The Beatles", "Dua Lipa", "Joost"}) {
		t.Errorf("artists who lead a song (in name order, articles aside): %v", got)
	}
	all, _, _ := l.st.ListMusicArtists(l.ctx, l.scope, store.MusicArtistQuery{Guests: true})
	if len(all) != 6 { // + DaBaby, Otto Waalkes, Ski Aggu
		t.Errorf("with guests: %v", artistNames(all))
	}

	// A song credits each artist, the leader first, and is found on each one's page.
	w := l.track("Wachtmuziek")
	if w.Artist != "Joost, Otto Waalkes & Ski Aggu" || len(w.Artists) != 3 || w.Artists[0].Name != "Joost" || w.Artists[2].Name != "Ski Aggu" {
		t.Errorf("credits: %q %v", w.Artist, w.Artists)
	}
	guest, _, _ := l.st.ListMusicTracks(l.ctx, l.scope, store.MusicTrackQuery{ArtistID: ArtistID("Ski Aggu")})
	if len(guest) != 1 || guest[0].Title != "Wachtmuziek" {
		t.Errorf("a guest's page lists: %v", titles(guest))
	}

	// Two songs of one album make an album; a lone song that names an album does not.
	albums, _, _ := l.st.ListMusicAlbums(l.ctx, l.scope, store.MusicAlbumQuery{})
	if len(albums) != 1 || albums[0].Title != "Kleinkunst" || albums[0].Artist != "Joost" || albums[0].TrackCount != 2 || albums[0].Year != 2022 || albums[0].DurationMs != 360000 {
		t.Errorf("albums: %+v", albums)
	}
	lev := l.track("Levitating (feat. DaBaby)")
	if lev.Album != "Levitating (feat. DaBaby)" || lev.AlbumID == "" || lev.Track != 0 {
		t.Errorf("the single still says what it is from, without the tool's track number: %+v", lev)
	}
	c, _ := l.st.MusicCounts(l.ctx)
	if c.Tracks != 4 || c.Albums != 1 || c.Artists != 3 {
		t.Errorf("counts: %+v", c)
	}

	// Pictures: the two album tracks share one, kept once; the album and its artist show it.
	intro := l.track("Intro")
	if intro.Art == "" || intro.Art != w.Art || !l.svc.Art().Has(intro.Art) {
		t.Errorf("cover: %q / %q", intro.Art, w.Art)
	}
	if albums[0].Art != intro.Art {
		t.Errorf("the album shows %q, its tracks %q", albums[0].Art, intro.Art)
	}
	if joost, _ := l.st.GetMusicArtist(l.ctx, l.scope, ArtistID("Joost")); joost == nil || joost.Art != intro.Art || joost.TrackCount != 2 || joost.AlbumCount != 1 {
		t.Errorf("artist: %+v", joost)
	}
	if lev.Art != "" {
		t.Errorf("a song with no picture got one: %q", lev.Art)
	}

	// Lyrics: a file beside the song, or the words the file carries.
	if intro.Lyrics != "lrc" {
		t.Errorf("lyrics beside the song: %q", intro.Lyrics)
	}
	beatles := l.track("I'm Only Sleeping")
	if beatles.Lyrics != "embedded" || !beatles.Lossless || beatles.BitDepth != 24 || beatles.SampleRate != 96000 {
		t.Errorf("the FLAC: %+v", beatles)
	}
	if got, _ := l.st.GetMusicTrack(l.ctx, l.scope, beatles.ID); got == nil || got.LyricsText != "When I wake up" {
		t.Errorf("embedded words: %+v", got)
	}
}

// A second pass over a library that has not changed reads no file; a changed file is
// read again and stays the same song.
func TestScanReadsOnlyWhatChanged(t *testing.T) {
	l := newTestLib(t)
	l.put("A/one.mp3", song{tags: tags("title", "One", "artist", "A")})
	p := l.put("A/two.mp3", song{tags: tags("title", "Two", "artist", "A")})
	l.scan(false)
	before := l.track("Two")

	// The stand-in ffprobe now fails for every file: an unchanged library never asks it.
	os.Rename(filepath.Join(filepath.Dir(l.root), "ffprobe"), filepath.Join(filepath.Dir(l.root), "ffprobe.off"))
	res := l.scan(false)
	if res.Added+res.Updated+res.Moved+res.Missing+res.Failed != 0 || l.svc.Status().Total != 0 {
		t.Fatalf("an unchanged library was read again: %+v", res)
	}
	os.Rename(filepath.Join(filepath.Dir(l.root), "ffprobe.off"), filepath.Join(filepath.Dir(l.root), "ffprobe"))

	// Retagged: same path, new contents.
	l.put("A/two.mp3", song{tags: tags("title", "Two (Remastered)", "artist", "A")})
	later := time.Now().Add(time.Hour)
	os.Chtimes(p, later, later)
	res = l.scan(false)
	if res.Updated != 1 || res.Added != 0 {
		t.Fatalf("a changed file: %+v", res)
	}
	after := l.track("Two (Remastered)")
	if after.ID != before.ID || !after.AddedAt.Equal(before.AddedAt) {
		t.Errorf("a retagged file became another song: %s / %s, added %v / %v", before.ID, after.ID, before.AddedAt, after.AddedAt)
	}
	if len(l.tracks()) != 2 {
		t.Errorf("tracks: %v", titles(l.tracks()))
	}

	// A forced scan reads everything.
	if res = l.scan(true); res.Updated != 2 {
		t.Errorf("forced: %+v", res)
	}
}

// plays and a favourite mark: what must follow a song wherever its file goes.
func (l *testLib) markHeard(id string, plays int) {
	l.t.Helper()
	for i := 0; i < plays; i++ {
		if err := l.st.RecordMusicPlay(l.ctx, l.scope, id, time.Now()); err != nil {
			l.t.Fatal(err)
		}
	}
	if err := l.st.SetMusicFavorite(l.ctx, l.scope, "track", id, true); err != nil {
		l.t.Fatal(err)
	}
}

// A file moved to another folder (or renamed) is the same song: same id, same plays,
// still a favourite, added when it first was.
func TestMovedFileIsTheSameSong(t *testing.T) {
	l := newTestLib(t)
	l.put("Joost/12 - Milf.mp3", song{tags: tags("title", "Milf", "artist", "Joost")})
	l.put("Joost/other.mp3", song{tags: tags("title", "Other", "artist", "Joost")})
	l.scan(false)
	before := l.track("Milf")
	l.markHeard(before.ID, 3)

	l.move("Joost/12 - Milf.mp3", "Joost/Scandinavian Boy/Joost - Scandinavian Boy - 12 - Milf.mp3")
	res := l.scan(false)
	if res.Moved != 1 || res.Added != 0 || res.Missing != 0 {
		t.Fatalf("after a move: %+v", res)
	}
	after := l.track("Milf")
	if after.ID != before.ID || after.PlayCount != 3 || !after.Favorite {
		t.Errorf("the moved song lost its history: %+v", after)
	}
	if !strings.Contains(after.Path, "Scandinavian Boy") {
		t.Errorf("path not followed: %s", after.Path)
	}
	if len(l.tracks()) != 2 {
		t.Errorf("tracks: %v", titles(l.tracks()))
	}
}

// An MP3 replaced by a FLAC of the same song (another file, another album tag, another
// length) is still that song.
func TestBetterCopyTakesOverTheSong(t *testing.T) {
	l := newTestLib(t)
	l.put("Dua Lipa/Levitating/Levitating.mp3", song{tags: tags("title", "Levitating", "artist", "Dua Lipa", "album", "Levitating"), seconds: 203})
	l.put("Dua Lipa/keep.mp3", song{tags: tags("title", "Keep", "artist", "Dua Lipa")})
	l.scan(false)
	before := l.track("Levitating")
	l.markHeard(before.ID, 5)

	l.remove("Dua Lipa/Levitating/Levitating.mp3")
	l.put("Dua Lipa/Future Nostalgia/05 - Levitating.flac", song{codec: "flac", bits: 16, tags: tags("TITLE", "Levitating", "ARTIST", "Dua Lipa", "ALBUM", "Future Nostalgia", "track", "5"), seconds: 204})
	res := l.scan(false)
	if res.Moved != 1 || res.Added != 0 || res.Missing != 0 {
		t.Fatalf("after the replacement: %+v", res)
	}
	after := l.track("Levitating")
	if after.ID != before.ID || after.PlayCount != 5 || !after.Favorite || after.Codec != "flac" || after.Album != "Future Nostalgia" {
		t.Errorf("the better copy is not the same song: %+v", after)
	}
}

// Both copies there for a while (the FLAC arrives, the MP3 is deleted days later): when
// the MP3 goes, what was noted about it is added to the copy that stays.
func TestCopyThatStaysInheritsTheHistory(t *testing.T) {
	l := newTestLib(t)
	l.put("A/song.mp3", song{tags: tags("title", "Song", "artist", "A")})
	l.put("A/hi/song.flac", song{codec: "flac", tags: tags("title", "Song", "artist", "A")})
	l.scan(false)
	var mp3, flac model.MusicTrack
	for _, tr := range l.tracks() {
		if tr.Codec == "flac" {
			flac = tr
		} else {
			mp3 = tr
		}
	}
	if mp3.ID == "" || flac.ID == "" || mp3.ID == flac.ID {
		t.Fatalf("two files of one song are two rows while both are there: %+v", l.tracks())
	}
	l.markHeard(mp3.ID, 4)
	if err := l.st.RecordMusicPlay(l.ctx, l.scope, flac.ID, time.Now()); err != nil {
		t.Fatal(err)
	}

	l.remove("A/song.mp3")
	res := l.scan(false)
	if res.Missing != 0 {
		t.Fatalf("the song is still there as a FLAC: %+v", res)
	}
	left := l.tracks()
	if len(left) != 1 || left[0].ID != flac.ID || left[0].PlayCount != 5 || !left[0].Favorite {
		t.Errorf("the copy that stayed: %+v", left)
	}
}

// A music folder with nothing in it, where there was music, is an unmounted drive (or
// the empty folder Docker makes in its place): nothing is touched.
func TestEmptyFolderChangesNothing(t *testing.T) {
	l := newTestLib(t)
	l.put("A/one.mp3", song{tags: tags("title", "One", "artist", "A")})
	l.put("A/two.mp3", song{tags: tags("title", "Two", "artist", "A")})
	l.scan(false)
	one := l.track("One")
	l.markHeard(one.ID, 2)

	if err := os.Rename(l.root, l.root+".unmounted"); err != nil {
		t.Fatal(err)
	}
	// The folder is not there at all.
	res := l.scan(false)
	if res.LeftAlone == "" || res.Missing != 0 || len(l.tracks()) != 2 {
		t.Fatalf("a missing folder: %+v, tracks %v", res, titles(l.tracks()))
	}
	// The folder is there and empty.
	if err := os.Mkdir(l.root, 0o755); err != nil {
		t.Fatal(err)
	}
	res = l.scan(true)
	if res.LeftAlone == "" || res.Missing != 0 || len(l.tracks()) != 2 {
		t.Fatalf("an empty folder: %+v, tracks %v", res, titles(l.tracks()))
	}
	// Mounted again: as before.
	os.Remove(l.root)
	if err := os.Rename(l.root+".unmounted", l.root); err != nil {
		t.Fatal(err)
	}
	res = l.scan(false)
	if got := l.track("One"); res.LeftAlone != "" || got.ID != one.ID || got.PlayCount != 2 {
		t.Errorf("after the drive is back: %+v, %+v", res, got)
	}
}

// One file gone among others: the song is hidden, and it is the same song when the file
// returns. Long gone, its row is dropped with what was noted about it.
func TestMissingFileIsHiddenAndComesBack(t *testing.T) {
	l := newTestLib(t)
	l.put("A/one.mp3", song{tags: tags("title", "One", "artist", "A"), contents: "the one"})
	l.put("A/two.mp3", song{tags: tags("title", "Two", "artist", "A", "album", "Solo")})
	l.put("B/three.mp3", song{tags: tags("title", "Three", "artist", "B")})
	l.scan(false)
	one := l.track("One")
	l.markHeard(one.ID, 2)

	l.remove("A/one.mp3")
	if res := l.scan(false); res.Missing != 1 {
		t.Fatalf("one file gone: %+v", res)
	}
	if got := titles(l.tracks()); fmt.Sprint(got) != "[Three Two]" {
		t.Errorf("listed: %v", got)
	}
	if tr, _ := l.st.GetMusicTrack(l.ctx, l.scope, one.ID); tr != nil {
		t.Error("a song whose file is gone can still be opened")
	}
	// A second pass does not count it again.
	if res := l.scan(false); res.Missing != 0 {
		t.Errorf("counted missing twice: %+v", res)
	}

	l.put("A/one.mp3", song{tags: tags("title", "One", "artist", "A"), contents: "the one"})
	if res := l.scan(false); res.Moved != 1 || res.Added != 0 {
		t.Fatalf("the file is back: %+v", res)
	}
	if got := l.track("One"); got.ID != one.ID || got.PlayCount != 2 || !got.Favorite {
		t.Errorf("back, but not the same song: %+v", got)
	}

	// Gone for longer than rows are kept: dropped, and the artist with no song left too.
	l.remove("B/three.mp3")
	l.scan(false)
	old := missingKept
	missingKept = -time.Hour
	defer func() { missingKept = old }()
	if res := l.scan(false); res.Removed != 1 {
		t.Fatalf("a row long missing: %+v", res)
	}
	if b, _ := l.st.GetMusicArtist(l.ctx, l.scope, ArtistID("B")); b != nil {
		t.Errorf("an artist with no song left is still there: %+v", b)
	}
	if a, _ := l.st.GetMusicArtist(l.ctx, l.scope, ArtistID("A")); a == nil || a.TrackCount != 2 {
		t.Errorf("artist A: %+v", a)
	}
}

// A file ffprobe cannot read is reported and skipped; the rest of the folder is read, and
// a song already known stays as it was.
func TestUnreadableFileIsReported(t *testing.T) {
	l := newTestLib(t)
	l.put("A/good.mp3", song{tags: tags("title", "Good", "artist", "A")})
	p := l.put("A/later-broken.mp3", song{tags: tags("title", "Was Fine", "artist", "A")})
	l.put("A/broken.mp3", song{noProbe: true})
	res := l.scan(false)
	if res.Added != 2 || res.Failed != 1 || len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "broken.mp3") {
		t.Fatalf("%+v", res)
	}
	// A file that was read once and cannot be read now is still there on disk: the song stays.
	l.put("A/later-broken.mp3", song{noProbe: true})
	later := time.Now().Add(time.Hour)
	os.Chtimes(p, later, later)
	res = l.scan(false)
	if res.Failed != 2 || res.Missing != 0 || len(l.tracks()) != 2 {
		t.Errorf("%+v, tracks %v", res, titles(l.tracks()))
	}
}

// Pictures kept in the folders: "cover.png" in an album's own folder is that album's
// (before the tracks' own), "artist.png" in an artist's folder is the artist's, and the
// artist is spelled as the folder is.
func TestFolderPictures(t *testing.T) {
	l := newTestLib(t)
	inFile, cover, face := testPicture(t, 10), testPicture(t, 120), testPicture(t, 250)
	l.put("Bring Me the Horizon/Sempiternal/01.mp3", song{tags: tags("title", "Can You Feel My Heart", "artist", "Bring Me The Horizon", "album", "Sempiternal", "track", "1"), picture: inFile})
	l.put("Bring Me the Horizon/Sempiternal/02.mp3", song{tags: tags("title", "The House of Wolves", "artist", "Bring Me The Horizon", "album", "Sempiternal", "track", "2"), picture: inFile})
	l.put("Bring Me the Horizon/loose.mp3", song{tags: tags("title", "Loose", "artist", "Bring Me The Horizon", "album", "Some Single")})
	for name, data := range map[string][]byte{
		"Bring Me the Horizon/Sempiternal/Cover.PNG": cover,
		"Bring Me the Horizon/artist.png":            face,
		"Bring Me the Horizon/folder.png":            cover, // in the artist's folder: not the loose single's cover
	} {
		if err := os.WriteFile(filepath.Join(l.root, filepath.FromSlash(name)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l.scan(false)
	albums, _, _ := l.st.ListMusicAlbums(l.ctx, l.scope, store.MusicAlbumQuery{Singles: true})
	var semp, single model.MusicAlbum
	for _, a := range albums {
		if a.Title == "Sempiternal" {
			semp = a
		} else {
			single = a
		}
	}
	first := l.track("Can You Feel My Heart")
	if semp.Art == "" || semp.Art == first.Art || !l.svc.Art().Has(semp.Art) {
		t.Errorf("the album shows %q; its tracks carry %q", semp.Art, first.Art)
	}
	if single.Art != "" {
		t.Errorf("a picture in the artist's folder became a single's cover: %q", single.Art)
	}
	a, _ := l.st.GetMusicArtist(l.ctx, l.scope, ArtistID("Bring Me the Horizon"))
	if a == nil || a.Name != "Bring Me the Horizon" {
		t.Fatalf("the artist is spelled as the folder is: %+v", a)
	}
	if a.Art == "" || a.Art == semp.Art || a.Art == first.Art {
		t.Errorf("the artist's own picture: %q (album %q, track %q)", a.Art, semp.Art, first.Art)
	}
	// A song without a picture of its own shows its album's.
	l.put("Bring Me the Horizon/Sempiternal/03.mp3", song{tags: tags("title", "Empire", "artist", "Bring Me The Horizon", "album", "Sempiternal", "track", "3")})
	l.scan(false)
	if got := l.track("Empire"); got.Art != semp.Art {
		t.Errorf("a track with no picture shows %q, its album %q", got.Art, semp.Art)
	}
}

// Folders that are not read: hidden ones, and any that holds an ignore file.
func TestScanSkipsWhatItIsToldTo(t *testing.T) {
	l := newTestLib(t)
	l.put("A/song.mp3", song{tags: tags("title", "Song", "artist", "A")})
	l.put(".trash/gone.mp3", song{tags: tags("title", "Trashed", "artist", "A")})
	l.put("A/incoming/half.mp3", song{tags: tags("title", "Half", "artist", "A")})
	l.put("A/incoming/deeper/half2.mp3", song{tags: tags("title", "Half 2", "artist", "A")})
	if err := os.WriteFile(filepath.Join(l.root, "A", "incoming", ".ignore"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.root, "A", "notes.txt"), []byte("not a song"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(l.root, "A", "song.mp3"), filepath.Join(l.root, "A", "link.mp3")); err != nil {
		t.Fatal(err)
	}
	res := l.scan(false)
	if got := titles(l.tracks()); res.Files != 1 || fmt.Sprint(got) != "[Song]" {
		t.Errorf("read %d files: %v", res.Files, got)
	}
}

// Songs are found by any word of their title, artist or album, accents or not.
func TestSearchAndOrder(t *testing.T) {
	l := newTestLib(t)
	l.put("Joost/a.mp3", song{tags: tags("title", "Fryslân", "artist", "Joost", "album", "Fryslân")})
	l.put("Joost/b.mp3", song{tags: tags("title", "Europapa", "artist", "Joost")})
	l.put("The Beatles/c.mp3", song{tags: tags("title", "Yesterday", "artist", "The Beatles", "album", "Help!")})
	l.scan(false)
	find := func(q string) []string {
		ts, _, err := l.st.ListMusicTracks(l.ctx, l.scope, store.MusicTrackQuery{Search: Fold(q)})
		if err != nil {
			t.Fatal(err)
		}
		return titles(ts)
	}
	for q, want := range map[string]string{"fryslan": "[Fryslân]", "JOOST": "[Europapa Fryslân]", "beatles help": "[Yesterday]", "joost help": "[]", "100%": "[]"} {
		if got := fmt.Sprint(find(q)); got != want {
			t.Errorf("search %q: %s, want %s", q, got, want)
		}
	}
	// By artist: "The Beatles" sorts under B, before Joost.
	ts, total, _ := l.st.ListMusicTracks(l.ctx, l.scope, store.MusicTrackQuery{Sort: "artist", Limit: 2})
	if total != 3 || len(ts) != 2 || ts[0].Title != "Yesterday" {
		t.Errorf("by artist, first two of %d: %v", total, titles(ts))
	}
	// Most played first; never played last.
	y := l.track("Yesterday")
	l.markHeard(y.ID, 2)
	ts, _, _ = l.st.ListMusicTracks(l.ctx, l.scope, store.MusicTrackQuery{Sort: "plays"})
	if ts[0].ID != y.ID {
		t.Errorf("by plays: %v", titles(ts))
	}
	ts, _, _ = l.st.ListMusicTracks(l.ctx, l.scope, store.MusicTrackQuery{Sort: "recent"})
	if len(ts) != 1 || ts[0].ID != y.ID {
		t.Errorf("recently played: %v", titles(ts))
	}
	ts, _, _ = l.st.ListMusicTracks(l.ctx, l.scope, store.MusicTrackQuery{Favorite: true})
	if len(ts) != 1 || ts[0].ID != y.ID {
		t.Errorf("favourites: %v", titles(ts))
	}
	// Another profile has heard nothing.
	ts, _, _ = l.st.ListMusicTracks(l.ctx, "someone-else", store.MusicTrackQuery{Favorite: true})
	if len(ts) != 0 {
		t.Errorf("another profile's favourites: %v", titles(ts))
	}
}

// Removing a music folder forgets its songs, albums and artists; the files stay.
func TestRemovingAFolder(t *testing.T) {
	l := newTestLib(t)
	p := l.put("A/Album/one.mp3", song{tags: tags("title", "One", "artist", "A", "album", "Album"), picture: testPicture(t, 77)})
	l.put("A/Album/two.mp3", song{tags: tags("title", "Two", "artist", "A", "album", "Album")})
	l.scan(false)
	l.markHeard(l.track("One").ID, 1)
	if err := l.st.DeleteMusicFolder(l.ctx, l.folder.ID); err != nil {
		t.Fatal(err)
	}
	c, _ := l.st.MusicCounts(l.ctx)
	if c.Tracks+c.Albums+c.Artists != 0 {
		t.Errorf("left after removing the folder: %+v", c)
	}
	var n int
	l.st.DB().QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists) + (SELECT COUNT(*) FROM music_albums) + (SELECT COUNT(*) FROM music_user_data)`).Scan(&n)
	if n != 0 {
		t.Errorf("%d rows left behind", n)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("the file itself must stay: %v", err)
	}
}

// ROXY_MUSIC_ROOTS: each configured root becomes a music folder once; one an admin
// removed in the app stays removed.
func TestEnsureFolders(t *testing.T) {
	l := newTestLib(t)
	other := t.TempDir()
	roots := []string{l.root, other, other + "/"}
	if err := EnsureFolders(l.ctx, l.st, roots); err != nil {
		t.Fatal(err)
	}
	folders, _ := l.st.MusicFolders(l.ctx)
	if len(folders) != 2 || folders[0].Root != l.root || folders[1].Root != other || folders[1].Name != filepath.Base(other) {
		t.Fatalf("folders: %+v", folders)
	}
	// Removed in the app (the handler notes it): the setting does not add it again.
	if err := l.st.DeleteMusicFolder(l.ctx, folders[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := l.st.SetRootRemoved(l.ctx, other, true); err != nil {
		t.Fatal(err)
	}
	if err := EnsureFolders(l.ctx, l.st, roots); err != nil {
		t.Fatal(err)
	}
	if folders, _ = l.st.MusicFolders(l.ctx); len(folders) != 1 {
		t.Fatalf("a removed folder came back: %+v", folders)
	}
}
