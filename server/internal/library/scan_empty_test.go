package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// The empty-library guard keeps the records of a library whose folder has no media files
// (an unmounted drive, Docker's empty folder for a missing bind mount). It used to keep
// them for good: a library whose files really were all removed kept every record and
// failed every scan. Now the records go when the folder is provably the one a scan found
// files in (same device, inode, creation time), or when an admin confirms it; a different
// directory at the path is still left alone.

func emptyFixture(t *testing.T) (*Library, model.Library, string, func() int, string) {
	t.Helper()
	l, st, lib, root, scope := moveFixture(t, model.KindMovies)
	writeSized(t, filepath.Join(root, "One (2001)", "One (2001).mkv"), 50)
	writeSized(t, filepath.Join(root, "Two (2002)", "Two (2002).mkv"), 60)
	if err := l.Scan(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	recorded := func() int {
		paths, err := st.ListPartPaths(context.Background(), lib.ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(paths)
	}
	if recorded() != 2 {
		t.Fatalf("fixture: %d files recorded", recorded())
	}
	return l, lib, root, recorded, scope
}

func removeVideos(t *testing.T, root string) {
	t.Helper()
	for _, p := range []string{"One (2001)/One (2001).mkv", "Two (2002)/Two (2002).mkv"} {
		if err := os.Remove(filepath.Join(root, p)); err != nil {
			t.Fatal(err)
		}
	}
}

// Every video deleted from a folder that is still the same directory: the records go,
// and the watch history comes back when a file returns.
func TestEmptiedLibraryFolderIsCleared(t *testing.T) {
	ctx := context.Background()
	l, lib, root, recorded, scope := emptyFixture(t)
	st := l.store
	one := filepath.Join(root, "One (2001)", "One (2001).mkv")
	it, _ := st.ItemForPath(ctx, one)
	if err := st.SetProgress(ctx, scope, it.GUID, 120_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	removeVideos(t, root) // the folders stay: the library's own folder is untouched
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatalf("an emptied library's scan failed: %v", err)
	}
	if n := recorded(); n != 0 {
		t.Fatalf("%d records kept of files that are gone", n)
	}
	if left, _, _ := st.ListItems(ctx, storeMovies(lib)); len(left) != 0 {
		t.Fatalf("%d films left with no files", len(left))
	}
	if _, waiting := l.EmptyRefused()[lib.ID]; waiting {
		t.Fatal("cleared library still waits for a confirmation")
	}
	// The file comes back: its history is found again.
	writeSized(t, one, 50)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	back, _ := st.ItemForPath(ctx, one)
	if back == nil {
		t.Fatal("returned file not added")
	}
	if d, _ := st.GetUserItemData(ctx, scope, back.GUID); d == nil || d.PositionMs != 120_000 {
		t.Fatalf("history of the returned file: %+v", d)
	}
}

// Another, empty directory at the library's path (a drive not mounted, Docker's stand-in
// folder): nothing is removed, the scan says so, and the library waits for an admin.
// Confirmed, the next scan removes the records; the confirmation is used up by it.
func TestReplacedEmptyLibraryFolderWaitsForConfirmation(t *testing.T) {
	ctx := context.Background()
	l, lib, root, recorded, _ := emptyFixture(t)
	if err := os.Rename(root, root+".away"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	err := l.Scan(ctx, lib)
	if err == nil || !strings.Contains(err.Error(), "nothing was removed") {
		t.Fatalf("scan of a stand-in folder: %v", err)
	}
	if n := recorded(); n != 2 {
		t.Fatalf("records removed from a library whose drive may just be missing: %d left", n)
	}
	if n := l.EmptyRefused()[lib.ID]; n != 2 {
		t.Fatalf("EmptyRefused = %v", l.EmptyRefused())
	}

	l.AcceptEmpty(lib.ID)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatalf("confirmed scan: %v", err)
	}
	if n := recorded(); n != 0 {
		t.Fatalf("confirmed empty, but %d records kept", n)
	}
	if _, waiting := l.EmptyRefused()[lib.ID]; waiting {
		t.Fatal("still waiting after the confirmation")
	}

	// Files again, then a stand-in folder again: the old confirmation does not carry over.
	writeSized(t, filepath.Join(root, "Three (2003)", "Three (2003).mkv"), 70)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+".away2"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := l.Scan(ctx, lib); err == nil || recorded() != 1 {
		t.Fatalf("a used-up confirmation removed records: err %v, %d left", err, recorded())
	}
}

// A confirmation given while the files are there is used up by that scan, not kept for
// a later one where the drive is missing.
func TestEmptyConfirmationIsNotKept(t *testing.T) {
	ctx := context.Background()
	l, lib, root, recorded, _ := emptyFixture(t)
	l.AcceptEmpty(lib.ID)
	if err := l.Scan(ctx, lib); err != nil { // files present: nothing to confirm
		t.Fatal(err)
	}
	if err := os.Rename(root, root+".away"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := l.Scan(ctx, lib); err == nil || recorded() != 2 {
		t.Fatalf("an old confirmation emptied the library: err %v, %d left", err, recorded())
	}
}

// When the folder's identity cannot be read (off Linux, or an error), an emptied folder
// is never cleared on its own: only a confirmation does it.
func TestEmptyFolderWithUnknownIdentityIsKept(t *testing.T) {
	ctx := context.Background()
	old := rootIdentity
	rootIdentity = func(string) (string, error) { return "", nil }
	t.Cleanup(func() { rootIdentity = old })
	l, lib, root, recorded, _ := emptyFixture(t)
	removeVideos(t, root)
	if err := l.Scan(ctx, lib); err == nil || recorded() != 2 {
		t.Fatalf("cleared without a known identity: err %v, %d left", err, recorded())
	}
}

// An extra that cannot be added (here: no title in its folder to belong to) makes the
// scan report it, while the rest of the library is still scanned; once it can be added,
// the scan is clean. Extra failures used to be logged only, so the scan reported success.
func TestExtraFailuresAreReported(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, _ := moveFixture(t, model.KindMovies)
	film := filepath.Join(root, "Film (2001)", "Film (2001).mkv")
	writeSized(t, film, 50)
	writeSized(t, filepath.Join(root, "Lonely (2005)", "trailers", "Teaser.mkv"), 30)
	err := l.Scan(ctx, lib)
	if err == nil || !strings.Contains(err.Error(), "1 extra(s) could not be added") || !strings.Contains(err.Error(), "Teaser.mkv") {
		t.Fatalf("scan with a failing extra: %v", err)
	}
	if p, _ := st.GetPartByPath(ctx, film); p == nil {
		t.Fatal("the rest of the library was not scanned")
	}
	// Its title arrives: the extra is added and the scan is clean.
	writeSized(t, filepath.Join(root, "Lonely (2005)", "Lonely (2005).mkv"), 40)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatalf("scan once the extra has its title: %v", err)
	}
}

func storeMovies(lib model.Library) store.ItemQuery {
	return store.ItemQuery{LibraryID: lib.ID, Types: []model.ItemType{model.TypeMovie}}
}

// .ignore rules that leave out everything in the library (an empty .ignore in its own
// folder, a "*" pattern) are not a library whose files were removed: nothing is removed
// without a confirmation, though the folder is still the same directory. They used to
// empty the library on their own, the walk having seen no files.
func TestIgnoreEverythingIsNotAnEmptyLibrary(t *testing.T) {
	for name, rules := range map[string]string{"empty .ignore": "", "star pattern": "*\n", "videos pattern": "*.mkv\n"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			l, lib, root, recorded, _ := emptyFixture(t)
			if err := os.WriteFile(filepath.Join(root, ".ignore"), []byte(rules), 0o644); err != nil {
				t.Fatal(err)
			}
			err := l.Scan(ctx, lib)
			if err == nil || !strings.Contains(err.Error(), ".ignore") || recorded() != 2 {
				t.Fatalf("rules hiding everything: err %v, %d of 2 records left", err, recorded())
			}
			// Confirmed by an admin, the records do go.
			l.AcceptEmpty(lib.ID)
			if err := l.Scan(ctx, lib); err != nil || recorded() != 0 {
				t.Fatalf("confirmed: err %v, %d left", err, recorded())
			}
		})
	}
}

// A film folder that can be listed but not entered (a "chmod -R 644"): the file cannot be
// checked, so its record stays and nothing is removed that scan, as for a folder that
// cannot be read. It used to be taken for a symlink out of the library and dropped; with
// every film folder like that, the library emptied itself.
func TestUnenterableFolderKeepsRecords(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root enters folders regardless of their permissions")
	}
	for _, dirs := range [][]string{{"One (2001)"}, {"One (2001)", "Two (2002)"}} {
		t.Run(strings.Join(dirs, "+"), func(t *testing.T) {
			ctx := context.Background()
			l, lib, root, recorded, _ := emptyFixture(t)
			for _, d := range dirs {
				dir := filepath.Join(root, d)
				if err := os.Chmod(dir, 0o644); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			}
			err := l.Scan(ctx, lib)
			if n := recorded(); n != 2 || err == nil {
				t.Fatalf("unenterable folders: err %v, %d of 2 records left", err, n)
			}
		})
	}
}

// A symlink to nothing is skipped, not counted as unreadable (that would stop every
// removal in the library for as long as the dangling link exists).
func TestDanglingSymlinkDoesNotBlockRemovals(t *testing.T) {
	ctx := context.Background()
	l, lib, root, recorded, _ := emptyFixture(t)
	if err := os.Symlink(filepath.Join(root, "nowhere.mkv"), filepath.Join(root, "One (2001)", "Link (2003).mkv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "Two (2002)", "Two (2002).mkv")); err != nil {
		t.Fatal(err)
	}
	if err := l.Scan(ctx, lib); err != nil || recorded() != 1 {
		t.Fatalf("dangling link: err %v, %d left (want the deleted film removed)", err, recorded())
	}
}
