package library

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

// TestLibraryLifecycle drives whole sample libraries through the real pipeline while Roxy
// runs: real (tiny) video files made by ffmpeg, the real ingest and prober, the folder
// watcher doing the rescans. Between steps it checks that every recorded file exists, that
// the database has no broken invariants (store.CheckIntegrity: no orphan items, no file
// twice, no watch history left unreachable), and that the watch history set early on is
// still where it belongs. Steps: first scan; watch history; a show folder renamed (new
// identity); episodes and a season added; a film moved out of a collection and a show out
// of a grouping folder; a season folder unreadable, then readable again; the whole library
// folder gone (an unmounted drive), then back; a file copied in while a scan runs;
// subtitles added, replaced and removed.
//
// Needs ffmpeg and ffprobe, and a non-root user (root reads unreadable folders): run it in
// a container, e.g.
//
//	docker run --rm -v "$PWD":/src -w /src golang:1.27-bookworm bash -c \
//	  'apt-get update -qq && apt-get install -y -qq ffmpeg >/dev/null &&
//	   useradd -m t && chmod -R a+rwX /src && su t -c "HOME=/home/t GOCACHE=/tmp/gc GOMODCACHE=/tmp/gm \
//	   go test ./internal/library -run Lifecycle -v"'
func TestLibraryLifecycle(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		skipUnlessRequired(t, "ffmpeg/ffprobe not installed")
	}
	if os.Geteuid() == 0 {
		skipUnlessRequired(t, "runs as an ordinary user (root reads unreadable folders)")
	}
	ff, err := ffmpeg.New("", "")
	if err != nil {
		skipUnlessRequired(t, err.Error())
	}
	oldQ, oldR := watchQuiet, watchRefresh
	watchQuiet, watchRefresh = 400*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() { watchQuiet, watchRefresh = oldQ, oldR })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	data := t.TempDir()
	safefs.Init(data)
	st, err := store.Open(filepath.Join(data, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	media := t.TempDir()
	anime := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: filepath.Join(media, "Anime")}
	movies := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: filepath.Join(media, "Movies")}
	for _, lib := range []*model.Library{anime, movies} {
		if err := os.MkdirAll(lib.Root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := st.CreateLibrary(ctx, lib); err != nil {
			t.Fatal(err)
		}
	}
	safefs.SetMediaRoots([]string{anime.Root, movies.Root})
	l := New(st, ff, &config.Config{DataDir: data})
	u := &model.User{ID: "u1", Name: "drew"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	scope, err := st.DefaultProfileID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Each video gets its own length, so each file has its own size (as real ones do).
	n := 0
	video := func(path string) {
		t.Helper()
		n++
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("ffmpeg", "-v", "error", "-y",
			"-f", "lavfi", "-i", fmt.Sprintf("testsrc=duration=%d:size=320x240:rate=10", 1+n%7),
			"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=%d", 200+n*10, 1+n%7),
			"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", "-f", "matroska", path).CombinedOutput()
		if err != nil {
			t.Fatalf("making %s: %v %s", path, err, out)
		}
	}
	sub := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("1\n00:00:00,500 --> 00:00:01,500\n"+text+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// settled (below) is called after every wait: a scan records the new paths and drops
	// the old ones before it carries watch history across and prunes, so what is asserted
	// next is only final once the scan is over.
	var settled func()
	wait := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			if cond() {
				settled()
				return
			}
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	item := func(path string) *model.Item {
		it, _ := st.ItemForPath(ctx, path)
		return it
	}
	recorded := func(path string) bool { return item(path) != nil }
	// settled waits until no scan runs and none is about to (the watcher waits watchQuiet
	// after a change): checks look at the library at rest, not halfway through a scan,
	// where records of vanished files are already gone and their items not yet pruned.
	settled = func() {
		t.Helper()
		for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); {
			if running, _, _ := l.ScanStatus(); !running {
				time.Sleep(3 * watchQuiet)
				if running, _, _ := l.ScanStatus(); !running {
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("scans never settled")
	}
	check := func(step string) {
		t.Helper()
		settled()
		probs, err := st.CheckIntegrity(ctx)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		if len(probs) > 0 {
			lost, _ := st.LostWatchData(ctx)
			t.Fatalf("%s: %v (lost: %s)", step, probs, lost)
		}
		for _, lib := range []*model.Library{anime, movies} {
			paths, err := st.ListPartPaths(ctx, lib.ID)
			if err != nil {
				t.Fatal(err)
			}
			for p := range paths {
				if _, err := os.Stat(p); err != nil && !strings.Contains(step, "unavailable") {
					t.Fatalf("%s: recorded file missing on disk: %s", step, p)
				}
			}
		}
	}
	userData := func(path string) *store.UserItemData {
		it := item(path)
		if it == nil {
			return nil
		}
		d, _ := st.GetUserItemData(ctx, scope, it.GUID)
		return d
	}
	seriesOf := func(path string) *model.Item {
		it := item(path)
		if it == nil || it.ParentID == nil {
			return nil
		}
		se, _ := st.GetItem(ctx, *it.ParentID)
		if se == nil || se.ParentID == nil {
			return nil
		}
		s, _ := st.GetItem(ctx, *se.ParentID)
		return s
	}

	// 1. The libraries as first found.
	showA := filepath.Join(anime.Root, "Show A (2020)", "Season 01")
	for i := 1; i <= 3; i++ {
		video(filepath.Join(showA, fmt.Sprintf("Show A - S01E%02d.mkv", i)))
	}
	sub(filepath.Join(showA, "Show A - S01E01.en.srt"), "hello")
	bluey := filepath.Join(anime.Root, "Kids", "Bluey", "Season 01", "Bluey - S01E01.mkv")
	video(bluey)
	filmOne := filepath.Join(movies.Root, "Collection", "Film One (2001).mkv")
	filmTwo := filepath.Join(movies.Root, "Collection", "Film Two (2002).mkv")
	video(filmOne)
	video(filmTwo)
	three := filepath.Join(movies.Root, "Film Three (2003)")
	video(filepath.Join(three, "Film Three (2003) - 1080p.mkv"))
	video(filepath.Join(three, "Film Three (2003) - 720p.mkv"))
	video(filepath.Join(three, "Film Three (2003)-trailer.mkv"))
	if err := l.ScanAll(ctx, false); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	e1 := filepath.Join(showA, "Show A - S01E01.mkv")
	e2 := filepath.Join(showA, "Show A - S01E02.mkv")
	if s := seriesOf(e1); s == nil || s.Title != "Show A" {
		t.Fatalf("show A: %+v", s)
	}
	if s := seriesOf(bluey); s == nil || s.Title != "Bluey" {
		t.Fatalf("Bluey in a grouping folder: %+v", s)
	}
	if a, b := item(filmOne), item(filmTwo); a == nil || b == nil || a.ID == b.ID {
		t.Fatal("two films of one collection folder must be two items")
	}
	f3 := item(filepath.Join(three, "Film Three (2003) - 1080p.mkv"))
	if srcs, _ := st.SourcesForItem(ctx, f3.ID); len(srcs) != 2 {
		t.Fatalf("Film Three versions: %d", len(srcs))
	}
	if x, _ := st.Extras(ctx, f3.ID); len(x) != 1 || x[0].ExtraType != "trailer" {
		t.Fatalf("Film Three extras: %+v", x)
	}
	src, _ := st.SourcesForItem(ctx, item(e1).ID)
	subIndex := -1
	for _, s := range src[0].Streams {
		if s.IsExternal && s.Language == "eng" {
			subIndex = s.Index
		}
	}
	if subIndex < 0 {
		t.Fatal("E01's sidecar subtitle was not found")
	}
	check("first scan")

	// 2. Watch history.
	if err := st.SetProgress(ctx, scope, item(e2).GUID, 1500, nil, &subIndex); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFavorite(ctx, scope, seriesOf(e1).GUID, true); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkPlayed(ctx, scope, item(filmOne).GUID, true); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkPlayed(ctx, scope, item(bluey).GUID, true); err != nil {
		t.Fatal(err)
	}
	check("watch history")

	runWatch(t, l)
	time.Sleep(time.Second) // watches in place

	// 3. The show's folder renamed, with an id tag: a new identity for every episode.
	renamedA := filepath.Join(anime.Root, "Show A (2020) {tvdb-777}")
	if err := os.Rename(filepath.Join(anime.Root, "Show A (2020)"), renamedA); err != nil {
		t.Fatal(err)
	}
	e2 = filepath.Join(renamedA, "Season 01", "Show A - S01E02.mkv")
	e1 = filepath.Join(renamedA, "Season 01", "Show A - S01E01.mkv")
	wait("the renamed show", func() bool { return recorded(e2) && !recorded(filepath.Join(showA, "Show A - S01E02.mkv")) })
	if d := userData(e2); d == nil || d.PositionMs != 1500 || d.SubtitleStreamIndex == nil || *d.SubtitleStreamIndex != subIndex {
		t.Fatalf("E02's position and subtitle choice after the rename: %+v", d)
	}
	if s := seriesOf(e2); s == nil || !strings.HasPrefix(s.GUID, "tvdb:") {
		t.Fatalf("renamed show identity: %+v", s)
	} else if d, _ := st.GetUserItemData(ctx, scope, s.GUID); d == nil || !d.IsFavorite {
		t.Fatalf("show's watchlist mark after the rename: %+v", d)
	}
	check("show renamed")

	// 4. More episodes, and a new season.
	video(filepath.Join(renamedA, "Season 01", "Show A - S01E04.mkv"))
	s2 := filepath.Join(renamedA, "Season 02", "Show A - S02E01.mkv")
	video(s2)
	wait("the new episodes", func() bool {
		return recorded(filepath.Join(renamedA, "Season 01", "Show A - S01E04.mkv")) && recorded(s2)
	})
	if it := item(s2); it.SeasonAired == nil || *it.SeasonAired != 2 {
		t.Fatalf("S02E01: %+v", it)
	}
	check("episodes added")

	// 5. Reorganised: a film out of its collection into a folder of its own, a show out of
	// its grouping folder.
	ownFolder := filepath.Join(movies.Root, "Film One (2001)", "Film One (2001).mkv")
	if err := os.MkdirAll(filepath.Dir(ownFolder), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filmOne, ownFolder); err != nil {
		t.Fatal(err)
	}
	newBluey := filepath.Join(anime.Root, "Bluey", "Season 01", "Bluey - S01E01.mkv")
	if err := os.Rename(filepath.Join(anime.Root, "Kids", "Bluey"), filepath.Join(anime.Root, "Bluey")); err != nil {
		t.Fatal(err)
	}
	wait("the moved film and show", func() bool {
		return recorded(ownFolder) && recorded(newBluey) && !recorded(filmOne) && !recorded(bluey)
	})
	if d := userData(ownFolder); d == nil || !d.Played {
		t.Fatalf("Film One's watched mark after the move: %+v", d)
	}
	if d := userData(newBluey); d == nil || !d.Played {
		t.Fatalf("Bluey E01's watched mark after the move: %+v", d)
	}
	check("reorganised")

	// 6. A season folder unreadable (a failing disk, lost permissions): nothing recorded is
	// removed, and the scan says so; readable again, all is as before.
	season1 := filepath.Join(renamedA, "Season 01")
	if err := os.Chmod(season1, 0); err != nil {
		t.Fatal(err)
	}
	video(filepath.Join(movies.Root, "Film Four (2004)", "Film Four (2004).mkv")) // a change, so the watcher scans
	wait("a scan with the folder unreadable", func() bool {
		_, _, lastErr := l.ScanStatus()
		return lastErr != nil && strings.Contains(lastErr.Error(), "could not be read")
	})
	if !recorded(e1) || !recorded(e2) {
		t.Fatal("episodes in an unreadable folder were removed")
	}
	if err := os.Chmod(season1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := l.ScanAll(ctx, false); err != nil {
		t.Fatalf("scan with the folder readable again: %v", err)
	}
	if d := userData(e2); d == nil || d.PositionMs != 1500 {
		t.Fatalf("E02's position after the unreadable folder: %+v", d)
	}
	check("season folder unreadable and back")

	// 7. The whole library folder gone (a drive that did not mount): nothing is removed.
	off := anime.Root + ".off"
	if err := os.Rename(anime.Root, off); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(anime.Root, 0o755); err != nil { // the empty folder Docker leaves behind
		t.Fatal(err)
	}
	if err := l.ScanAll(ctx, false); err == nil || !strings.Contains(err.Error(), "drive mounted") {
		t.Fatalf("scan of an emptied library folder: %v", err)
	}
	check("library folder unavailable")
	if err := os.Remove(anime.Root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(off, anime.Root); err != nil {
		t.Fatal(err)
	}
	if err := l.ScanAll(ctx, false); err != nil {
		t.Fatalf("scan with the drive back: %v", err)
	}
	if !recorded(e2) || !recorded(newBluey) {
		t.Fatal("files lost after the drive came back")
	}
	if d := userData(e2); d == nil || d.PositionMs != 1500 {
		t.Fatalf("E02's position after the drive came back: %+v", d)
	}
	check("library folder back")

	// 8. A file copied in after a running scan has read its folder. The scan is held right
	// after its walk of the Movies library (afterWalk), the file is made then, so that
	// pass cannot see it; the watcher's request for a scan arrives while the pass is
	// still running and must be queued, not dropped: the same ScanAll call then runs it
	// and returns with the file recorded.
	settled()
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	hold := func(lib model.Library) {
		if lib.ID == movies.ID {
			once.Do(func() { close(reached); <-release })
		}
	}
	afterWalk.Store(&hold)
	defer afterWalk.Store(nil)
	done := make(chan error, 1)
	go func() { done <- l.ScanAll(ctx, true) }()
	<-reached
	late := filepath.Join(movies.Root, "Late (2025)", "Late (2025).mkv")
	video(late)
	if recorded(late) {
		t.Fatal("the held pass saw a file made after its walk")
	}
	for deadline := time.Now().Add(30 * time.Second); len(l.normalNext) == 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the watcher never asked for a scan while one was running")
		}
	}
	afterWalk.Store(nil)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the scan and its queued pass: %v", err)
	}
	if !recorded(late) {
		t.Fatal("a scan asked for during a scan was lost: the file copied in is not recorded")
	}
	check("copied in during a scan")

	// 9. Subtitles beside an episode: added, replaced, removed.
	sub2 := filepath.Join(renamedA, "Season 01", "Show A - S01E02.de.srt")
	sub(sub2, "hallo")
	tracks := func(path, lang string) []model.MediaStream {
		srcs, _ := st.SourcesForItem(ctx, item(path).ID)
		var out []model.MediaStream
		for _, s := range srcs[0].Streams {
			if s.IsExternal && s.Language == lang {
				out = append(out, s)
			}
		}
		return out
	}
	wait("an added subtitle", func() bool { return len(tracks(e2, "deu")) == 1 })
	first := tracks(e2, "deu")[0].Index
	sub(sub2, "hallo, und noch etwas länger")
	wait("a replaced subtitle", func() bool {
		tr := tracks(e2, "deu")
		return len(tr) == 1 && tr[0].ExternalSize > 20
	})
	if got := tracks(e2, "deu")[0].Index; got != first {
		t.Fatalf("a replaced subtitle changed number: %d -> %d", first, got)
	}
	if err := os.Remove(sub2); err != nil {
		t.Fatal(err)
	}
	wait("a removed subtitle", func() bool { return len(tracks(e2, "deu")) == 0 })
	check("subtitles")

	// Watch history set in step 2, at the end of it all.
	if d := userData(e2); d == nil || d.PositionMs != 1500 {
		t.Fatalf("E02 at the end: %+v", d)
	}
	if d := userData(ownFolder); d == nil || !d.Played {
		t.Fatalf("Film One at the end: %+v", d)
	}
}
