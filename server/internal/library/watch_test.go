package library

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A file copied into a library folder while the server runs is added after the folders go
// quiet, and removed again when deleted, with no scan asked for. Roxy used to see such a
// file only at the next restart (or a manual scan): there is no scheduled scan.
func TestWatchPicksUpChanges(t *testing.T) {
	old := watchQuiet
	watchQuiet = 200 * time.Millisecond
	t.Cleanup(func() { watchQuiet = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l, st, lib, root, _ := moveFixture(t, model.KindMovies)
	writeSized(t, filepath.Join(root, "Old (2000)", "Old (2000).mkv"), 50)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	runWatch(t, l)
	time.Sleep(300 * time.Millisecond) // watches in place

	has := func(p string) bool {
		part, _ := st.GetPartByPath(ctx, p)
		return part != nil
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if cond() {
				return
			}
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	nw := filepath.Join(root, "New (2024)", "New (2024).mkv") // a new folder, then a file in it
	writeSized(t, nw, 70)
	waitFor("the new film to be added", func() bool { return has(nw) })

	if err := os.RemoveAll(filepath.Join(root, "New (2024)")); err != nil {
		t.Fatal(err)
	}
	waitFor("the deleted film to go", func() bool { return !has(nw) })
	if !has(filepath.Join(root, "Old (2000)", "Old (2000).mkv")) {
		t.Fatal("the untouched film went too")
	}
}

// A library added while the server runs is watched from the next refresh on: a film copied
// into it shows up without a scan being asked for. The library list used to be read once,
// at start, so a new library was watched only after a restart.
func TestWatchPicksUpNewLibrary(t *testing.T) {
	oldQ, oldR := watchQuiet, watchRefresh
	watchQuiet, watchRefresh = 200*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { watchQuiet, watchRefresh = oldQ, oldR })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l, st, _, _, _ := moveFixture(t, model.KindMovies)
	runWatch(t, l)
	time.Sleep(300 * time.Millisecond)

	lib := addLibrary(t, st, model.KindMovies)
	time.Sleep(600 * time.Millisecond) // a refresh picks it up
	film := filepath.Join(lib.Root, "Later (2025)", "Later (2025).mkv")
	writeSized(t, film, 40)
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if part, _ := st.GetPartByPath(ctx, film); part != nil {
			return
		}
	}
	t.Fatal("a film copied into a library added after start was never picked up")
}

// A library folder that becomes a different directory at the same path (a drive
// unmounted and mounted again, a folder deleted or moved away and made again) is watched
// afresh at the next refresh: what is in it then is scanned, and files copied in later are
// seen. The watcher used to skip a root it had registered once, by path, while its watches
// were on the old directory (or dropped with it), so nothing in the new one was noticed
// until a restart. A mount cannot be made in a test; both replacements leave the old
// watch just as blind.
func TestWatchFollowsReplacedLibraryFolder(t *testing.T) {
	for _, how := range []string{"moved away", "deleted"} {
		t.Run(how, func(t *testing.T) {
			oldQ, oldR := watchQuiet, watchRefresh
			watchQuiet, watchRefresh = 200*time.Millisecond, 200*time.Millisecond
			t.Cleanup(func() { watchQuiet, watchRefresh = oldQ, oldR })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			l, st, lib, root, _ := moveFixture(t, model.KindMovies)
			writeSized(t, filepath.Join(root, "Old (2000)", "Old (2000).mkv"), 50)
			if err := l.Scan(ctx, lib); err != nil {
				t.Fatal(err)
			}
			runWatch(t, l)
			time.Sleep(300 * time.Millisecond)

			// The drive goes and another (or the same, mounted again) comes back at the path.
			if how == "moved away" {
				if err := os.Rename(root, root+".old"); err != nil {
					t.Fatal(err)
				}
			} else if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			mounted := filepath.Join(root, "On The Drive (2010)", "On The Drive (2010).mkv")
			writeSized(t, mounted, 60)
			has := func(p string) bool {
				part, _ := st.GetPartByPath(ctx, p)
				return part != nil
			}
			waitFor := func(what string, cond func() bool) {
				t.Helper()
				for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
					if cond() {
						return
					}
				}
				t.Fatalf("timed out waiting for %s", what)
			}
			waitFor("the film on the remounted folder to be scanned", func() bool { return has(mounted) })
			later := filepath.Join(root, "Copied Later (2024)", "Copied Later (2024).mkv")
			writeSized(t, later, 70)
			waitFor("a film copied into the remounted folder to be noticed", func() bool { return has(later) })
		})
	}
}

// A library folder that is still the same directory is not watched afresh (and so not
// rescanned) at every refresh.
func TestWatchLeavesUnchangedFolderAlone(t *testing.T) {
	oldQ, oldR := watchQuiet, watchRefresh
	watchQuiet, watchRefresh = 100*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { watchQuiet, watchRefresh = oldQ, oldR })
	l, _, _, root, _ := moveFixture(t, model.KindMovies)
	writeSized(t, filepath.Join(root, "Old (2000)", "Old (2000).mkv"), 50)
	var scans atomic.Int32
	l.scanOne = func(context.Context, model.Library, bool) error { scans.Add(1); return nil }
	runWatch(t, l)
	time.Sleep(1500 * time.Millisecond) // many refreshes, and time for any scan they asked for
	if n := scans.Load(); n != 0 {
		t.Fatalf("an unchanged library folder was rescanned %d times", n)
	}
}

// runWatch starts the watcher for a test and, when the test ends, stops it and waits for
// it to return, before any earlier cleanup runs (cleanups run last-added first). Tests
// restore watchQuiet and watchRefresh in a cleanup: a watcher still running then read
// them as they were written, a data race that failed the package under -race now and
// then.
func runWatch(t *testing.T, l *Library) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.Watch(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}
