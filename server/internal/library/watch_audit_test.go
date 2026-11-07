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

// Found by an audit: a show folder renamed while watched kept its watches under the old
// name, so a folder made inside it afterwards could not be found under that name, was
// never watched, and a file in it was never noticed. A renamed folder's watches go with
// its old name now, and its new name is watched afresh.
func watchRenamed(t *testing.T, rename bool, sub string) {
	old := watchQuiet
	watchQuiet = 200 * time.Millisecond
	t.Cleanup(func() { watchQuiet = old })
	ctx := context.Background()
	l, st, lib, root, _ := moveFixture(t, model.KindSeries)
	writeSized(t, filepath.Join(root, "Show A", "Season 1", "Show A - S01E01.mkv"), 50)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	runWatch(t, l)
	time.Sleep(300 * time.Millisecond)
	show := "Show A"
	if rename {
		show = "Show B"
		if err := os.Rename(filepath.Join(root, "Show A"), filepath.Join(root, "Show B")); err != nil {
			t.Fatal(err)
		}
	}
	moved := filepath.Join(root, show, "Season 1", "Show A - S01E01.mkv")
	has := func(p string) bool { part, _ := st.GetPartByPath(ctx, p); return part != nil }
	for deadline := time.Now().Add(5 * time.Second); !has(moved); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("rename not picked up")
		}
	}
	time.Sleep(500 * time.Millisecond)
	// A new folder below the renamed show's season folder, then a file in it.
	nf := filepath.Join(root, show, sub, "Show A - S01E02.mkv")
	if err := os.MkdirAll(filepath.Dir(nf), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // the folder's Create has been handled
	writeSized(t, nf, 70)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if has(nf) {
			return
		}
	}
	t.Fatal("a file in a folder made inside a renamed show was never picked up")
}

func TestWatchNewFolderNoRename(t *testing.T)      { watchRenamed(t, false, "Season 1/Part 2") }
func TestWatchNewFolderInRenamedShow(t *testing.T) { watchRenamed(t, true, "Season 1/Part 2") }
func TestWatchNewSeasonInRenamedShow(t *testing.T) { watchRenamed(t, true, "Season 2") }

// Found by an audit: one file that keeps changing (a download or recording being written)
// reset the single quiet timer forever, so nothing copied anywhere else was scanned
// meanwhile. In a folder the .ignore rules leave out, it is not watched at all now; in a
// watched folder, a change waits at most watchMaxDelay.
func TestBusyFileDoesNotStarveScans(t *testing.T) {
	t.Run("ignored folder", func(t *testing.T) { busyFile(t, true) })
	t.Run("watched folder", func(t *testing.T) { busyFile(t, false) })
}

func busyFile(t *testing.T, ignored bool) {
	old, oldMax := watchQuiet, watchMaxDelay
	watchQuiet, watchMaxDelay = 300*time.Millisecond, 2*time.Second
	t.Cleanup(func() { watchQuiet, watchMaxDelay = old, oldMax })
	ctx, cancel := context.WithCancel(context.Background())
	l, st, lib, root, _ := moveFixture(t, model.KindMovies)
	writeSized(t, filepath.Join(root, "Old (2000)", "Old (2000).mkv"), 50)
	if ignored {
		os.WriteFile(filepath.Join(root, ".ignore"), []byte("/incoming\n"), 0o644)
	}
	os.MkdirAll(filepath.Join(root, "incoming"), 0o755)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	runWatch(t, l)
	time.Sleep(300 * time.Millisecond)
	writing := make(chan struct{})
	t.Cleanup(func() { cancel(); <-writing }) // stops before the folder is removed
	go func() {
		defer close(writing)
		f, _ := os.Create(filepath.Join(root, "incoming", "dl.mkv"))
		defer f.Close()
		for ctx.Err() == nil {
			f.Write(make([]byte, 1024))
			time.Sleep(100 * time.Millisecond)
		}
	}()
	nw := filepath.Join(root, "New (2024)", "New (2024).mkv")
	writeSized(t, nw, 70)
	for deadline := time.Now().Add(6 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if part, _ := st.GetPartByPath(ctx, nw); part != nil {
			return
		}
	}
	t.Fatal("new film not scanned within 6s while another file was being written")
}

// Found by an audit: a folder-only rule ("Bonus/") ignored a file named Bonus in a
// subfolder; it reaches files only through their folders now.
func TestIgnoreDirOnlyRuleSkipsFiles(t *testing.T) {
	r, _ := compileIgnore("Bonus/")
	rules := &ignoreRules{rules: []ignoreRule{r}}
	if ign, _ := rules.match("Show/Bonus", false); ign {
		t.Error("folder-only rule 'Bonus/' ignored a FILE named Bonus in a subfolder")
	}
	if ign, _ := rules.match("Show/Bonus/clip.mkv", false); !ign {
		t.Error("folder-only rule 'Bonus/' no longer ignores a file inside a Bonus folder")
	}
	if ign, _ := rules.match("Show/Bonus", true); !ign {
		t.Error("folder-only rule 'Bonus/' no longer ignores the folder")
	}
}

// An .ignore rule removed while watched: the folder it left out is watched from then on,
// so an episode copied into it later is noticed. The watches were set up once, with the
// rules as they were, and never rebuilt when the rules changed. The rule goes three ways:
// the file deleted; replaced atomically (a new file written beside it and renamed over
// it, as editors and config tools do), with a rule that no longer names the season; and
// renamed away.
func TestWatchFollowsIgnoreRuleChanges(t *testing.T) {
	t.Run("deleted", func(t *testing.T) {
		watchIgnoreChange(t, func(rules string) error { return os.Remove(rules) })
	})
	t.Run("replaced atomically", func(t *testing.T) {
		watchIgnoreChange(t, func(rules string) error {
			tmp := rules + ".tmp"
			if err := os.WriteFile(tmp, []byte("Extras/\n"), 0o644); err != nil {
				return err
			}
			return os.Rename(tmp, rules)
		})
	})
	t.Run("renamed away", func(t *testing.T) {
		watchIgnoreChange(t, func(rules string) error { return os.Rename(rules, rules+".bak") })
	})
}

func watchIgnoreChange(t *testing.T, change func(rules string) error) {
	oldQ, oldR := watchQuiet, watchRefresh
	watchQuiet, watchRefresh = 200*time.Millisecond, time.Hour // no refresh to hide behind
	t.Cleanup(func() { watchQuiet, watchRefresh = oldQ, oldR })
	ctx := context.Background()
	l, st, lib, root, _ := moveFixture(t, model.KindSeries)
	show := filepath.Join(root, "Show")
	rules := filepath.Join(show, ".ignore")
	writeSized(t, filepath.Join(show, "Season 01", "Show - S01E01.mkv"), 50)
	writeSized(t, filepath.Join(show, "Season 02", "Show - S02E01.mkv"), 60)
	if err := os.WriteFile(rules, []byte("Season 01/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	has := func(p string) bool { part, _ := st.GetPartByPath(ctx, p); return part != nil }
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if cond() {
				return
			}
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	if has(filepath.Join(show, "Season 01", "Show - S01E01.mkv")) {
		t.Fatal("fixture: the ignored season was scanned")
	}
	runWatch(t, l)
	time.Sleep(300 * time.Millisecond)

	if err := change(rules); err != nil {
		t.Fatal(err)
	}
	waitFor("the season the rule left out to be scanned", func() bool { return has(filepath.Join(show, "Season 01", "Show - S01E01.mkv")) })
	time.Sleep(500 * time.Millisecond)
	later := filepath.Join(show, "Season 01", "Show - S01E02.mkv")
	writeSized(t, later, 70)
	waitFor("an episode copied into the no-longer-ignored season", func() bool { return has(later) })
}

// The reverse: an .ignore rule added while watched stops the folder it leaves out being
// watched (its watches are removed), and a change inside it no longer asks for a scan.
func TestWatchDropsFoldersANewIgnoreRuleLeavesOut(t *testing.T) {
	oldQ, oldR := watchQuiet, watchRefresh
	watchQuiet, watchRefresh = 200*time.Millisecond, time.Hour
	t.Cleanup(func() { watchQuiet, watchRefresh = oldQ, oldR })
	ctx := context.Background()
	l, _, lib, root, _ := moveFixture(t, model.KindSeries)
	show := filepath.Join(root, "Show")
	season1 := filepath.Join(show, "Season 01")
	ep := filepath.Join(season1, "Show - S01E01.mkv")
	writeSized(t, ep, 50)
	writeSized(t, filepath.Join(show, "Season 02", "Show - S02E01.mkv"), 60)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	var scans atomic.Int32
	l.scanOne = func(ctx context.Context, lib model.Library, force bool) error {
		scans.Add(1)
		return l.scan(ctx, lib, force)
	}
	runWatch(t, l)
	watching := func(dir string) bool {
		if f := l.watchList.Load(); f != nil {
			for _, p := range (*f)() {
				if p == dir {
					return true
				}
			}
		}
		return false
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if cond() {
				return
			}
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	waitFor("Season 01 to be watched", func() bool { return watching(season1) })

	if err := os.WriteFile(filepath.Join(show, ".ignore"), []byte("Season 01/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor("Season 01's watch to be removed", func() bool { return !watching(season1) })
	if !watching(filepath.Join(show, "Season 02")) {
		t.Fatal("Season 02, which the rule does not name, is no longer watched")
	}
	waitFor("the scan the rule change asks for", func() bool { return scans.Load() > 0 })
	time.Sleep(4 * watchQuiet) // that scan done, and nothing else pending
	before := scans.Load()

	// Changes inside the folder the rule now leaves out: no scan.
	for i := 0; i < 3; i++ {
		writeSized(t, ep, 80+i)
		writeSized(t, filepath.Join(season1, "Show - S01E02.mkv"), 90+i)
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(8 * watchQuiet) // well past the quiet period a scan would wait for
	if n := scans.Load(); n != before {
		t.Fatalf("changes in a folder the rules leave out asked for %d scan(s)", n-before)
	}
}
