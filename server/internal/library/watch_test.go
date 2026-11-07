package library

import (
	"context"
	"os"
	"path/filepath"
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
	go l.Watch(ctx)
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
	go l.Watch(ctx)
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
