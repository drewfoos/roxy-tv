package music

import (
	"context"
	"fmt"
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

// TestLiveMusicFolder reads a real music folder (ROXY_LIVE_MUSIC=<folder>, read only)
// with the real ffprobe and ffmpeg into a throwaway database and prints what it made of
// it: run it before shipping a change to how tags are read or artists are told apart, and
// look at the artists it lists. ROXY_LIVE_MUSIC_LIST=1 prints every artist.
//
//	ROXY_LIVE_MUSIC=/mnt/media/Music go test ./internal/music -run LiveMusicFolder -v
func TestLiveMusicFolder(t *testing.T) {
	root := os.Getenv("ROXY_LIVE_MUSIC")
	if root == "" {
		t.Skip("set ROXY_LIVE_MUSIC to a music folder")
	}
	ff, err := ffmpeg.New(os.Getenv("ROXY_FFMPEG_PATH"), os.Getenv("ROXY_FFPROBE_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	safefs.Init(data)
	st, err := store.Open(filepath.Join(data, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	folder := model.MusicFolder{Name: "Music", Root: root}
	if err := st.CreateMusicFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	svc := New(st, ff, filepath.Join(data, "cache", "music", "art"))
	start := time.Now()
	if err := svc.Scan(ctx, false); err != nil {
		t.Fatal(err)
	}
	first := time.Since(start)
	res := svc.Status().Folders[0]
	start = time.Now()
	if err := svc.Scan(ctx, false); err != nil {
		t.Fatal(err)
	}
	again := svc.Status().Folders[0]
	t.Logf("first scan %v: %+v", first.Round(time.Millisecond), res)
	t.Logf("second scan %v: %+v", time.Since(start).Round(time.Millisecond), again)
	if again.Added+again.Updated+again.Moved+again.Missing+again.Removed != 0 {
		t.Errorf("a second scan of an unchanged folder changed things: %+v", again)
	}
	counts, _ := st.MusicCounts(ctx)
	leads, _, _ := st.ListMusicArtists(ctx, "", store.MusicArtistQuery{})
	all, _, _ := st.ListMusicArtists(ctx, "", store.MusicArtistQuery{Guests: true})
	albums, _, _ := st.ListMusicAlbums(ctx, "", store.MusicAlbumQuery{})
	tracks, _, _ := st.ListMusicTracks(ctx, "", store.MusicTrackQuery{})
	t.Logf("%d songs, %d albums listed, %d artists who lead a song (%d with guests)", counts.Tracks, len(albums), len(leads), len(all))
	withArt, withLyrics, lossless, noArtistArt := 0, 0, 0, 0
	for _, tr := range tracks {
		if tr.Art != "" {
			withArt++
		}
		if tr.Lyrics != "" {
			withLyrics++
		}
		if tr.Lossless {
			lossless++
		}
	}
	for _, a := range leads {
		if a.Art == "" {
			noArtistArt++
		}
	}
	t.Logf("%d songs with a picture, %d with lyrics, %d lossless; %d leading artists without a picture", withArt, withLyrics, lossless, noArtistArt)
	var odd []string
	for _, a := range all {
		if strings.Contains(a.Name, ", ") || strings.Contains(a.Name, " & ") || strings.Contains(a.Name, ";") || strings.Contains(strings.ToLower(a.Name), " feat") {
			odd = append(odd, a.Name)
		}
	}
	sort.Strings(odd)
	t.Logf("%d artist names that still hold a separator (real names, or joins not understood):\n  %s", len(odd), strings.Join(odd, "\n  "))
	sort.Slice(albums, func(i, j int) bool { return albums[i].TrackCount > albums[j].TrackCount })
	for i := 0; i < len(albums) && i < 8; i++ {
		t.Logf("album: %s — %s (%d songs)", albums[i].Artist, albums[i].Title, albums[i].TrackCount)
	}
	if os.Getenv("ROXY_LIVE_MUSIC_LIST") != "" {
		var names []string
		for _, a := range all {
			names = append(names, fmt.Sprintf("%s (%d)", a.Name, a.TrackCount))
		}
		t.Logf("artists:\n  %s", strings.Join(names, "\n  "))
	}
}
